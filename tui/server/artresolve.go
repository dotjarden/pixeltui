package server

import (
	"strings"
	"sync"

	"github.com/dotjarden/pixeltui/tui/ytm"
)

// Art resolution: library and history entries often carry no artwork (M3U8
// lines without art=, ListenBrainz history lines), and the derivable
// i.ytimg.com video thumbnail is a letterboxed video frame, not the official
// album cover. The resolver looks up the proper square cover — YouTube
// Music's song result first, then Last.fm's album art when a key is
// configured — caches it, and the serving handlers swap it in. Library
// normalization (normalize.go) makes the upgrade permanent for likes and
// playlists; history entries get it from this cache on every read.

// maxArtCache bounds the in-memory resolved-art map.
const maxArtCache = 4000

type artResolver struct {
	mu      sync.Mutex
	art     map[string]string // "artist|track" → official art URL ("" = none found)
	pending map[string]struct{}
	sem     chan struct{} // caps concurrent upstream lookups
}

func newArtResolver() *artResolver {
	return &artResolver{
		art:     map[string]string{},
		pending: map[string]struct{}{},
		sem:     make(chan struct{}, 4),
	}
}

func artKey(artist, track string) string {
	return strings.ToLower(strings.TrimSpace(artist) + "|" + strings.TrimSpace(track))
}

// isVideoThumb reports whether an art URL is a YouTube video thumbnail (the
// letterboxed frame) rather than official cover art.
func isVideoThumb(u string) bool { return strings.Contains(u, "i.ytimg.com/") }

// fillResolvedArt upgrades tracks whose art is missing or a video thumbnail
// to the official cover when the resolver knows it, queueing a background
// lookup otherwise (so the next read has it).
func (s *server) fillResolvedArt(ds []trackDTO) {
	if s.artRes == nil { // tests construct server{} directly
		return
	}
	for i := range ds {
		d := &ds[i]
		if d.Source != "youtube" || d.Artist == "" || d.Track == "" {
			continue
		}
		if d.Art != "" && !isVideoThumb(d.Art) {
			continue // already official
		}
		if art, ok := s.lookupArt(d.Artist, d.Track); ok {
			if art != "" {
				d.Art = art
			}
		} else {
			s.queueArtResolve(d.Artist, d.Track)
		}
	}
}

// lookupArt returns the cached resolution for a track; ok=false means not
// yet resolved (a lookup may be in flight).
func (s *server) lookupArt(artist, track string) (string, bool) {
	if s.artRes == nil {
		return "", true // "resolved, nothing found" — callers won't queue
	}
	s.artRes.mu.Lock()
	defer s.artRes.mu.Unlock()
	art, ok := s.artRes.art[artKey(artist, track)]
	return art, ok
}

// queueArtResolve resolves a track's official art in the background:
// YouTube Music song result first (square cover), Last.fm album art as the
// fallback when a key is configured. Failures cache as "" so misses don't
// re-query upstream on every read.
func (s *server) queueArtResolve(artist, track string) {
	r := s.artRes
	if r == nil {
		return
	}
	key := artKey(artist, track)
	r.mu.Lock()
	if _, done := r.art[key]; done {
		r.mu.Unlock()
		return
	}
	if _, inFlight := r.pending[key]; inFlight {
		r.mu.Unlock()
		return
	}
	r.pending[key] = struct{}{}
	r.mu.Unlock()

	go func() {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()

		art := ""
		if song, err := ytm.Resolve(artist, track); err == nil && !isVideoThumb(song.ArtURL) {
			art = song.ArtURL
		}
		if art == "" && s.cfg.Lastfm != nil {
			if a, err := s.cfg.Lastfm.TrackAlbumArt(artist, track); err == nil {
				art = a
			}
		}

		r.mu.Lock()
		if len(r.art) >= maxArtCache { // crude bound; resets rarely
			r.art = map[string]string{}
		}
		r.art[key] = art
		delete(r.pending, key)
		r.mu.Unlock()
	}()
}
