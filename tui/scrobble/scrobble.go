package scrobble

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Scrobbler fans a play out to every configured service (Last.fm and/or
// ListenBrainz). All submission methods are asynchronous and never block the
// caller; failed scrobbles are spooled to disk and retried later, so listens
// made offline aren't lost.
type Scrobbler struct {
	lastfm *Lastfm       // nil = not configured
	lb     *ListenBrainz // nil = not configured

	spoolPath string
	mu        sync.Mutex // guards the spool file
}

// maxSpool caps the offline backlog so the spool can't grow without bound.
const maxSpool = 500

// New builds a Scrobbler from whichever clients are non-nil. dataDir hosts the
// offline spool (under library/). Returns nil when no service is configured.
func New(lastfm *Lastfm, lb *ListenBrainz, dataDir string) *Scrobbler {
	if lastfm == nil && lb == nil {
		return nil
	}
	return &Scrobbler{
		lastfm:    lastfm,
		lb:        lb,
		spoolPath: filepath.Join(dataDir, "library", "scrobble-spool.jsonl"),
	}
}

// Targets describes the configured services, for status/doctor output.
func (s *Scrobbler) Targets() string {
	var t []string
	if s.lastfm != nil {
		t = append(t, "Last.fm")
	}
	if s.lb != nil {
		t = append(t, "ListenBrainz")
	}
	return strings.Join(t, " + ")
}

// pendingScrobble is one spooled (not yet delivered) play.
type pendingScrobble struct {
	Artist    string `json:"artist"`
	Track     string `json:"track"`
	Album     string `json:"album,omitempty"`
	Duration  int    `json:"duration_sec,omitempty"`
	StartedAt int64  `json:"started_at"`
	// Per-service delivery flags so a partial failure only retries the
	// service that missed.
	NeedLastfm bool `json:"need_lastfm"`
	NeedLB     bool `json:"need_lb"`
}

// NowPlaying announces the current track to all services. Fire-and-forget:
// returns immediately, errors are dropped (now-playing is ephemeral).
func (s *Scrobbler) NowPlaying(c engine.Candidate) {
	if s == nil {
		return
	}
	go func() {
		if s.lastfm != nil {
			s.lastfm.UpdateNowPlaying(c.Artist, c.Track, c.Album, c.DurationSec) //nolint:errcheck
		}
		if s.lb != nil {
			s.lb.PlayingNow(c.Artist, c.Track, c.Album, c.DurationSec) //nolint:errcheck
		}
	}()
}

// Scrobble submits one qualified play (caller enforces the 50%/4-minute rule).
// Asynchronous; failures are spooled and retried by the next RetrySpool.
func (s *Scrobbler) Scrobble(c engine.Candidate, startedAt time.Time) {
	if s == nil || (c.Artist == "" && c.Track == "") {
		return
	}
	p := pendingScrobble{
		Artist:     c.Artist,
		Track:      c.Track,
		Album:      c.Album,
		Duration:   c.DurationSec,
		StartedAt:  startedAt.Unix(),
		NeedLastfm: s.lastfm != nil,
		NeedLB:     s.lb != nil,
	}
	go func() {
		if rest := s.deliver(p); rest != nil {
			s.spool(*rest)
		}
	}()
}

// deliver attempts each still-needed service. It returns nil when everything
// succeeded, else the pending entry with the failed services still flagged.
func (s *Scrobbler) deliver(p pendingScrobble) *pendingScrobble {
	at := time.Unix(p.StartedAt, 0)
	if p.NeedLastfm && s.lastfm != nil {
		if err := s.lastfm.Scrobble(p.Artist, p.Track, p.Album, p.Duration, at); err == nil {
			p.NeedLastfm = false
		}
	}
	if p.NeedLB && s.lb != nil {
		if err := s.lb.Listen(p.Artist, p.Track, p.Album, p.Duration, at); err == nil {
			p.NeedLB = false
		}
	}
	if p.NeedLastfm || p.NeedLB {
		return &p
	}
	return nil
}

// spool appends a failed scrobble for a later retry.
func (s *Scrobbler) spool(p pendingScrobble) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.spoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n')) //nolint:errcheck
}

// RetrySpool re-attempts every spooled scrobble, rewriting the spool with
// whatever still fails (bounded to the most recent maxSpool entries).
// Synchronous — call from a goroutine (e.g. once at startup).
func (s *Scrobbler) RetrySpool() {
	if s == nil {
		return
	}
	s.mu.Lock()
	pending := s.readSpoolLocked()
	if len(pending) == 0 {
		s.mu.Unlock()
		return
	}
	// Truncate now so scrobbles spooled while we retry aren't lost or doubled.
	os.Remove(s.spoolPath) //nolint:errcheck
	s.mu.Unlock()

	var failed []pendingScrobble
	for _, p := range pending {
		if rest := s.deliver(p); rest != nil {
			failed = append(failed, *rest)
		}
	}
	for _, p := range failed {
		s.spool(p)
	}
}

// readSpoolLocked loads spooled entries (most recent maxSpool). Caller holds mu.
func (s *Scrobbler) readSpoolLocked() []pendingScrobble {
	f, err := os.Open(s.spoolPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []pendingScrobble
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var p pendingScrobble
		if json.Unmarshal([]byte(line), &p) == nil {
			out = append(out, p)
		}
	}
	if len(out) > maxSpool {
		out = out[len(out)-maxSpool:]
	}
	return out
}
