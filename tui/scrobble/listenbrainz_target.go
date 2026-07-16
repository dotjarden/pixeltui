package scrobble

import (
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ListenBrainzTarget wraps a ListenBrainz client as a scrobble.Target.
type ListenBrainzTarget struct {
	client *ListenBrainz
}

// NewListenBrainzTarget creates a ListenBrainz scrobble target. Returns nil if
// the client is nil.
func NewListenBrainzTarget(client *ListenBrainz) *ListenBrainzTarget {
	if client == nil {
		return nil
	}
	return &ListenBrainzTarget{client: client}
}

// Name returns the target identifier.
func (ListenBrainzTarget) Name() string { return "listenbrainz" }

// NowPlaying announces the current track to ListenBrainz.
func (t *ListenBrainzTarget) NowPlaying(c engine.Candidate) {
	if t == nil || t.client == nil {
		return
	}
	_ = t.client.PlayingNow(c.Artist, c.Track, c.Album, c.DurationSec)
}

// Scrobble submits a completed play to ListenBrainz.
func (t *ListenBrainzTarget) Scrobble(c engine.Candidate, startedAt time.Time) error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Listen(c.Artist, c.Track, c.Album, c.DurationSec, startedAt)
}

// Love is a no-op for ListenBrainz (the service has no equivalent).
func (ListenBrainzTarget) Love(_ engine.Candidate) {}
