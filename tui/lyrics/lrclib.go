// Package lyrics implements an LRCLIB provider.
//
// LRCLIB (https://lrclib.net) is a free, open, no-auth lyrics database that
// provides synced (timestamped) and plain lyrics. It runs get and search
// concurrently so a slow miss only costs one timeout.
package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LRCLIBProvider is a lyrics.Provider backed by lrclib.net.
type LRCLIBProvider struct {
	client    *http.Client
	userAgent string
}

// NewLRCLIBProvider creates an LRCLIB provider with sensible defaults.
func NewLRCLIBProvider() *LRCLIBProvider {
	return &LRCLIBProvider{
		// LRCLIB can be slow (several seconds of TTFB when its server is loaded).
		// The worst-case win comes from running get+search concurrently (a miss
		// costs ~one timeout, not two), not from a short cap — measured TTFB is
		// ~7s during a slow spell, so an over-aggressive timeout would drop valid
		// but slow lyrics. 8s catches those while concurrency keeps the total
		// bounded.
		client:    &http.Client{Timeout: 8 * time.Second},
		userAgent: "pixeltui (https://github.com/dotjarden/pixeltui)",
	}
}

// Name returns the provider identifier.
func (LRCLIBProvider) Name() string { return "lrclib" }

// Priority places LRCLIB above YouTube Music because it often has synced lyrics
// and needs no video id.
func (LRCLIBProvider) Priority() int { return 100 }

// Fetch looks up lyrics by artist/track, using album and duration (when known)
// to disambiguate.
func (p *LRCLIBProvider) Fetch(ctx context.Context, artist, track, album string, durationSec int) (Result, error) {
	if strings.TrimSpace(artist) == "" && strings.TrimSpace(track) == "" {
		return Result{}, fmt.Errorf("no track info")
	}

	// Exact get — most accurate (duration disambiguates remixes/live versions).
	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", track)
	if album != "" {
		q.Set("album_name", album)
	}
	if durationSec > 0 {
		q.Set("duration", strconv.Itoa(durationSec))
	}
	// Search fallback by artist+track.
	s := url.Values{}
	s.Set("artist_name", artist)
	s.Set("track_name", track)

	c, cancel := context.WithCancel(ctx)
	defer cancel()

	getCh := make(chan Result, 1)
	go func() {
		var got apiLyrics
		if p.getJSON(c, "https://lrclib.net/api/get?"+q.Encode(), &got) {
			getCh <- toResult(got)
			return
		}
		getCh <- Result{}
	}()
	searchCh := make(chan Result, 1)
	go func() {
		var hits []apiLyrics
		if p.getJSON(c, "https://lrclib.net/api/search?"+s.Encode(), &hits) {
			for _, h := range hits {
				if r := toResult(h); !r.Empty() {
					searchCh <- r
					return
				}
			}
		}
		searchCh <- Result{}
	}()

	if g := <-getCh; !g.Empty() {
		cancel() // exact hit — drop the in-flight search
		return g, nil
	}
	if r := <-searchCh; !r.Empty() {
		return r, nil
	}
	return Result{}, ErrNotFound
}

type apiLyrics struct {
	SyncedLyrics string `json:"syncedLyrics"`
	PlainLyrics  string `json:"plainLyrics"`
}

func (p *LRCLIBProvider) getJSON(ctx context.Context, u string, v interface{}) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(resp.Body).Decode(v) == nil
}

func toResult(a apiLyrics) Result {
	return Result{Synced: parseLRC(a.SyncedLyrics), Plain: strings.TrimSpace(a.PlainLyrics)}
}

// parseLRC parses LRC-format text ("[mm:ss.xx] words") into sorted, timestamped
// lines. Non-timestamp tags (e.g. [ar:...]) and blank lines are ignored.
func parseLRC(s string) []Line {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []Line
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimRight(raw, "\r")
		var stamps []float64
		rest := line
		for strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				break
			}
			if t, ok := parseStamp(rest[1:end]); ok {
				stamps = append(stamps, t)
				rest = rest[end+1:]
				continue
			}
			break // metadata tag, not a timestamp
		}
		text := strings.TrimSpace(rest)
		for _, t := range stamps {
			out = append(out, Line{T: t, Text: text})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

// parseStamp parses "mm:ss.xx" or "mm:ss" into seconds.
func parseStamp(s string) (float64, bool) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return 0, false
	}
	mm, err := strconv.Atoi(strings.TrimSpace(s[:colon]))
	if err != nil {
		return 0, false
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(s[colon+1:]), 64)
	if err != nil {
		return 0, false
	}
	return float64(mm)*60 + sec, true
}
