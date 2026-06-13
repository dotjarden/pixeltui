package server

import (
	"os"
	"path/filepath"
	"time"
)

// watchLibrary polls the library files' mtimes and broadcasts an SSE
// "library" event when anything changes. Server-side writes notify directly;
// this catches the TUI (and anything else) writing the M3U8s/history file —
// the cheap, dependency-free version of a file watcher, at a cadence that's
// plenty for "my like showed up on my phone".
//
// History appends are tracked separately: a play landing in history.jsonl
// broadcasts the "history" hint (clients update Recently Played with one
// cheap fetch), while likes/playlists/library files broadcast "library"
// (clients resync the catalog). Without the split, every reported play —
// including the reporting device's own — looked like a full library change.
func (s *server) watchLibrary() {
	if s.cfg.Library == nil {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "library")
	lastHist, lastRest := libraryStamps(dir)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for range tick.C {
		hist, rest := libraryStamps(dir)
		if rest != lastRest {
			lastHist, lastRest = hist, rest
			s.notifyLibrary("library")
		} else if hist != lastHist {
			lastHist = hist
			s.notifyLibrary("history")
		}
	}
}

// libraryStamps folds the library tree's file names, sizes, and mtimes into
// two comparable values: one for history.jsonl, one for everything else.
func libraryStamps(dir string) (history, rest int64) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr — unreadable entries just don't count
		}
		v := info.ModTime().UnixNano() + info.Size() + int64(len(info.Name()))
		if info.Name() == "history.jsonl" {
			history += v
		} else {
			rest += v
		}
		return nil
	})
	return history, rest
}
