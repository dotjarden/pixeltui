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

// SpoolerTarget is a scrobble.Target that records listens to a local spool
// file for offline play-counting or later submission. It never fails.
type SpoolerTarget struct {
	spoolPath string
	mu        sync.Mutex
}

// NewSpoolerTarget creates a local-file scrobble target. dataDir hosts the
// offline spool under library/.
func NewSpoolerTarget(dataDir string) *SpoolerTarget {
	return &SpoolerTarget{
		spoolPath: filepath.Join(dataDir, "library", "scrobble-spool.jsonl"),
	}
}

// Name returns the target identifier.
func (*SpoolerTarget) Name() string { return "spool" }

// NowPlaying is a no-op for the local spool.
func (*SpoolerTarget) NowPlaying(_ engine.Candidate) {}

// Love is a no-op for the local spool.
func (*SpoolerTarget) Love(_ engine.Candidate) {}

// Scrobble appends one completed play to the spool file.
func (t *SpoolerTarget) Scrobble(c engine.Candidate, startedAt time.Time) error {
	if t == nil {
		return nil
	}
	p := pendingScrobble{
		Artist:    c.Artist,
		Track:     c.Track,
		Album:     c.Album,
		Duration:  c.DurationSec,
		StartedAt: startedAt.Unix(),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(t.spoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// maxSpool caps the offline backlog so the spool can't grow without bound.
const maxSpool = 500

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

// readSpoolLocked loads spooled entries (most recent maxSpool). Caller holds mu.
func readSpoolLocked(path string) []pendingScrobble {
	f, err := os.Open(path)
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
