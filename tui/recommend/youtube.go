package recommend

import (
	"context"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// YouTubeRadioEngine seeds recommendations from YouTube Music's watch playlist
// for a given video id.
type YouTubeRadioEngine struct {
	priority int
}

// NewYouTubeRadioEngine creates a YouTube Music radio engine.
func NewYouTubeRadioEngine() *YouTubeRadioEngine {
	return &YouTubeRadioEngine{priority: 100}
}

// Name returns the engine identifier.
func (YouTubeRadioEngine) Name() string { return "youtube_radio" }

// Priority returns the engine priority. Higher than Last.fm so a direct YTM
// source track uses YTM's own radio first.
func (e *YouTubeRadioEngine) Priority() int {
	if e == nil || e.priority == 0 {
		return 100
	}
	return e.priority
}

// CanSeed reports whether the candidate has a YouTube video id.
func (YouTubeRadioEngine) CanSeed(_ context.Context, c engine.Candidate) bool {
	return c.VideoID != ""
}

// Recommend fetches YouTube Music's watch playlist for the video id.
func (e *YouTubeRadioEngine) Recommend(_ context.Context, c engine.Candidate, n int, exclude []string) ([]engine.Candidate, error) {
	if c.VideoID == "" {
		return nil, ErrNoEngine
	}
	tracks, err := ytm.Radio(c.VideoID, n)
	if err != nil {
		return nil, err
	}
	if len(exclude) == 0 {
		return tracks, nil
	}
	out := make([]engine.Candidate, 0, len(tracks))
	for _, t := range tracks {
		key := strings.ToLower(t.Track + "|" + t.Artist)
		if contains(exclude, key) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
