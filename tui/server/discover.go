package server

import (
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// Discovery endpoints: stations (YouTube Music's native radio) and
// recommendations (pixeltui's own engine, seeded from the shared library) —
// the same features the TUI's station/autoplay modes use.

var (
	radioCache = newTTLCache(10*time.Minute, 64)
	recsCache  = newTTLCache(30*time.Minute, 8)
)

// handleRadio returns YouTube Music's watch-playlist radio for a track —
// the seed for stations and autoplay queueing.
// Query: id (yt:<videoID>), n (default 25).
func (s *server) handleRadio(w http.ResponseWriter, r *http.Request) {
	kind, vid, ok := splitID(r.URL.Query().Get("id"))
	if !ok || kind != "yt" {
		http.Error(w, "radio needs a yt: track id", http.StatusBadRequest)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 50 {
		n = 25
	}
	key := vid + "|" + strconv.Itoa(n)
	if v, ok := radioCache.get(key); ok {
		writeJSON(w, v)
		return
	}
	cands, err := ytm.Radio(vid, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// The seed video itself usually leads the watch playlist — drop it.
	out := cands[:0]
	for _, c := range cands {
		if c.VideoID != vid {
			out = append(out, c)
		}
	}
	resp := map[string]any{"tracks": toDTOs(out)}
	radioCache.put(key, resp)
	writeJSON(w, resp)
}

// handleRecommend returns recommendations from pixeltui's own engine.
// Seeds: explicit ?artist=&track= when given; otherwise up to 4 random liked
// tracks from the shared library (the TUI's blended-station behavior).
// Results are resolved to playable YouTube tracks. Query: artist, track, n.
func (s *server) handleRecommend(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Rec == nil {
		http.Error(w, "recommendations need a Last.fm key on the server (pixeltui setup)", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("n"))
	if n <= 0 || n > 40 {
		n = 20
	}

	var seeds []engine.Seed
	if a, t := q.Get("artist"), q.Get("track"); a != "" || t != "" {
		seeds = []engine.Seed{{Artist: a, Track: t}}
	} else if s.cfg.Library != nil {
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

	key := strings.ToLower(seedsKey(seeds)) + "|" + strconv.Itoa(n)
	if v, ok := recsCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	cands, err := s.cfg.Rec.RecommendMulti(seeds, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resolveToSongs(cands)
	playable := cands[:0]
	for _, c := range cands {
		if c.VideoID != "" {
			playable = append(playable, c)
		}
	}
	resp := map[string]any{"tracks": toDTOs(playable)}
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
