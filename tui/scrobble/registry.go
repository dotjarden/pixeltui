package scrobble

import (
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Target is one scrobbling destination. Implementations wrap Last.fm,
// ListenBrainz, Libre.fm, Maloja, a local play-count file, etc.
type Target interface {
	// Name returns a short identifier, e.g. "lastfm", "listenbrainz".
	Name() string

	// NowPlaying announces the current track (best-effort, fire-and-forget).
	NowPlaying(c engine.Candidate)

	// Scrobble submits one completed play that started at startedAt.
	Scrobble(c engine.Candidate, startedAt time.Time) error

	// Love marks a track as loved, if the service supports it.
	Love(c engine.Candidate)
}

// Registry holds scrobbling targets and fans plays out to all of them.
// Zero value is usable but empty.
type Registry struct {
	targets []Target
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a target.
func (r *Registry) Register(t Target) {
	r.targets = append(r.targets, t)
}

// Targets returns the registered targets.
func (r *Registry) Targets() []Target {
	return append([]Target(nil), r.targets...)
}

// NowPlaying announces the current track to every target, asynchronously.
func (r *Registry) NowPlaying(c engine.Candidate) {
	if r == nil {
		return
	}
	for _, t := range r.targets {
		go t.NowPlaying(c)
	}
}

// Scrobble submits a completed play to every target. Errors are ignored by
// the registry; persistent targets should spool failures internally.
func (r *Registry) Scrobble(c engine.Candidate, startedAt time.Time) {
	if r == nil {
		return
	}
	for _, t := range r.targets {
		go func(t Target) {
			_ = t.Scrobble(c, startedAt)
		}(t)
	}
}

// Love marks a track as loved on every target that supports it.
func (r *Registry) Love(c engine.Candidate) {
	if r == nil {
		return
	}
	for _, t := range r.targets {
		go t.Love(c)
	}
}
