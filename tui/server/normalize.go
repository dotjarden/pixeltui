package server

import (
	"strings"
	"sync"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/library"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// Library normalization: playlists (and Liked) accumulate music-video ids —
// tracks queued from radio, charts before the remap, or older clients. Those
// entries play the MV cut and show 16:9 video thumbnails instead of album
// audio and square covers. After serving a playlist, the server resolves each
// YouTube entry to its album-song counterpart in the background (same
// confidence guard as the charts remap) and rewrites the M3U8, so the whole
// library converges on proper songs. Clients learn via the SSE library event.

var (
	normMu   sync.Mutex
	normDone = map[string]bool{} // playlist names normalized this process
)

// normalizeAsync kicks off a one-time (per process) background song-remap of
// the named playlist. tracks is the just-served snapshot.
func (s *server) normalizeAsync(name string, tracks []engine.Candidate) {
	if s.cfg.Library == nil || len(tracks) == 0 {
		return
	}
	normMu.Lock()
	done := normDone[name]
	normDone[name] = true
	normMu.Unlock()
	if done {
		return
	}

	// Only YouTube entries can be remapped; copy them so the resolve can't
	// race the response that was just written.
	var yt []engine.Candidate
	for _, c := range tracks {
		if c.VideoID != "" && c.Artist != "" {
			yt = append(yt, c)
		}
	}
	if len(yt) == 0 {
		return
	}

	go func() {
		fixed := make([]engine.Candidate, len(yt))
		copy(fixed, yt)
		ytm.RemapToSongs(fixed)

		changed := map[string]engine.Candidate{} // old videoID → song version
		for i := range yt {
			if fixed[i].VideoID != yt[i].VideoID {
				changed[yt[i].VideoID] = fixed[i]
			}
		}
		if len(changed) == 0 {
			return
		}

		// Re-load right before saving so concurrent edits aren't clobbered;
		// only the matched entries are touched.
		cur, err := s.cfg.Library.LoadPlaylist(name)
		if err != nil {
			return
		}
		n := 0
		for i := range cur {
			f, ok := changed[cur[i].VideoID]
			if !ok {
				continue
			}
			cur[i].VideoID = f.VideoID
			if f.ArtURL != "" {
				cur[i].ArtURL = f.ArtURL
			}
			if f.DurationSec > 0 {
				cur[i].DurationSec = f.DurationSec
			}
			if f.Album != "" {
				cur[i].Album = f.Album
			}
			n++
		}
		if n == 0 {
			return
		}
		if s.cfg.Library.SavePlaylist(name, cur) == nil {
			if strings.EqualFold(name, library.LikedName) {
				s.notifyLibrary("liked")
			} else {
				s.notifyLibrary("playlists")
			}
		}
	}()
}
