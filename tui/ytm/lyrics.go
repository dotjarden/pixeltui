package ytm

import (
	"context"
	"fmt"
	"strings"

	"github.com/dotjarden/pixeltui/tui/lyrics"
)

// LyricsProvider exposes YouTube Music's plain-text lyrics as a lyrics.Provider.
// It needs a valid video id, so it's typically registered as a fallback behind
// LRCLIB.
type LyricsProvider struct{}

// NewLyricsProvider creates a YouTube Music lyrics provider.
func NewLyricsProvider() *LyricsProvider {
	return &LyricsProvider{}
}

// Name returns the provider identifier.
func (LyricsProvider) Name() string { return "youtube" }

// Priority places YouTube Music below LRCLIB; LRCLIB has synced lyrics and works
// without a video id, so it runs first.
func (LyricsProvider) Priority() int { return 50 }

// Fetch returns plain-text lyrics for a track. album and duration are ignored.
func (LyricsProvider) Fetch(_ context.Context, _, track, _ string, _ int) (lyrics.Result, error) {
	if strings.TrimSpace(track) == "" {
		return lyrics.Result{}, fmt.Errorf("no track")
	}
	// We don't have a video id here; this provider is normally used when the
	// caller already knows the id (e.g. the TUI after resolving a candidate).
	// For now, return not found so the registry can fall back or the caller can
	// use FetchByVideoID directly.
	return lyrics.Result{}, lyrics.ErrNotFound
}

// FetchByVideoID returns plain-text lyrics for a specific video id.
func (LyricsProvider) FetchByVideoID(_ context.Context, videoID string) (lyrics.Result, error) {
	if videoID == "" {
		return lyrics.Result{}, fmt.Errorf("no track")
	}
	text, err := Lyrics(videoID)
	if err != nil {
		return lyrics.Result{}, err
	}
	if strings.TrimSpace(text) == "" {
		return lyrics.Result{}, lyrics.ErrNotFound
	}
	return lyrics.Result{Plain: text}, nil
}
