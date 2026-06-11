package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Daily mixes: the engine's answer to "made for you, by mood". Your most
// played artists (history + likes) are grouped by their dominant Last.fm tag;
// each of the top tags becomes a mix seeded from its artists — the same
// multi-seed blend as a TUI station, so affinity, discovery level, and
// per-artist caps all apply. Rebuilt at most once a day (cache below).

var mixesCache = newTTLCache(24*time.Hour, 2)

// handleMixes returns up to 4 tag-based mixes. Needs the recommender engine
// (Last.fm key) and the library; 503 otherwise.
func (s *server) handleMixes(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Rec == nil || s.cfg.Lastfm == nil {
		http.Error(w, "mixes need a Last.fm key on the server (pixeltui setup)", http.StatusServiceUnavailable)
		return
	}
	if s.cfg.Library == nil {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	if v, ok := mixesCache.get("mixes"); ok {
		writeJSON(w, v)
		return
	}

	// Most-played artists from the last 90 days, topped up with likes.
	plays := map[string]int{}
	names := map[string]string{} // lower → display
	cutoff := time.Now().AddDate(0, 0, -90)
	if listens, err := s.cfg.Library.Listens(0); err == nil {
		for _, l := range listens {
			if l.At.Before(cutoff) || l.Candidate.Artist == "" {
				continue
			}
			k := strings.ToLower(l.Candidate.Artist)
			plays[k]++
			names[k] = l.Candidate.Artist
		}
	}
	for _, c := range s.cfg.Library.Liked() {
		if c.Artist == "" {
			continue
		}
		k := strings.ToLower(c.Artist)
		plays[k] += 2 // a like outweighs a single play
		names[k] = c.Artist
	}
	if len(plays) == 0 {
		writeJSON(w, map[string]any{"mixes": []any{}})
		return
	}

	type ranked struct {
		name  string
		plays int
	}
	top := make([]ranked, 0, len(plays))
	for k, n := range plays {
		top = append(top, ranked{names[k], n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].plays > top[j].plays })
	if len(top) > 12 {
		top = top[:12]
	}

	// Dominant tag per artist (concurrent; Last.fm artist info is cheap and
	// the whole result caches for a day).
	type tagged struct {
		ranked
		tag string
	}
	tags := make([]tagged, len(top))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, a := range top {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, a ranked) {
			defer wg.Done()
			defer func() { <-sem }()
			tags[i] = tagged{ranked: a}
			if info, err := s.cfg.Lastfm.GetArtistInfo(a.name); err == nil && len(info.Tags) > 0 {
				tags[i].tag = strings.ToLower(info.Tags[0])
			}
		}(i, a)
	}
	wg.Wait()

	// Group by tag, rank tags by total plays.
	groups := map[string][]tagged{}
	for _, t := range tags {
		if t.tag != "" {
			groups[t.tag] = append(groups[t.tag], t)
		}
	}
	type cluster struct {
		tag     string
		artists []tagged
		weight  int
	}
	clusters := make([]cluster, 0, len(groups))
	for tag, as := range groups {
		c := cluster{tag: tag, artists: as}
		for _, a := range as {
			c.weight += a.plays
		}
		clusters = append(clusters, c)
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].weight > clusters[j].weight })
	if len(clusters) > 4 {
		clusters = clusters[:4]
	}

	type mixDTO struct {
		Title  string     `json:"title"`
		Tag    string     `json:"tag"`
		Tracks []trackDTO `json:"tracks"`
	}
	mixes := make([]mixDTO, 0, len(clusters))
	for _, c := range clusters {
		seeds := make([]engine.Seed, 0, 4)
		for _, a := range c.artists {
			seeds = append(seeds, engine.Seed{Artist: a.name})
			if len(seeds) == 4 {
				break
			}
		}
		cands, err := s.cfg.Rec.RecommendMulti(seeds, 25)
		if err != nil {
			continue
		}
		resolveToSongs(cands)
		playable := cands[:0]
		for _, cd := range cands {
			if cd.VideoID != "" {
				playable = append(playable, cd)
			}
		}
		if len(playable) < 5 {
			continue
		}
		mixes = append(mixes, mixDTO{
			Title:  mixTitle(c.tag),
			Tag:    c.tag,
			Tracks: toDTOs(playable),
		})
	}

	resp := map[string]any{"mixes": mixes}
	if len(mixes) > 0 { // an empty result may be a transient upstream failure
		mixesCache.put("mixes", resp)
	}
	writeJSON(w, resp)
}

