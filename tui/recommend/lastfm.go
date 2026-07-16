package recommend

import (
	"context"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// LastfmEngine wraps the existing engine.Recommender as a recommend.Engine.
// It seeds from any candidate that has artist + track.
type LastfmEngine struct {
	rec      *engine.Recommender
	priority int
}

// NewLastfmEngine creates a Last.fm-based recommendation engine. priority
// defaults to 50 if zero.
func NewLastfmEngine(rec *engine.Recommender) *LastfmEngine {
	if rec == nil {
		return nil
	}
	return &LastfmEngine{rec: rec, priority: 50}
}

// Name returns the engine identifier.
func (LastfmEngine) Name() string { return "lastfm" }

// Priority returns the engine priority.
func (e *LastfmEngine) Priority() int {
	if e == nil || e.priority == 0 {
		return 50
	}
	return e.priority
}

// CanSeed reports whether the candidate has enough metadata for Last.fm.
func (LastfmEngine) CanSeed(_ context.Context, c engine.Candidate) bool {
	return c.Artist != "" && c.Track != ""
}

// Recommend delegates to the underlying engine.Recommender.
func (e *LastfmEngine) Recommend(_ context.Context, c engine.Candidate, n int, exclude []string) ([]engine.Candidate, error) {
	if e == nil || e.rec == nil {
		return nil, ErrNoEngine
	}
	recs, err := e.rec.Recommend(c.Artist, c.Track, n)
	if err != nil {
		return nil, err
	}
	if len(exclude) == 0 {
		return recs, nil
	}
	out := make([]engine.Candidate, 0, len(recs))
	for _, r := range recs {
		key := strings.ToLower(r.Track + "|" + r.Artist)
		if contains(exclude, key) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
