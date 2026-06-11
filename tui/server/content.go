package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/lyrics"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// Content endpoints: lyrics, charts, and full artist/album pages — the same
// data the TUI renders, shaped for the companion app.

// ── tiny TTL cache ──────────────────────────────────────────────────────────

// ttlCache is a small bounded TTL map for content responses (lyrics never
// change; charts/artist pages change slowly). Oldest entries are evicted
// once cap is reached.
type ttlCache struct {
	mu  sync.Mutex
	ttl time.Duration
	cap int
	m   map[string]ttlEntry
}

type ttlEntry struct {
	v   any
	at  time.Time
	key string
}

func newTTLCache(ttl time.Duration, capacity int) *ttlCache {
	return &ttlCache{ttl: ttl, cap: capacity, m: map[string]ttlEntry{}}
}

func (c *ttlCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Since(e.at) > c.ttl {
		return nil, false
	}
	return e.v, true
}

func (c *ttlCache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.cap {
		oldestKey, oldest := "", time.Now()
		for k, e := range c.m {
			if e.at.Before(oldest) {
				oldestKey, oldest = k, e.at
			}
		}
		delete(c.m, oldestKey)
	}
	c.m[key] = ttlEntry{v: v, at: time.Now(), key: key}
}

var (
	lyricsCache = newTTLCache(24*time.Hour, 200)
	chartsCache = newTTLCache(30*time.Minute, 16)
	artistCache = newTTLCache(time.Hour, 64)
	albumCache  = newTTLCache(24*time.Hour, 64)
	searchCache = newTTLCache(10*time.Minute, 64)
)

// ── lyrics ──────────────────────────────────────────────────────────────────

type lyricLine struct {
	T    float64 `json:"t"` // seconds from track start
	Text string  `json:"text"`
}

// handleLyrics returns synced (LRCLIB) or plain lyrics for a track.
// Query: artist, track, duration (sec, optional), id (yt:<vid>, optional —
// enables the YouTube Music plain-text fallback).
func (s *server) handleLyrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	artist, track := q.Get("artist"), q.Get("track")
	if artist == "" && track == "" {
		http.Error(w, "missing artist/track", http.StatusBadRequest)
		return
	}
	durSec, _ := strconv.Atoi(q.Get("duration"))
	key := strings.ToLower(artist + "|" + track)
	if v, ok := lyricsCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	out := map[string]any{"synced": []lyricLine{}, "plain": ""}
	found := false
	if res, err := lyrics.Fetch(artist, track, "", durSec); err == nil && !res.Empty() {
		lines := make([]lyricLine, 0, len(res.Synced))
		for _, l := range res.Synced {
			lines = append(lines, lyricLine{T: l.T, Text: l.Text})
		}
		out["synced"] = lines
		out["plain"] = res.Plain
		found = true
	} else if kind, vid, ok := splitID(q.Get("id")); ok && kind == "yt" {
		if text, err := ytm.Lyrics(vid); err == nil && text != "" {
			out["plain"] = text
			found = true
		}
	}
	// Only cache hits: an empty result may be a transient upstream failure
	// (LRCLIB blip), and caching it would blank this track's lyrics for the
	// whole TTL across every client.
	if found {
		lyricsCache.put(key, out)
	}
	writeJSON(w, out)
}

// ── charts ──────────────────────────────────────────────────────────────────

// handleCharts returns the current YouTube Music top tracks.
// Query: country (2-letter code; empty/ZZ = global).
func (s *server) handleCharts(w http.ResponseWriter, r *http.Request) {
	country := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country")))
	if country == "" {
		country = "ZZ"
	}
	if v, ok := chartsCache.get(country); ok {
		writeJSON(w, v)
		return
	}
	cs, err := ytm.Charts(country, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := map[string]any{"tracks": toDTOs(cs), "country": country}
	chartsCache.put(country, out)
	writeJSON(w, out)
}

// ── entity search (artists + albums in one call) ───────────────────────────

// handleSearchEntities returns artist and album entities for a query — the
// rails next to track results in the app's search page.
// Query: q.
func (s *server) handleSearchEntities(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	key := "ent|" + strings.ToLower(q)
	if v, ok := searchCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	// Both lookups in parallel — they're independent upstream calls.
	var (
		artists []ytm.ArtistHit
		albums  []ytm.Album
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); artists, _ = ytm.SearchArtists(q, 8) }()
	go func() { defer wg.Done(); albums, _ = ytm.SearchAlbums(q, 10) }()
	wg.Wait()

	type artistHitDTO struct {
		Name string `json:"name"`
		Art  string `json:"art,omitempty"`
	}
	hits := make([]artistHitDTO, 0, len(artists))
	for _, a := range artists {
		hits = append(hits, artistHitDTO{Name: a.Name, Art: a.ArtURL})
	}
	out := map[string]any{"artists": hits, "albums": toAlbumDTOs(albums)}
	searchCache.put(key, out)
	writeJSON(w, out)
}

