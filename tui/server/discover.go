package server

import (
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/source"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// Discovery endpoints: stations (YouTube Music's native radio) and
// recommendations (pixeltui's own engine, seeded from the shared library) —
// the same features the TUI's station/autoplay modes use.

var (
	radioCache = newTTLCache(10*time.Minute, 64)
	recsCache  = newTTLCache(30*time.Minute, 8)
)

// handleRadio returns a source-specific radio/station for a track.
// Query: id (source-prefixed), n (default 25), exclude (comma-separated artists
// to mute, same as the TUI's x key).
func (s *server) handleRadio(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var provider source.Provider
	var seed string
	var ok bool
	if s.cfg.Sources != nil {
		provider, _, ok = s.cfg.Sources.SourceFor(id)
		if ok {
			seed = source.StreamID(id)
		}
	}
	if !ok {
		kind, vid, ok2 := splitID(id)
		if ok2 && kind == "yt" {
			provider, seed, ok = nil, vid, true
		}
	}
	if !ok {
		http.Error(w, "radio needs a source-prefixed track id", http.StatusBadRequest)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 50 {
		n = 25
	}
	exclude := r.URL.Query().Get("exclude")
	key := seed + "|" + strconv.Itoa(n) + "|" + strings.ToLower(exclude)
	if v, ok := radioCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	var (
		cands []engine.Candidate
		err   error
	)
	if provider != nil {
		cands, err = provider.Radio(r.Context(), seed, n, nil)
	} else {
		cands, err = ytm.Radio(seed, n)
	}
	if err != nil {
		if errors.Is(err, source.ErrNotSupported) {
			http.Error(w, "radio not supported for this source", http.StatusNotImplemented)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Drop the seed if it appears first (YouTube watch playlists usually do).
	out := cands[:0]
	for _, c := range cands {
		if c.VideoID != seed {
			out = append(out, c)
		}
	}
	out = filterExcluded(out, exclude)
	resp := map[string]any{"tracks": s.toDTOsWithCaps(out)}
	radioCache.put(key, resp)
	writeJSON(w, resp)
}

// handleRecommend returns recommendations from the registered recommend engines.
// Seeds: explicit ?artist=&track= when given; otherwise up to 4 random liked
// tracks from the shared library (the TUI's blended-station behavior).
// Results are resolved to playable tracks. Query: artist, track, n.
func (s *server) handleRecommend(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Recommend == nil {
		http.Error(w, "recommendations are not configured on the server", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("n"))
	if n <= 0 || n > 40 {
		n = 20
	}

	var seeds []engine.Seed
	// Explicit multi-seed (seed=Artist|Track, repeated) — the client's
	// station/autoplay blend, mirroring the TUI's multi-seed stations.
	for _, sv := range q["seed"] {
		artist, track, _ := strings.Cut(sv, "|")
		if artist = strings.TrimSpace(artist); artist != "" {
			seeds = append(seeds, engine.Seed{Artist: artist, Track: strings.TrimSpace(track)})
		}
		if len(seeds) == 4 {
			break
		}
	}
	if a, t := q.Get("artist"), q.Get("track"); len(seeds) == 0 && (a != "" || t != "") {
		seeds = []engine.Seed{{Artist: a, Track: t}}
	} else if len(seeds) == 0 && s.cfg.Library != nil {
		liked := s.cfg.Library.Liked()
		rand.Shuffle(len(liked), func(i, j int) { liked[i], liked[j] = liked[j], liked[i] })
		for _, c := range liked {
			if c.Artist == "" {
				continue
			}
			seeds = append(seeds, engine.Seed{Artist: c.Artist, Track: c.Track})
			if len(seeds) == 4 {
				break
			}
		}
	}
	if len(seeds) == 0 {
		http.Error(w, "no seeds — like some tracks first", http.StatusNotFound)
		return
	}

	exclude := q.Get("exclude")
	key := strings.ToLower(seedsKey(seeds)) + "|" + strconv.Itoa(n) + "|" + strings.ToLower(exclude)
	if v, ok := recsCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	ctx := r.Context()
	seen := map[string]bool{}
	var all []engine.Candidate
	for _, seed := range seeds {
		c := engine.Candidate{Artist: seed.Artist, Track: seed.Track}
		if !s.cfg.Recommend.CanSeed(ctx, c) {
			continue
		}
		res, err := s.cfg.Recommend.Recommend(ctx, c, n/len(seeds)+4, nil)
		if err != nil {
			continue
		}
		for _, cand := range res {
			k := strings.ToLower(cand.Track + "|" + cand.Artist)
			if seen[k] {
				continue
			}
			seen[k] = true
			all = append(all, cand)
		}
	}
	all = filterExcluded(all, exclude)
	resolveToSongs(all)
	playable := all[:0]
	for _, c := range all {
		if c.VideoID != "" {
			playable = append(playable, c)
		}
	}
	if len(playable) > n {
		playable = playable[:n]
	}
	resp := map[string]any{"tracks": s.toDTOsWithCaps(playable)}
	recsCache.put(key, resp)
	writeJSON(w, resp)
}

func seedsKey(seeds []engine.Seed) string {
	parts := make([]string, len(seeds))
	for i, s := range seeds {
		parts[i] = s.Artist + "|" + s.Track
	}
	return strings.Join(parts, ";")
}

// resolveToSongs fills VideoID/art/duration for bare recommender candidates
// via YouTube Music search (concurrent; same approach as the charts remap).
func resolveToSongs(cands []engine.Candidate) {
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range cands {
		if cands[i].VideoID != "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c *engine.Candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			song, err := ytm.Resolve(c.Artist, c.Track)
			if err != nil || song.VideoID == "" {
				return
			}
			c.VideoID = song.VideoID
			c.ArtURL = song.ArtURL
			if song.DurationSec > 0 {
				c.DurationSec = song.DurationSec
			}
			if c.Album == "" {
				c.Album = song.Album
			}
		}(&cands[i])
	}
	wg.Wait()
}
