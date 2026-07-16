package scrobble

import (
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Scrobbler fans a play out to every configured scrobbling Target (Last.fm,
// ListenBrainz, a local spool, etc.). All submission methods are asynchronous
// and never block the caller; targets that need retry logic should implement it
// internally.
type Scrobbler struct {
	registry *Registry
}

// New builds a Scrobbler from a registry. Returns nil when no target is
// configured. This keeps backwards compatibility with callers that built
// Scrobbler from individual Last.fm/ListenBrainz clients.
func New(reg *Registry) *Scrobbler {
	if reg == nil || len(reg.Targets()) == 0 {
		return nil
	}
	return &Scrobbler{registry: reg}
}

// Targets describes the configured services, for status/doctor output.
func (s *Scrobbler) Targets() string {
	if s == nil || s.registry == nil {
		return ""
	}
	var names []string
	for _, t := range s.registry.Targets() {
		names = append(names, t.Name())
	}
	return strings.Join(names, " + ")
}

// NowPlaying announces the current track to all services. Fire-and-forget:
// returns immediately, errors are dropped (now-playing is ephemeral).
func (s *Scrobbler) NowPlaying(c engine.Candidate) {
	if s == nil || s.registry == nil {
		return
	}
	s.registry.NowPlaying(c)
}

// Love marks a liked track as loved on every service that supports it.
func (s *Scrobbler) Love(c engine.Candidate) {
	if s == nil || s.registry == nil {
		return
	}
	s.registry.Love(c)
}

// Scrobble submits one qualified play (caller enforces the 50%/4-minute rule).
// Asynchronous; persistent targets handle their own retry/spool logic.
func (s *Scrobbler) Scrobble(c engine.Candidate, startedAt time.Time) {
	if s == nil || s.registry == nil {
		return
	}
	s.registry.Scrobble(c, startedAt)
}

// RetrySpool is a no-op in the new registry model. Individual targets that
// spool to disk manage their own retry loops. It is kept for backwards
// compatibility with callers that started `go scrob.RetrySpool()`.
func (s *Scrobbler) RetrySpool() {}
