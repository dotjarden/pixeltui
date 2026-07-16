// Package recommend provides a registry of recommendation engines. Different
// engines can seed from different sources: Last.fm similarity graph, YouTube
// Music radio, Spotify audio features, local play history, etc. The registry
// tries engines in priority order; each engine decides whether it can seed from
// a given candidate. This keeps every recommender optional and independent.
package recommend

import (
	"context"
	"errors"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ErrNoEngine means no registered engine could seed from the given track.
var ErrNoEngine = errors.New("no recommendation engine available for this seed")

// Engine is one recommendation source.
type Engine interface {
	// Name returns a short identifier, e.g. "lastfm", "youtube_radio".
	Name() string

	// Priority orders engines within a registry. Higher values are tried first.
	Priority() int

	// CanSeed reports whether this engine can produce recommendations from c.
	CanSeed(ctx context.Context, c engine.Candidate) bool

	// Recommend returns up to n candidates similar to/derived from c.
	// exclude is a set of track keys the caller wants to avoid.
	Recommend(ctx context.Context, c engine.Candidate, n int, exclude []string) ([]engine.Candidate, error)
}

// Registry holds recommendation engines and queries them in priority order.
type Registry struct {
	engines []Engine
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds an engine.
func (r *Registry) Register(e Engine) {
	r.engines = append(r.engines, e)
}

// Engines returns registered engines in priority order.
func (r *Registry) Engines() []Engine {
	return append([]Engine(nil), r.engines...)
}

// Recommend tries each engine in priority order and returns the first non-empty
// result from an engine that CanSeed the given candidate.
func (r *Registry) Recommend(ctx context.Context, c engine.Candidate, n int, exclude []string) ([]engine.Candidate, error) {
	if r == nil {
		return nil, ErrNoEngine
	}
	for _, e := range r.engines {
		if !e.CanSeed(ctx, c) {
			continue
		}
		res, err := e.Recommend(ctx, c, n, exclude)
		if err != nil {
			continue
		}
		if len(res) > 0 {
			return res, nil
		}
	}
	return nil, ErrNoEngine
}

// CanSeed reports whether any registered engine can seed from c.
func (r *Registry) CanSeed(ctx context.Context, c engine.Candidate) bool {
	if r == nil {
		return false
	}
	for _, e := range r.engines {
		if e.CanSeed(ctx, c) {
			return true
		}
	}
	return false
}
