package scrobble

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const lbAPI = "https://api.listenbrainz.org/1/submit-listens"

// ListenBrainz submits listens with a user token (listenbrainz.org/profile).
type ListenBrainz struct {
	Token string
	http  *http.Client
}

func NewListenBrainz(token string) *ListenBrainz {
	return &ListenBrainz{Token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

// lbTrackMeta mirrors the payload's track_metadata object (kept format-
// compatible with the history JSONL the library package writes).
type lbTrackMeta struct {
	ArtistName     string         `json:"artist_name"`
	TrackName      string         `json:"track_name"`
	ReleaseName    string         `json:"release_name,omitempty"`
	AdditionalInfo map[string]any `json:"additional_info,omitempty"`
}

type lbListen struct {
	ListenedAt    int64       `json:"listened_at,omitempty"` // omitted for playing_now
	TrackMetadata lbTrackMeta `json:"track_metadata"`
}

// submit POSTs one listen document of the given type ("single"|"playing_now").
func (lb *ListenBrainz) submit(listenType string, listens []lbListen) error {
	if lb.Token == "" {
		return fmt.Errorf("listenbrainz: no token")
	}
	doc := struct {
		ListenType string     `json:"listen_type"`
		Payload    []lbListen `json:"payload"`
	}{listenType, listens}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", lbAPI, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+lb.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := lb.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("listenbrainz: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

func lbMeta(artist, track, album string, durationSec int) lbTrackMeta {
	meta := lbTrackMeta{ArtistName: artist, TrackName: track, ReleaseName: album}
	if durationSec > 0 {
		meta.AdditionalInfo = map[string]any{"duration_ms": durationSec * 1000}
	}
	return meta
}

// PlayingNow reports the current track (not persisted by ListenBrainz).
func (lb *ListenBrainz) PlayingNow(artist, track, album string, durationSec int) error {
	return lb.submit("playing_now", []lbListen{{TrackMetadata: lbMeta(artist, track, album, durationSec)}})
}

// Listen submits one completed play that started at startedAt.
func (lb *ListenBrainz) Listen(artist, track, album string, durationSec int, startedAt time.Time) error {
	return lb.submit("single", []lbListen{{
		ListenedAt:    startedAt.Unix(),
		TrackMetadata: lbMeta(artist, track, album, durationSec),
	}})
}
