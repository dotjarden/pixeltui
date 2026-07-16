package scrobble

import (
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// LastfmTarget wraps a Last.fm client as a scrobble.Target.
type LastfmTarget struct {
	client *Lastfm
}

// NewLastfmTarget creates a Last.fm scrobble target. Returns nil if the client
// is nil or unauthorized.
func NewLastfmTarget(client *Lastfm) *LastfmTarget {
	if client == nil {
		return nil
	}
	return &LastfmTarget{client: client}
}

// Name returns the target identifier.
func (LastfmTarget) Name() string { return "lastfm" }

// NowPlaying announces the current track to Last.fm.
func (t *LastfmTarget) NowPlaying(c engine.Candidate) {
	if t == nil || t.client == nil {
		return
	}
	_ = t.client.UpdateNowPlaying(c.Artist, c.Track, c.Album, c.DurationSec)
}

// Scrobble submits a completed play to Last.fm.
func (t *LastfmTarget) Scrobble(c engine.Candidate, startedAt time.Time) error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Scrobble(c.Artist, c.Track, c.Album, c.DurationSec, startedAt)
}

// Love marks a track as loved on Last.fm.
func (t *LastfmTarget) Love(c engine.Candidate) {
	if t == nil || t.client == nil || c.Artist == "" || c.Track == "" {
		return
	}
	_ = t.client.Love(c.Artist, c.Track)
}
