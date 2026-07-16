package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/identify"
	"github.com/dotjarden/pixeltui/tui/lyrics"
	"github.com/dotjarden/pixeltui/tui/source"
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
	lyricsCache        = newTTLCache(24*time.Hour, 200)
	lyricsMissCache    = newTTLCache(10*time.Minute, 256) // short-TTL negative cache (LRCLIB miss/timeout)
	chartsCache        = newTTLCache(30*time.Minute, 16)
	artistCache        = newTTLCache(time.Hour, 64)
	artistExtrasCache  = newTTLCache(time.Hour, 128)
	artistResolveCache = newTTLCache(24*time.Hour, 256)
	albumCache         = newTTLCache(24*time.Hour, 64)
	trackInfoCache     = newTTLCache(24*time.Hour, 128)
	ytMetaCache        = newTTLCache(24*time.Hour, 256) // yt-dlp dump-json per video id (lazy, off the credits critical path)
	searchCache        = newTTLCache(10*time.Minute, 64)
)

// youtubeProvider returns the registered YouTube source, or nil when the
// registry isn't wired yet. Endpoints that only make sense for YouTube use it
// and fall back to the legacy ytm package functions.
func (s *server) youtubeProvider() source.Provider {
	if s.cfg.Sources == nil {
		return nil
	}
	return s.cfg.Sources.ByKey("youtube")
}

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
	// A recent miss (LRCLIB had nothing, or was too slow and timed out) is cached
	// briefly so a slow/absent song doesn't re-pay the full fetch on every open.
	// Short TTL on purpose: LRCLIB slowness is often transient and lyrics get
	// added over time, so re-check soon rather than blanking the track for long.
	if _, ok := lyricsMissCache.get(key); ok {
		writeJSON(w, map[string]any{"synced": []lyricLine{}, "plain": ""})
		return
	}

	out := map[string]any{"synced": []lyricLine{}, "plain": ""}
	found := false
	var res lyrics.Result
	var err error
	if s.cfg.Lyrics != nil {
		res, err = s.cfg.Lyrics.Fetch(r.Context(), artist, track, "", durSec)
	} else {
		res, err = lyrics.Fetch(artist, track, "", durSec)
	}
	if err == nil && !res.Empty() {
		lines := make([]lyricLine, 0, len(res.Synced))
		for _, l := range res.Synced {
			lines = append(lines, lyricLine{T: l.T, Text: l.Text})
		}
		out["synced"] = lines
		out["plain"] = res.Plain
		found = true
	} else if kind, vid, ok := splitID(q.Get("id")); ok && kind == "yt" {
		if text, lerr := ytm.Lyrics(vid); lerr == nil && text != "" {
			out["plain"] = text
			found = true
		}
	}
	// Cache hits for a day; cache misses only briefly (lyricsMissCache) so a
	// transient LRCLIB blip doesn't blank the track for long, but a genuinely
	// absent/slow song isn't re-fetched on every open within the short window.
	if found {
		lyricsCache.put(key, out)
	} else {
		lyricsMissCache.put(key, true)
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
	var (
		cs  []engine.Candidate
		err error
	)
	if p := s.youtubeProvider(); p != nil {
		cs, err = p.Charts(r.Context(), country, 50)
	} else {
		cs, err = ytm.Charts(country, 50)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := map[string]any{"tracks": s.toDTOsWithCaps(cs), "country": country}
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
		artists []source.ArtistRef
		albums  []source.AlbumRef
		wg      sync.WaitGroup
	)
	if p := s.youtubeProvider(); p != nil {
		wg.Add(2)
		go func() { defer wg.Done(); artists, _ = p.SearchArtists(r.Context(), q, 8) }()
		go func() { defer wg.Done(); albums, _ = p.SearchAlbums(r.Context(), q, 10) }()
		wg.Wait()
	} else {
		// No YouTube provider: derive artist + album rails from the configured
		// sources' track search results.
		artists, albums = s.searchEntitiesFromTracks(r.Context(), q)
	}
	// Entity shelves are a richer, separate YouTube Music query and occasionally
	// come back empty even while the regular track search is healthy. Never turn
	// a successful search into a songs-only experience in that case: tracks have
	// enough artist, album, and artwork data to build a useful fallback shelf.
	if len(artists) == 0 || len(albums) == 0 {
		fallbackArtists, fallbackAlbums := s.searchEntitiesFromTracks(r.Context(), q)
		if len(artists) == 0 {
			artists = fallbackArtists
		}
		if len(albums) == 0 {
			albums = fallbackAlbums
		}
	}

	type artistHitDTO struct {
		Name     string `json:"name"`
		Art      string `json:"art,omitempty"`
		BrowseID string `json:"browse_id,omitempty"`
	}
	hits := make([]artistHitDTO, 0, len(artists))
	for _, a := range artists {
		hits = append(hits, artistHitDTO{Name: a.Name, Art: a.Art, BrowseID: a.Ref})
	}
	out := map[string]any{"artists": hits, "albums": toAlbumDTOs(albums)}
	searchCache.put(key, out)
	writeJSON(w, out)
}

