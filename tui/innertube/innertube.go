// Package innertube resolves YouTube video ids to pre-signed CDN audio URLs
// natively, via the InnerTube /player endpoint using the ANDROID_VR client.
// That client returns *pre-signed* URLs — no signature cipher, no `nsig`
// descrambling — so resolution is a single stdlib HTTP call (~0.2s) instead
// of spawning yt-dlp/Python (~2–20s). Used by both the TUI player and the
// companion-app server; yt-dlp remains only as a fallback at the call sites.
//
// A `visitorData` token is attached to the client context: without it YouTube
// answers some videos with LOGIN_REQUIRED ("confirm you're not a bot"). The
// token is fetched once and refreshed automatically when it goes stale.
package innertube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	playerURL        = "https://youtubei.googleapis.com/youtubei/v1/player"
	visitorURL       = "https://youtubei.googleapis.com/youtubei/v1/visitor_id"
	androidVRVersion = "1.61.48"
	androidVRUA      = "com.google.android.apps.youtube.vr.oculus/1.61.48 (Linux; U; Android 12)"
)

// Result is a resolved stream: a direct AAC/m4a CDN URL, the video's
// authoritative duration, and the URL's expiry (unix seconds).
type Result struct {
	URL         string
	DurationSec int
	Expire      int64
}

type format struct {
	Itag             int    `json:"itag"`
	URL              string `json:"url"`
	MimeType         string `json:"mimeType"`
	Bitrate          int    `json:"bitrate"`
	ApproxDurationMs string `json:"approxDurationMs"`
}

// visitorData is fetched once and reused across requests; refreshed on demand
// when a resolution comes back LOGIN_REQUIRED.
var (
	visitorMu   sync.Mutex
	visitorData string
)

func clientContext() map[string]any {
	visitorMu.Lock()
	vd := visitorData
	visitorMu.Unlock()
	c := map[string]any{
		"clientName":        "ANDROID_VR",
		"clientVersion":     androidVRVersion,
		"androidSdkVersion": 32,
		"osName":            "Android",
		"osVersion":         "12",
	}
	if vd != "" {
		c["visitorData"] = vd
	}
	return c
}

// ensureVisitor fetches a visitorData token if we don't have one. force=true
// refetches even if one is cached (used after a LOGIN_REQUIRED).
func ensureVisitor(ctx context.Context, force bool) {
	visitorMu.Lock()
	have := visitorData != ""
	visitorMu.Unlock()
	if have && !force {
		return
	}
	reqBody, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName": "ANDROID_VR", "clientVersion": androidVRVersion,
		}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, visitorURL, bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", androidVRUA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var vr struct {
		ResponseContext struct {
			VisitorData string `json:"visitorData"`
		} `json:"responseContext"`
	}
	if json.NewDecoder(resp.Body).Decode(&vr) == nil && vr.ResponseContext.VisitorData != "" {
		visitorMu.Lock()
		visitorData = vr.ResponseContext.VisitorData
		visitorMu.Unlock()
	}
}

// Resolve returns a direct AAC/m4a CDN URL for a video id plus its
// authoritative duration and the URL's expiry. It pins itag 140 (128k AAC-LC,
// clean progressive) and never returns itag 139 (48k HE-AAC, whose SBR
// half-rate base trips players into reporting 2× duration with trailing
// silence) unless nothing else exists. On LOGIN_REQUIRED it refreshes the
// visitorData token and retries once.
func Resolve(ctx context.Context, videoID string) (Result, error) {
	ensureVisitor(ctx, false)
	res, err := player(ctx, videoID)
	if err != nil && strings.Contains(err.Error(), "LOGIN_REQUIRED") {
		ensureVisitor(ctx, true) // token stale/bot-flagged — refresh and retry once
		res, err = player(ctx, videoID)
	}
	return res, err
}

func player(ctx context.Context, videoID string) (Result, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"context":        map[string]any{"client": clientContext()},
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	})

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, playerURL, bytes.NewReader(reqBody))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", androidVRUA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	var pr struct {
		PlayabilityStatus struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"playabilityStatus"`
		StreamingData struct {
			ExpiresInSeconds string   `json:"expiresInSeconds"`
			AdaptiveFormats  []format `json:"adaptiveFormats"`
		} `json:"streamingData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return Result{}, fmt.Errorf("innertube decode: %w", err)
	}
	if pr.PlayabilityStatus.Status != "OK" {
		return Result{}, fmt.Errorf("innertube: %s", pr.PlayabilityStatus.Status)
	}

	// Pick the cleanest AAC: itag 140 first; else the highest-bitrate audio/mp4
	// that isn't HE-AAC itag 139; itag 139 only as a last resort.
	var best, fallback139 *format
	for i := range pr.StreamingData.AdaptiveFormats {
		fm := &pr.StreamingData.AdaptiveFormats[i]
		if fm.URL == "" || !strings.HasPrefix(fm.MimeType, "audio/mp4") {
			continue
		}
		switch {
		case fm.Itag == 140:
			best = fm
		case fm.Itag == 139:
			fallback139 = fm
		case best == nil || fm.Bitrate > best.Bitrate:
			best = fm
		}
		if best != nil && best.Itag == 140 {
			break
		}
	}
	if best == nil {
		best = fallback139
	}
	if best == nil {
		return Result{}, fmt.Errorf("innertube: no audio/mp4 format")
	}

	durSec := 0
	if ms, err := strconv.Atoi(best.ApproxDurationMs); err == nil {
		durSec = ms / 1000
	}
	expire := time.Now().Add(5 * time.Hour).Unix()
	if secs, err := strconv.ParseInt(pr.StreamingData.ExpiresInSeconds, 10, 64); err == nil && secs > 60 {
		// Trim 60s so we never hand out a URL that expires mid-request.
		expire = time.Now().Add(time.Duration(secs-60) * time.Second).Unix()
	}
	return Result{URL: best.URL, DurationSec: durSec, Expire: expire}, nil
}