// Genre stations: one mix for a caller-chosen Last.fm tag. Seeds come from
// the user's own listening when possible (same dominant-tag grouping as the
// daily mixes), falling back to Last.fm's tag chart for tags the user hasn't
// touched yet.

var stationCache = newTTLCache(time.Hour, 16)

// handleStation returns a station for ?tag=<last.fm tag>. Needs the
// recommender engine and a Last.fm key; 503 otherwise.
func (s *server) handleStation(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Rec == nil || s.cfg.Lastfm == nil {
		http.Error(w, "stations need a Last.fm key on the server (pixeltui setup)", http.StatusServiceUnavailable)
		return
	}
	tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	if tag == "" {
		http.Error(w, "missing tag", http.StatusBadRequest)
		return
	}
	if v, ok := stationCache.get(tag); ok {
		writeJSON(w, v)
		return
	}

	artists := s.tagArtistsFromLibrary(tag)
	if len(artists) < 2 {
		if names, err := s.cfg.Lastfm.TagTopArtists(tag, 8); err == nil {
			artists = names
		}
	}
	if len(artists) == 0 {
		http.Error(w, "no artists found for tag", http.StatusNotFound)
		return
	}

	seeds := make([]engine.Seed, 0, 4)
	for _, a := range artists {
		seeds = append(seeds, engine.Seed{Artist: a})
		if len(seeds) == 4 {
			break
		}
	}
	cands, err := s.cfg.Rec.RecommendMulti(seeds, 30)
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
	resp := map[string]any{"tag": tag, "tracks": toDTOs(playable)}
	if len(playable) > 0 { // an empty result may be a transient upstream failure
		stationCache.put(tag, resp)
	}
	writeJSON(w, resp)
}

// tagArtistsFromLibrary returns the user's most-played/liked artists whose
// dominant Last.fm tag matches (the daily-mixes approach), best first.
func (s *server) tagArtistsFromLibrary(tag string) []string {
	if s.cfg.Library == nil {
		return nil
	}
	plays := map[string]int{}
	names := map[string]string{} // lower → display
	cutoff := time.Now().AddDate(0, 0, -90)
	if listens, err := s.cfg.Library.Listens(0); err == nil {
		for _, l := range listens {
			if l.At.Before(cutoff) || l.Candidate.Artist == "" {
				continue
			}
			k := strings.ToLower(l.Candidate.Artist)
			plays[k]++
			names[k] = l.Candidate.Artist
		}
	}
	for _, c := range s.cfg.Library.Liked() {
		if c.Artist == "" {
			continue
		}
		k := strings.ToLower(c.Artist)
		plays[k] += 2 // a like outweighs a single play
		names[k] = c.Artist
	}

	type ranked struct {
		name  string
		plays int
	}
	top := make([]ranked, 0, len(plays))
	for k, n := range plays {
		top = append(top, ranked{names[k], n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].plays > top[j].plays })
	if len(top) > 12 {
		top = top[:12]
	}

	// Dominant tag per artist (concurrent), keep the ones matching the tag.
	matched := make([]string, len(top))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, a := range top {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			if info, err := s.cfg.Lastfm.GetArtistInfo(name); err == nil &&
				len(info.Tags) > 0 && strings.EqualFold(info.Tags[0], tag) {
				matched[i] = name
			}
		}(i, a.name)
	}
	wg.Wait()
	out := matched[:0]
	for _, name := range matched {
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// mixTitle turns a Last.fm tag into a mix name: "indie rock" → "Indie Rock Mix".
func mixTitle(tag string) string {
	words := strings.Fields(tag)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ") + " Mix"
}