// searchEntitiesFromTracks builds artist/album rails from the configured track
// searches. It is the resilient path when an upstream entity search is absent,
// slow, or returns no useful matches.
func (s *server) searchEntitiesFromTracks(ctx context.Context, q string) ([]source.ArtistRef, []source.AlbumRef) {
	if s.cfg.Sources == nil {
		return nil, nil
	}
	lowerQ := strings.ToLower(q)

	artistMap := map[string]source.ArtistRef{}
	albumMap := map[string]source.AlbumRef{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range s.cfg.Sources.All() {
		wg.Add(1)
		go func(p source.Provider) {
			defer wg.Done()
			cs, err := p.Search(ctx, q, 100)
			if err != nil || len(cs) == 0 {
				return
			}
			for _, c := range cs {
				key := strings.ToLower(strings.TrimSpace(c.Track + " " + c.Artist + " " + c.Album))
				if !strings.Contains(key, lowerQ) {
					continue
				}
				mu.Lock()
				if a := strings.TrimSpace(c.Artist); a != "" {
					aKey := strings.ToLower(a)
					if _, ok := artistMap[aKey]; !ok {
						artistMap[aKey] = source.ArtistRef{Name: a, Art: c.ArtURL}
					}
				}
				if alb := strings.TrimSpace(c.Album); alb != "" {
					albKey := strings.ToLower(alb + "|" + strings.TrimSpace(c.Artist))
					if _, ok := albumMap[albKey]; !ok {
						// A synthetic ref is valid for local/Subsonic fallback pages.
						// Let YouTube albums resolve by title instead — a synthetic
						// ref is not a YouTube browse ID.
						ref := ""
						if p.Key() != "youtube" {
							ref = albumRefID(alb, c.Artist)
						}
						albumMap[albKey] = source.AlbumRef{
							Title:  alb,
							Artist: strings.TrimSpace(c.Artist),
							Art:    c.ArtURL,
							Ref:    ref,
						}
					}
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	artists := make([]source.ArtistRef, 0, len(artistMap))
	for _, a := range artistMap {
		artists = append(artists, a)
	}
	sort.Slice(artists, func(i, j int) bool {
		return strings.ToLower(artists[i].Name) < strings.ToLower(artists[j].Name)
	})
	if len(artists) > 8 {
		artists = artists[:8]
	}

	albums := make([]source.AlbumRef, 0, len(albumMap))
	for _, a := range albumMap {
		albums = append(albums, a)
	}
	sort.Slice(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
	})
	if len(albums) > 10 {
		albums = albums[:10]
	}

	return artists, albums
}

// ── artist page ─────────────────────────────────────────────────────────────

type albumDTO struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Year     string `json:"year,omitempty"`
	BrowseID string `json:"browse_id"`
	Art      string `json:"art,omitempty"`
}

type artistStatsDTO struct {
	Listeners int      `json:"listeners,omitempty"`
	Playcount int      `json:"playcount,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Bio       string   `json:"bio,omitempty"`
}

type similarArtistDTO struct {
	Name      string `json:"name"`
	Art       string `json:"art,omitempty"`
	Listeners int    `json:"listeners,omitempty"`
	BrowseID  string `json:"browse_id,omitempty"`
}

// artistExtras contains metadata that is useful but must never delay the
// playable artist page. It deliberately avoids a YouTube search per similar
// artist: that old fan-out made one artist navigation wait on up to 12 remote
// searches. Cards resolve a missing image/ID independently after they paint.
func (s *server) artistExtras(name string) map[string]any {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || s.cfg.Lastfm == nil {
		return map[string]any{}
	}
	if v, ok := artistExtrasCache.get(key); ok {
		return v.(map[string]any)
	}

	var (
		stats *artistStatsDTO
		sims  []similarArtistDTO
		wg    sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		info, err := s.cfg.Lastfm.GetArtistInfo(name)
		if err != nil {
			return
		}
		tags := info.Tags
		if len(tags) > 4 {
			tags = tags[:4]
		}
		stats = &artistStatsDTO{Listeners: info.Listeners, Playcount: info.Playcount, Tags: tags, Bio: info.Summary}
	}()
	go func() {
		defer wg.Done()
		found, err := s.cfg.Lastfm.GetSimilarArtists(name, 12)
		if err != nil {
			return
		}
		out := make([]similarArtistDTO, 0, len(found))
		for _, artist := range found {
			if artist.Name == "" {
				continue
			}
			out = append(out, similarArtistDTO{Name: artist.Name, Art: artist.Artwork()})
		}
		sims = out
	}()
	wg.Wait()

	out := map[string]any{}
	if stats != nil {
		out["stats"] = stats
	}
	if len(sims) > 0 {
		out["similar_artists"] = sims
	}
	artistExtrasCache.put(key, out)
	return out
}

// handleArtistExtras serves nonessential profile metadata separately from the
// core browse response. Desktop uses it after the page becomes interactive.
func (s *server) handleArtistExtras(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.artistExtras(name))
}

// handleArtistResolve gives a recommendation card its canonical browse ID and
// artwork without coupling that lookup to the artist page request.
func (s *server) handleArtistResolve(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	key := strings.ToLower(name)
	if v, ok := artistResolveCache.get(key); ok {
		writeJSON(w, v)
		return
	}
	out := map[string]string{"name": name}
	if p := s.youtubeProvider(); p != nil {
		refs, err := p.SearchArtists(r.Context(), name, 1)
		if err == nil && len(refs) > 0 {
			out["browse_id"] = refs[0].Ref
			out["art"] = refs[0].Art
		}
	}
	artistResolveCache.put(key, out)
	writeJSON(w, out)
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
	browseID := strings.TrimSpace(r.URL.Query().Get("browse_id"))
	art := strings.TrimSpace(r.URL.Query().Get("art"))
	if browseID != "" {
		key += "|" + browseID
	}
	if art != "" {
		key += "|art:" + art
	}
	fast := r.URL.Query().Get("fast") == "1"
	cacheKey := key
	if fast {
		cacheKey += "|fast"
	}
	if v, ok := artistCache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}

	var (
		ref source.ArtistRef
		err error
	)
	var page *source.ArtistPage
	if p := s.youtubeProvider(); p != nil {
		if browseID != "" {
			ref = source.ArtistRef{Name: name, Ref: browseID, Art: art}
			page, err = p.BrowseArtist(r.Context(), ref)
		} else {
			refs, serr := p.SearchArtists(r.Context(), name, 1)
			if serr == nil && len(refs) > 0 {
				ref = refs[0]
				page, err = p.BrowseArtist(r.Context(), ref)
			} else {
				// ArtistSearch is a separate YouTube Music surface and can return
				// intermittent 404s even while normal track search works. A page
				// request must not fail solely because its entity lookup did.
				page, err = s.buildArtistPageFromSearch(r.Context(), name, true)
				ref = source.ArtistRef{Name: name}
			}
		}
	} else {
		// No YouTube provider: build a best-effort artist page from whatever
		// configured sources (local, Subsonic, …) can return for this name.
		ref = source.ArtistRef{Name: name}
		page, err = s.buildArtistPageFromSearch(r.Context(), name, false)
	}
	if err != nil && s.youtubeProvider() != nil {
		// A browse ID can expire or point at a layout YouTube no longer serves.
		// Keep the detail route useful with the same track-search fallback rather
		// than returning a blank 404 page to the desktop client.
		page, err = s.buildArtistPageFromSearch(r.Context(), name, true)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if page == nil || page.Name == "" {
		if page == nil {
			page = &source.ArtistPage{}
		}
		page.Name = ref.Name
	}
	if page.Art == "" {
		page.Art = ref.Art
	}

	out := map[string]any{
		"name":        page.Name,
		"art":         page.Art,
		"top_songs":   s.toDTOsWithCaps(page.TopSongs),
		"albums":      toAlbumDTOs(page.Albums),
		"singles":     toAlbumDTOs(page.Singles),
		"description": page.Description,
	}
	if !fast {
		for k, v := range s.artistExtras(ref.Name) {
			out[k] = v
		}
	}
	artistCache.put(cacheKey, out)
	writeJSON(w, out)
}

// buildArtistPageFromSearch constructs a usable artist page from track search
// results. It normally omits YouTube when another provider is the primary
// fallback, but can include it when YouTube's artist entity endpoint is flaky.
func (s *server) buildArtistPageFromSearch(ctx context.Context, name string, includeYouTube bool) (*source.ArtistPage, error) {
	if s.cfg.Sources == nil {
		return nil, source.ErrNotSupported
	}

	type matched struct {
		tracks []engine.Candidate
		albums map[string]source.AlbumRef
		art    string
	}
	res := matched{albums: map[string]source.AlbumRef{}}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range s.cfg.Sources.All() {
		if p.Key() == "youtube" && !includeYouTube {
			continue
		}
		wg.Add(1)
		go func(p source.Provider) {
			defer wg.Done()
			cs, err := p.Search(ctx, name, 200)
			if err != nil || len(cs) == 0 {
				return
			}
			for _, c := range cs {
				if !artistNameMatches(name, c.Artist) {
					continue
				}
				mu.Lock()
				res.tracks = append(res.tracks, c)
				if res.art == "" && c.ArtURL != "" {
					res.art = c.ArtURL
				}
				if alb := strings.TrimSpace(c.Album); alb != "" {
					key := strings.ToLower(alb)
					if _, ok := res.albums[key]; !ok {
						res.albums[key] = source.AlbumRef{
							Title:  alb,
							Artist: strings.TrimSpace(c.Artist),
							Art:    c.ArtURL,
							Ref:    albumRefID(alb, c.Artist),
						}
					}
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	if len(res.tracks) == 0 {
		return nil, fmt.Errorf("artist not found")
	}

	// Pick the most common artist spelling among matched tracks as the header.
	canon := name
	counts := map[string]int{}
	for _, t := range res.tracks {
		a := strings.TrimSpace(t.Artist)
		if a != "" {
			counts[a]++
		}
	}
	best := 0
	for a, n := range counts {
		if n > best {
			best = n
			canon = a
		}
	}

	seen := map[string]bool{}
	top := make([]engine.Candidate, 0, len(res.tracks))
	for _, t := range res.tracks {
		k := strings.ToLower(strings.TrimSpace(t.Track))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		top = append(top, t)
		if len(top) >= 50 {
			break
		}
	}

	albums := make([]source.AlbumRef, 0, len(res.albums))
	for _, a := range res.albums {
		albums = append(albums, a)
	}
	sort.Slice(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
	})

	return &source.ArtistPage{
		Name:     canon,
		Art:      res.art,
		TopSongs: top,
		Albums:   albums,
	}, nil
}

func artistNameMatches(query, candidate string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	c := strings.ToLower(strings.TrimSpace(candidate))
	return c != "" && (c == q || strings.Contains(c, q) || strings.Contains(q, c))
}

func albumRefID(title, artist string) string {
	payload := strings.ToLower(strings.TrimSpace(title)) + "|" + strings.ToLower(strings.TrimSpace(artist))
	return "alb:" + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// ── album page ──────────────────────────────────────────────────────────────

// handleAlbum returns an album's ordered tracks + metadata.
// Query: browse_id (from an artist page / album search), title, artist.
func (s *server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	browseID := q.Get("browse_id")
	title, artist := q.Get("title"), q.Get("artist")

	var ref source.AlbumRef
	// No browse id → resolve via album search (e.g. "go to album" on a track).
	if browseID == "" {
		if title == "" {
			http.Error(w, "missing browse_id or title", http.StatusBadRequest)
			return
		}
		if p := s.youtubeProvider(); p != nil {
			hits, serr := p.SearchAlbums(r.Context(), title+" "+artist, 5)
			if serr != nil || len(hits) == 0 {
				http.Error(w, "album not found", http.StatusNotFound)
				return
			}
			ref = bestAlbumMatch(hits, title)
		} else {
			// No YouTube provider: build a synthetic browse id so the client can
			// request this album again, and populate it from search below.
			ref = source.AlbumRef{Title: title, Artist: artist, Ref: albumRefID(title, artist)}
		}
		browseID = ref.Ref
		title, artist = ref.Title, ref.Artist
	} else {
		ref = source.AlbumRef{Title: title, Artist: artist, Ref: browseID}
	}

	// Synthetic album refs carry their own title/artist; trust them if the
	// query string omitted those fields.
	if strings.HasPrefix(browseID, "alb:") {
		if t, a, ok := parseAlbumRefID(browseID); ok {
			if title == "" {
				title = t
			}
			if artist == "" {
				artist = a
			}
		}
	}

	cacheKey := browseID
	if cacheKey == "" && title != "" {
		cacheKey = "album|" + strings.ToLower(title) + "|" + strings.ToLower(artist)
	}
	if v, ok := albumCache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	// No track cap: an album page must show the whole tracklist (long
	// compilations exceed the old 60), and the browse response is a single
	// fetch either way.
	var (
		detail *source.AlbumPage
		tracks []trackDTO
		art    string
		err    error
	)
	if p := s.youtubeProvider(); p != nil {
		detail, err = p.BrowseAlbum(r.Context(), ref)
	} else {
		detail, err = s.buildAlbumPageFromSearch(r.Context(), title, artist)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	tracks = s.toDTOsWithCaps(detail.Tracks)
	art = detail.Art
	if art == "" {
		// Some album headers omit the cover; the first track's thumbnail is
		// better than a blank page header.
		for _, t := range tracks {
			if t.Art != "" {
				art = t.Art
				break
			}
		}
	}
	out := map[string]any{
		"title":       detail.Title,
		"artist":      detail.Artist,
		"year":        detail.Year,
		"art":         art,
		"tracks":      tracks,
		"description": detail.Description,
		"explicit":    detail.Explicit,
	}
	albumCache.put(cacheKey, out)
	writeJSON(w, out)
}

// buildAlbumPageFromSearch returns a best-effort album page when no YouTube
// provider is registered. It searches every other configured source and
// returns tracks whose album matches the requested title.
func (s *server) buildAlbumPageFromSearch(ctx context.Context, title, artist string) (*source.AlbumPage, error) {
	if s.cfg.Sources == nil {
		return nil, source.ErrNotSupported
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("album title required")
	}

	type matched struct {
		tracks []engine.Candidate
		art    string
	}
	res := matched{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range s.cfg.Sources.All() {
		if p.Key() == "youtube" {
			continue
		}
		wg.Add(1)
		go func(p source.Provider) {
			defer wg.Done()
			cs, err := p.Search(ctx, title+" "+artist, 200)
			if err != nil || len(cs) == 0 {
				return
			}
			for _, c := range cs {
				if !albumMatches(title, artist, c) {
					continue
				}
				mu.Lock()
				res.tracks = append(res.tracks, c)
				if res.art == "" && c.ArtURL != "" {
					res.art = c.ArtURL
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	if len(res.tracks) == 0 {
		return nil, fmt.Errorf("album not found")
	}

	sort.Slice(res.tracks, func(i, j int) bool {
		return strings.ToLower(res.tracks[i].Track) < strings.ToLower(res.tracks[j].Track)
	})

	albumArtist := artist
	if strings.TrimSpace(albumArtist) == "" {
		counts := map[string]int{}
		for _, t := range res.tracks {
			counts[t.Artist]++
		}
		best := 0
		for a, n := range counts {
			if n > best {
				best = n
				albumArtist = a
			}
		}
	}

	return &source.AlbumPage{
		Title:  title,
		Artist: albumArtist,
		Art:    res.art,
		Tracks: res.tracks,
	}, nil
}

func albumMatches(title, artist string, c engine.Candidate) bool {
	gotAlbum := strings.ToLower(strings.TrimSpace(c.Album))
	wantAlbum := strings.ToLower(strings.TrimSpace(title))
	if gotAlbum == "" || wantAlbum == "" {
		return false
	}
	albumOK := gotAlbum == wantAlbum || strings.Contains(gotAlbum, wantAlbum) || strings.Contains(wantAlbum, gotAlbum)
	if !albumOK {
		return false
	}
	gotArtist := strings.ToLower(strings.TrimSpace(c.Artist))
	wantArtist := strings.ToLower(strings.TrimSpace(artist))
	if wantArtist == "" {
		return true
	}
	return gotArtist == wantArtist || strings.Contains(gotArtist, wantArtist) || strings.Contains(wantArtist, gotArtist)
}

func parseAlbumRefID(id string) (title, artist string, ok bool) {
	if !strings.HasPrefix(id, "alb:") {
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "alb:"))
	if err != nil {
		return
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return
	}
	return parts[0], parts[1], true
}

// ── track info / credits ────────────────────────────────────────────────────

// handleTrackInfo returns rich metadata for a single track from every source
// the server can reach: Last.fm stats, YouTube Music video details, and the
// server's own listening history.
// Query: id (stream id), title, artist, album, duration, art (title/artist required).
func (s *server) handleTrackInfo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := strings.TrimSpace(q.Get("id"))
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if v, ok := trackInfoCache.get(id); ok {
		writeJSON(w, v)
		return
	}

	kind, val, ok := splitID(id)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(q.Get("title"))
	artist := strings.TrimSpace(q.Get("artist"))
	if title == "" || artist == "" {
		http.Error(w, "title and artist required", http.StatusBadRequest)
		return
	}
	album := strings.TrimSpace(q.Get("album"))
	duration, _ := strconv.Atoi(q.Get("duration"))
	art := strings.TrimSpace(q.Get("art"))

	out := map[string]any{
		"id":       id,
		"source":   kind,
		"title":    title,
		"artist":   artist,
		"album":    album,
		"duration": duration,
		"art":      art,
	}

	// The five metadata sources below are independent and all best-effort.
	// They used to run serially, so page latency was the *sum* of Last.fm +
	// provider + yt-dlp (up to 15s) + a full-history scan + a full lyrics
	// fetch. Run them concurrently so it's the *max* of the set instead —
	// dominated by the yt-dlp subprocess, not stacked on top of it. Each
	// goroutine merges its own keys into `out` under a shared mutex.
	var mu sync.Mutex
	var wg sync.WaitGroup
	put := func(key string, v any) {
		mu.Lock()
		out[key] = v
		mu.Unlock()
	}

	// Last.fm track info.
	if s.cfg.Lastfm != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, err := s.cfg.Lastfm.GetTrackInfo(artist, title); err == nil {
				lfm := map[string]any{
					"listeners": info.Listeners,
					"playcount": info.Playcount,
					"tags":      info.Tags,
				}
				if info.Wiki != "" {
					lfm["wiki"] = info.Wiki
				}
				if info.Album != "" {
					lfm["album"] = info.Album
				}
				if info.Duration > 0 {
					lfm["duration"] = info.Duration
				}
				put("lastfm", lfm)
			}
		}()
	}

	// Source-specific metadata, when the registry knows about this id. This
	// one can override the base title/artist/album/duration/art, so it takes
	// the mutex for each write.
	if s.cfg.Sources != nil {
		if provider, _, ok2 := s.cfg.Sources.SourceFor(id); ok2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if meta, err := provider.TrackInfo(r.Context(), val); err == nil {
					mu.Lock()
					if meta.Artist != "" {
						out["artist"] = meta.Artist
					}
					if meta.Title != "" {
						out["title"] = meta.Title
					}
					if meta.Album != "" {
						out["album"] = meta.Album
					}
					if meta.DurationSec > 0 {
						out["duration"] = meta.DurationSec
					}
					if meta.ArtURL != "" {
						out["art"] = meta.ArtURL
					}
					for k, v := range meta.Raw {
						out[k] = v
					}
					mu.Unlock()
				}
			}()
		}
	}

	// YouTube rich metadata (yt-dlp) is NOT fetched here: it spawns a
	// subprocess that can take seconds, so it would dominate page latency.
	// The client loads it lazily from /api/trackinfo/youtube, which renders
	// the YouTube card after the rest of the page is already on screen.

	// Listening history on this server — scans the full history file.
	if s.cfg.Library != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listens, err := s.cfg.Library.Listens(0)
			if err != nil {
				return
			}
			var plays int
			var firstPlayed, lastPlayed int64
			for _, l := range listens {
				if l.Candidate.Track == "" || l.Candidate.Artist == "" {
					continue
				}
				var match bool
				switch kind {
				case "yt":
					match = l.Candidate.VideoID == val
				default:
					match = strings.EqualFold(l.Candidate.Track, title) &&
						strings.EqualFold(l.Candidate.Artist, artist)
				}
				if !match {
					continue
				}
				plays++
				u := l.At.Unix()
				if firstPlayed == 0 || u < firstPlayed {
					firstPlayed = u
				}
				if u > lastPlayed {
					lastPlayed = u
				}
			}
			if plays > 0 {
				put("history", map[string]any{
					"plays":        plays,
					"first_played": firstPlayed,
					"last_played":  lastPlayed,
				})
			}
		}()
	}

	// Lyrics availability is deliberately NOT fetched here. The credits page
	// only needed it as a boolean ("Lyrics: Available"), but computing it ran
	// a full multi-provider lyrics fetch (plus a YouTube Music fallback for yt
	// ids) — the one slow source unique to this endpoint, since album/artist
	// pages never fetch lyrics. Dropping it leaves the fast path as Last.fm +
	// provider + history, all of which the fast album/artist endpoints already
	// do. The player shows lyrics on its own surface regardless.

	wg.Wait()

	trackInfoCache.put(id, out)
	writeJSON(w, out)
}

// handleTrackInfoYouTube returns the yt-dlp-rich YouTube metadata for a single
// track id (channel, views, upload date, description, license). This is the
// slow step that used to block /api/trackinfo — split out so the credits page
// renders from the fast payload first and streams this card in after. Cached
// per video id for 24h, so re-opening the same track never re-runs yt-dlp.
// Query: id (yt:<videoID>). Best-effort: returns {} when the id isn't a
// YouTube id, yt-dlp is missing, or the lookup fails.
func (s *server) handleTrackInfoYouTube(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	kind, val, ok := splitID(id)
	if !ok || kind != "yt" {
		writeJSON(w, map[string]any{})
		return
	}
	if v, ok := ytMetaCache.get(val); ok {
		writeJSON(w, v)
		return
	}
	out := map[string]any{}
	if meta, err := trackMetadata(val); err == nil && meta != nil {
		out["youtube"] = map[string]any{
			"video_id":    meta.VideoID,
			"title":       meta.Title,
			"channel":     meta.Channel,
			"upload_date": meta.UploadDate,
			"views":       meta.ViewCount,
			"description": meta.Description,
			"license":     meta.License,
			"duration":    meta.Duration,
		}
	}
	ytMetaCache.put(val, out)
	writeJSON(w, out)
}

// handleIdentify accepts a Chromaprint fingerprint + duration and returns the
// best matching track from the registered identifiers (local index, AcoustID, …).
func (s *server) handleIdentify(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Identify == nil {
		http.Error(w, "identify not configured", http.StatusNotImplemented)
		return
	}
	var body struct {
		Fingerprint []int32 `json:"fingerprint"`
		Duration    int     `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Fingerprint) == 0 || body.Duration <= 0 {
		http.Error(w, "fingerprint and duration required", http.StatusBadRequest)
		return
	}
	res, err := s.cfg.Identify.Identify(r.Context(), body.Fingerprint, body.Duration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	dto, _ := s.toDTOWithCaps(res.Candidate)
	writeJSON(w, map[string]any{
		"candidate": dto,
		"score":     res.Score,
		"source":    res.Source,
	})
}

// handleIdentifyAudio accepts a raw audio clip (e.g. WAV from iOS microphone)
// and fingerprints it server-side with fpcalc, then runs the same recognition
// registry. Lets phone clients identify without shipping a native Chromaprint
// library.
func (s *server) handleIdentifyAudio(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Identify == nil {
		http.Error(w, "identify not configured", http.StatusNotImplemented)
		return
	}
	const maxUpload = 20 << 20 // 20 MiB — plenty for a 10 s mono WAV
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "identify-*.wav")
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, file); err != nil {
		http.Error(w, "upload error", http.StatusBadRequest)
		return
	}
	_ = tmp.Close()

	fp, err := identify.ComputeFingerprint(r.Context(), tmp.Name())
	if err != nil {
		fmt.Printf("identify audio: fpcalc failed: %v\n", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// Debug: log fingerprint shape and stash the last uploaded clip so we can
	// inspect it / re-run fpcalc+AcoustID by hand when matches fail.
	fmt.Printf("identify audio: fingerprinted %ds (subfp=%d, compressed=%dB), searching %v\n",
		fp.DurationSec, len(fp.Data), len(fp.Compressed), s.cfg.Identify.AvailableNames())
	if clipDir := filepath.Join(s.cfg.DataDir, "identify"); os.MkdirAll(clipDir, 0o755) == nil {
		if saved, e := os.Create(filepath.Join(clipDir, "last-clip.wav")); e == nil {
			src, _ := os.Open(tmp.Name())
			if src != nil {
				_, _ = io.Copy(saved, src)
				_ = src.Close()
			}
			_ = saved.Close()
			fmt.Printf("identify audio: saved clip → %s\n", filepath.Join(clipDir, "last-clip.wav"))
		}
	}
	// Match the clip against the local fingerprint index only. AcoustID is
	// bypassed for clips: its web lookup hard-filters on duration≈recording
	// length, so a ~10 s clip submitted with its own duration can never match a
	// multi-minute recording (verified: same fingerprint → 0 results at
	// duration=60, 0.97 at duration=174), and even a full-file AcoustID match
	// yields a zero-id candidate here. The local slide-matcher is the correct
	// clip recognizer. The full registry (incl. AcoustID) is still used by the
	// fingerprint-based /api/identify handler.
	li := s.cfg.Identify.LocalIndex()
	if li == nil {
		http.Error(w, "identify not configured", http.StatusNotImplemented)
		return
	}
	res, err := li.Identify(r.Context(), fp.Data, fp.DurationSec)
	if err != nil {
		fmt.Printf("identify audio: no match: %v\n", err)
		if errors.Is(err, identify.ErrNoMatch) {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	fmt.Printf("identify audio: matched via %s (score %.2f)\n", res.Source, res.Score)
	dto, _ := s.toDTOWithCaps(res.Candidate)
	writeJSON(w, map[string]any{
		"candidate": dto,
		"score":     res.Score,
		"source":    res.Source,
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

func toAlbumDTOs(as []source.AlbumRef) []albumDTO {
	out := make([]albumDTO, 0, len(as))
	for _, a := range as {
		out = append(out, albumDTO{Title: a.Title, Artist: a.Artist, Year: a.Year,
			BrowseID: a.Ref, Art: a.Art})
	}
	return out
}

// bestAlbumMatch picks the album whose title most closely matches the target.
func bestAlbumMatch(hits []source.AlbumRef, target string) source.AlbumRef {
	for i, a := range hits {
		if strings.EqualFold(strings.TrimSpace(a.Title), strings.TrimSpace(target)) {
			return hits[i]
		}
	}
	return hits[0]
}