// ── artist page ─────────────────────────────────────────────────────────────

type albumDTO struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Year     string `json:"year,omitempty"`
	BrowseID string `json:"browse_id"`
	Art      string `json:"art,omitempty"`
}

// handleArtist returns a full artist page: top songs, albums, singles, and
// Last.fm listener stats when a key is configured.
// Query: name (artist name to resolve).
func (s *server) handleArtist(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	key := strings.ToLower(name)
	if v, ok := artistCache.get(key); ok {
		writeJSON(w, v)
		return
	}

	hits, err := ytm.SearchArtists(name, 1)
	if err != nil || len(hits) == 0 {
		http.Error(w, "artist not found", http.StatusNotFound)
		return
	}

	// Last.fm stats fetch runs while the page browse is in flight.
	type stats struct {
		Listeners int      `json:"listeners,omitempty"`
		Playcount int      `json:"playcount,omitempty"`
		Tags      []string `json:"tags,omitempty"`
		Bio       string   `json:"bio,omitempty"`
	}
	statsCh := make(chan *stats, 1)
	go func() {
		if s.cfg.Lastfm == nil {
			statsCh <- nil
			return
		}
		info, err := s.cfg.Lastfm.GetArtistInfo(hits[0].Name)
		if err != nil {
			statsCh <- nil
			return
		}
		tags := info.Tags
		if len(tags) > 4 {
			tags = tags[:4]
		}
		statsCh <- &stats{Listeners: info.Listeners, Playcount: info.Playcount,
			Tags: tags, Bio: info.Summary}
	}()

	page, err := ytm.BrowseArtist(hits[0].BrowseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if page.Name == "" {
		page.Name = hits[0].Name
	}

	out := map[string]any{
		"name":      page.Name,
		"top_songs": toDTOs(page.TopSongs),
		"albums":    toAlbumDTOs(page.Albums),
		"singles":   toAlbumDTOs(page.Singles),
	}
	if st := <-statsCh; st != nil {
		out["stats"] = st
	}
	artistCache.put(key, out)
	writeJSON(w, out)
}

// ── album page ──────────────────────────────────────────────────────────────

// handleAlbum returns an album's ordered tracks + metadata.
// Query: browse_id (from an artist page / album search), title, artist.
func (s *server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	browseID := q.Get("browse_id")
	title, artist := q.Get("title"), q.Get("artist")

	// No browse id → resolve via album search (e.g. "go to album" on a track).
	if browseID == "" {
		if title == "" {
			http.Error(w, "missing browse_id or title", http.StatusBadRequest)
			return
		}
		hits, err := ytm.SearchAlbums(title+" "+artist, 5)
		if err != nil || len(hits) == 0 {
			http.Error(w, "album not found", http.StatusNotFound)
			return
		}
		pick := 0
		for i, a := range hits {
			if strings.EqualFold(strings.TrimSpace(a.Title), strings.TrimSpace(title)) {
				pick = i
				break
			}
		}
		browseID = hits[pick].BrowseID
		title, artist = hits[pick].Title, hits[pick].Artist
	}

	if v, ok := albumCache.get(browseID); ok {
		writeJSON(w, v)
		return
	}
	detail, err := ytm.BrowseAlbum(ytm.Album{Title: title, Artist: artist, BrowseID: browseID}, 60)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := map[string]any{
		"title":  detail.Album.Title,
		"artist": detail.Album.Artist,
		"year":   detail.Album.Year,
		"art":    detail.ArtURL,
		"tracks": toDTOs(detail.Tracks),
	}
	albumCache.put(browseID, out)
	writeJSON(w, out)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func toDTOs(cs []engine.Candidate) []trackDTO {
	out := make([]trackDTO, 0, len(cs))
	for _, c := range cs {
		if d, ok := toDTO(c); ok {
			out = append(out, d)
		}
	}
	return out
}

func toAlbumDTOs(as []ytm.Album) []albumDTO {
	out := make([]albumDTO, 0, len(as))
	for _, a := range as {
		out = append(out, albumDTO{Title: a.Title, Artist: a.Artist, Year: a.Year,
			BrowseID: a.BrowseID, Art: a.ArtURL})
	}
	return out
}
