package identify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// LocalIndex fingerprints the user's own library and matches incoming snippets
// against it. It is fully offline and private.
type LocalIndex struct {
	dataDir string
	mu      sync.RWMutex
	entries []indexEntry
	loaded  bool
}

type indexEntry struct {
	ID          string  `json:"id"`
	Artist      string  `json:"artist"`
	Track       string  `json:"track"`
	Album       string  `json:"album,omitempty"`
	DurationSec int     `json:"duration_sec"`
	Fingerprint []int32 `json:"fingerprint"`
	Path        string  `json:"path,omitempty"`
}

// NewLocalIndex creates a local fingerprint index rooted at dataDir.
func NewLocalIndex(dataDir string) *LocalIndex {
	return &LocalIndex{dataDir: dataDir}
}

func (l *LocalIndex) Name() string { return "local" }

func (l *LocalIndex) Available() bool {
	return l != nil
}

// Identify returns the best local match for the given fingerprint.
func (l *LocalIndex) Identify(ctx context.Context, fingerprint []int32, durationSec int) (Result, error) {
	if err := l.load(); err != nil {
		return Result{}, err
	}
	l.mu.RLock()
	entries := append([]indexEntry(nil), l.entries...)
	l.mu.RUnlock()

	// The incoming fingerprint is a short clip (e.g. 10 s); the reference is a
	// whole track. The reference only needs to be long enough to contain the
	// clip — there is no upper bound, so a 10 s clip can match a 4-minute track.
	// (The old abs(delta) check compared clip length to full-track length and
	// rejected everything.)
	const toleranceSec = 2
	var best indexEntry
	bestScore := 0.0
	for _, e := range entries {
		if e.DurationSec != 0 && e.DurationSec < durationSec-toleranceSec {
			continue
		}
		score := compareFingerprints(fingerprint, e.Fingerprint)
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	if bestScore <= 0 {
		return Result{}, ErrNoMatch
	}
	return Result{
		Source: l.Name(),
		Score:  bestScore,
		Candidate: engine.Candidate{
			Artist:      best.Artist,
			Track:       best.Track,
			Album:       best.Album,
			DurationSec: best.DurationSec,
			Source:      sourceForID(best.ID),
			Path:        best.Path,
			VideoID:     videoIDFromID(best.ID),
			ArtURL:      artURLForID(best.ID),
		},
	}, nil
}

// Add fingerprints one track and appends it to the index. It re-computes the
// fingerprint if fp is empty.
func (l *LocalIndex) Add(ctx context.Context, id, artist, track, album string, path string, fp Fingerprint) error {
	if len(fp.Data) == 0 && path != "" {
		var err error
		fp, err = ComputeFingerprint(ctx, path)
		if err != nil {
			return err
		}
	}
	if len(fp.Data) == 0 {
		return fmt.Errorf("no fingerprint for %s", id)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, indexEntry{
		ID:          id,
		Artist:      artist,
		Track:       track,
		Album:       album,
		DurationSec: fp.DurationSec,
		Fingerprint: fp.Data,
		Path:        path,
	})
	return l.saveLocked()
}

// IndexPath returns the local fingerprint store path.
func (l *LocalIndex) IndexPath() string {
	return filepath.Join(l.dataDir, "identify", "fingerprints.jsonl")
}

func (l *LocalIndex) load() error {
	l.mu.RLock()
	if l.loaded {
		l.mu.RUnlock()
		return nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loaded {
		return nil
	}
	path := l.IndexPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			l.loaded = true
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e indexEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			l.entries = append(l.entries, e)
		}
	}
	l.loaded = true
	return sc.Err()
}

func (l *LocalIndex) saveLocked() error {
	path := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range l.entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}

func sourceForID(id string) string {
	if len(id) > 3 && id[2] == ':' {
		switch id[:2] {
		case "yt":
			return "youtube"
		case "lo":
			return "local"
		case "su":
			return "subsonic"
		}
	}
	return ""
}

func videoIDFromID(id string) string {
	if len(id) > 3 && id[:3] == "yt:" {
		return id[3:]
	}
	return ""
}

func artURLForID(id string) string {
	if vid := videoIDFromID(id); vid != "" {
		return "https://img.youtube.com/vi/" + vid + "/0.jpg"
	}
	return ""
}
