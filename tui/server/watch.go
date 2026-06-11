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
func (s *server) watchLibrary() {
	if s.cfg.Library == nil {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "library")
	last := libraryStamp(dir)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for range tick.C {
		if cur := libraryStamp(dir); cur != last {
			last = cur
			s.notifyLibrary("library")
		}
	}
}

// libraryStamp folds the library tree's file names, sizes, and mtimes into a
// single comparable value.
func libraryStamp(dir string) int64 {
	var stamp int64
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr — unreadable entries just don't count
		}
		stamp += info.ModTime().UnixNano() + info.Size() + int64(len(info.Name()))
		return nil
	})
	return stamp
}
