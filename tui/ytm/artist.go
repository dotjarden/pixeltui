package ytm

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// discographySectionCap bounds how many albums/singles we accumulate per
// section while following YTM's discography continuations. Continuation chains
// are normally short, but the cap stops a pathological/looping response from
// fetching unboundedly; hitting it is logged.
const discographySectionCap = 200

// ArtistPage is a full artist landing: top songs plus the album/single shelves,
// like the YouTube Music artist page it's parsed from.
type ArtistPage struct {
	Name        string
	Description string
	TopSongs    []engine.Candidate
	Albums      []Album
	Singles     []Album
}

// topSongsMax bounds the artist Top Songs list. The artist page shelf only
// shows 5; the shelf's "more" playlist returns ~100 ranked rows — 40 keeps the
// list a genuine "top songs" page without shipping the full catalog dump.
const topSongsMax = 40

// BrowseArtist fetches and parses an artist page by channel browse id (UC…).
// The landing page itself is shallow (5 top songs, first-page carousels), so
// when YTM exposes "more" endpoints — the song shelf's full playlist and the
// album/single carousels' discography pages — those are fetched too and
// replace the shallow sections.
func BrowseArtist(browseID string) (*ArtistPage, error) {
	root, err := browse(map[string]interface{}{"browseId": browseID, "context": innerContext("US")})
	if err != nil {
		return nil, err
	}
	page := &ArtistPage{}

	// "More" endpoints discovered while walking the page (fetched after).
	var songsPlaylistID string                 // song shelf bottomEndpoint (VL…)
	type moreEndpoint struct{ id, params string }
	var albumsMore, singlesMore *moreEndpoint

	// Header: immersive (with description) or visual, depending on the artist.
	for _, h := range []string{"musicImmersiveHeaderRenderer", "musicVisualHeaderRenderer"} {
		if page.Name == "" {
			page.Name = cleanText(runText(dig(root, "header", h, "title", "runs")))
		}
		if page.Description == "" {
			page.Description = cleanText(runText(dig(root, "header", h, "description", "runs")))
		}
	}

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if shelf, ok := t["musicShelfRenderer"].(map[string]interface{}); ok {
				// The song shelf ("Songs" / "Top songs"). Require video ids so
				// non-playable rows never sneak in.
				title := strings.ToLower(runText(dig(shelf, "title", "runs")))
				if title == "" || strings.Contains(title, "song") {
					page.TopSongs = append(page.TopSongs,
						parseRichTrackRows(shelf, page.Name, "", 0, true)...)
					if id := str(dig(shelf, "bottomEndpoint", "browseEndpoint", "browseId")); id != "" {
						songsPlaylistID = id
					}
				}
				return
			}
			if car, ok := t["musicCarouselShelfRenderer"].(map[string]interface{}); ok {
				header := dig(car, "header", "musicCarouselShelfBasicHeaderRenderer")
				title := strings.ToLower(runText(dig(header, "title", "runs")))
				more := &moreEndpoint{
					id: str(dig(header, "moreContentButton", "buttonRenderer",
						"navigationEndpoint", "browseEndpoint", "browseId")),
					params: str(dig(header, "moreContentButton", "buttonRenderer",
						"navigationEndpoint", "browseEndpoint", "params")),
				}
				switch {
				case strings.Contains(title, "single"): // "Singles", "Singles & EPs"
					page.Singles = append(page.Singles, albumCards(car, page.Name)...)
					if more.id != "" {
						singlesMore = more
					}
				case strings.Contains(title, "album"): // "Albums", "Albums & Singles"
					page.Albums = append(page.Albums, albumCards(car, page.Name)...)
					if more.id != "" {
						albumsMore = more
					}
				}
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(dig(root, "contents"))

	// Deepen the shallow sections concurrently. Every fetch is best-effort:
	// on any failure the landing-page slice stays.
	var wg sync.WaitGroup
	if songsPlaylistID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pl, err := browse(map[string]interface{}{
				"browseId": songsPlaylistID, "context": innerContext("US")})
			if err != nil {
				return
			}
			if rows := parseRichTrackRows(pl, page.Name, "", topSongsMax, true); len(rows) > len(page.TopSongs) {
				page.TopSongs = rows
			}
		}()
	}
	// fetchMore fully paginates a section's discography endpoint. YTM only
	// returns ~30 entities per page; the rest hang off continuation tokens, so
	// the first browse is followed by `?continuation=<token>` browses until the
	// chain ends (or the per-section cap trips). Accumulated cards are deduped
	// by browseId and sorted newest-first before replacing the shallow slice.
	fetchMore := func(more *moreEndpoint, dst *[]Album, section string) {
		defer wg.Done()
		payload := map[string]interface{}{"browseId": more.id, "context": innerContext("US")}
		if more.params != "" {
			payload["params"] = more.params
		}
		pg, err := browse(payload)
		if err != nil {
			return
		}

		var acc []Album
		seen := map[string]bool{}
		add := func(cards []Album) {
			for _, c := range cards {
				if c.BrowseID == "" || seen[c.BrowseID] {
					continue
				}
				seen[c.BrowseID] = true
				acc = append(acc, c)
			}
		}
		add(albumCards(pg, page.Name))

		// Follow continuation tokens. Each continuation browse reuses the same
		// endpoint with a `continuation` body field (WEB_REMIX's grid/musicShelf
		// continuation convention); the response carries the same renderer
		// shapes albumCards already parses, plus the next token (if any).
		token := continuationToken(pg)
		capped := false
		for token != "" {
			if len(acc) >= discographySectionCap {
				capped = true
				break
			}
			next, err := browse(map[string]interface{}{
				"continuation": token, "context": innerContext("US")})
			if err != nil {
				break
			}
			before := len(acc)
			add(albumCards(next, page.Name))
			token = continuationToken(next)
			if len(acc) == before && token == "" {
				break // no new cards and no further token — chain exhausted
			}
		}
		if capped {
			log.Printf("ytm artist %q: %s discography capped at %d items",
				page.Name, section, discographySectionCap)
		}

		// Newest first; unknown years sink to the bottom. Stable so the
		// continuation arrival order is preserved within a year.
		sortAlbumsByYearDesc(acc)
		if len(acc) > len(*dst) {
			*dst = acc
		}
	}
	if albumsMore != nil {
		wg.Add(1)
		go fetchMore(albumsMore, &page.Albums, "albums")
	}
	if singlesMore != nil {
		wg.Add(1)
		go fetchMore(singlesMore, &page.Singles, "singles")
	}
	wg.Wait()

	if page.Name == "" && len(page.TopSongs) == 0 && len(page.Albums) == 0 {
		return nil, fmt.Errorf("artist: empty page")
	}
	return page, nil
}

// albumCards extracts album/single entities from a carousel shelf.
func albumCards(car interface{}, artist string) []Album {
	var out []Album
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if tr, ok := t["musicTwoRowItemRenderer"].(map[string]interface{}); ok {
				id := str(dig(tr, "navigationEndpoint", "browseEndpoint", "browseId"))
				title := cleanText(runText(dig(tr, "title", "runs")))
				if !strings.HasPrefix(id, "MPREb") || title == "" {
					return // not an album entity
				}
				out = append(out, Album{
					Title:    title,
					Artist:   artist,
					Year:     yearFromRuns(dig(tr, "subtitle", "runs")),
					BrowseID: id,
					ArtURL: thumbsBest(dig(tr, "thumbnailRenderer",
						"musicThumbnailRenderer", "thumbnail", "thumbnails")),
				})
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(car)
	return out
}

// continuationToken pulls the next discography page's continuation token from a
// browse response, supporting both shapes WEB_REMIX emits: the legacy
// `continuations[].nextContinuationData.continuation` attached to a
// grid/musicShelf/sectionList, and the newer `continuationItemRenderer` whose
// `continuationEndpoint.continuationCommand.token` carries it. Returns "" when
// the response exposes no continuation (the chain has ended).
func continuationToken(root interface{}) string {
	var token string
	var walk func(v interface{})
	walk = func(v interface{}) {
		if token != "" {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			// Newer shape: continuationItemRenderer.
			if cir, ok := t["continuationItemRenderer"].(map[string]interface{}); ok {
				if tok := str(dig(cir, "continuationEndpoint",
					"continuationCommand", "token")); tok != "" {
					token = tok
					return
				}
			}
			// Legacy shape: continuations[].nextContinuationData.continuation.
			if conts, ok := t["continuations"].([]interface{}); ok {
				for _, c := range conts {
					if tok := str(dig(c, "nextContinuationData", "continuation")); tok != "" {
						token = tok
						return
					}
				}
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root)
	return token
}

// sortAlbumsByYearDesc orders albums newest-first using the already-parsed Year
// string; entries with an unknown/unparseable year sort last. Stable, so cards
// with equal (or missing) years keep their accumulation order.
func sortAlbumsByYearDesc(albums []Album) {
	year := func(a Album) int {
		if n, err := strconv.Atoi(a.Year); err == nil {
			return n
		}
		return -1 // unknown years sink below any real year
	}
	sort.SliceStable(albums, func(i, j int) bool {
		return year(albums[i]) > year(albums[j])
	})
}

var (
	yearRe  = regexp.MustCompile(`^(19|20)\d\d$`)
	playsRe = regexp.MustCompile(`(?i)^[\d,.]+[KMB]?\s+plays$`) // "142M plays"
)

// yearFromRuns scans subtitle runs (e.g. "Album", " • ", "2013") for a year.
func yearFromRuns(runs interface{}) string {
	a, ok := runs.([]interface{})
	if !ok {
		return ""
	}
	for i := len(a) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(str(dig(a[i], "text"))); yearRe.MatchString(s) {
			return s
		}
	}
	return ""
}

// parseRichTrackRows extracts full track rows (title, artist, album, video id,
// duration, art) from any browse payload containing musicResponsiveListItem
// rows. fallbackArtist/fallbackAlbum fill columns the page leaves implicit
// (album pages don't repeat the artist per row). requireVideoID drops rows
// without a playable id (used for artist pages, where shelves mix row types).
func parseRichTrackRows(root interface{}, fallbackArtist, fallbackAlbum string, limit int, requireVideoID bool) []engine.Candidate {
	var out []engine.Candidate
	seen := map[string]bool{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		if limit > 0 && len(out) >= limit {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			if mr, ok := t["musicResponsiveListItemRenderer"].(map[string]interface{}); ok {
				if c, ok := richRow(mr, fallbackArtist, fallbackAlbum, requireVideoID); ok {
					k := strings.ToLower(c.Track + "|" + c.Artist)
					if !seen[k] {
						seen[k] = true
						out = append(out, c)
					}
				}
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// richRow converts one musicResponsiveListItemRenderer into a Candidate.
func richRow(mr map[string]interface{}, fallbackArtist, fallbackAlbum string, requireVideoID bool) (engine.Candidate, bool) {
	cols, _ := mr["flexColumns"].([]interface{})
	title := cleanTitle(cleanText(flexText(cols, 0)))
	if title == "" {
		return engine.Candidate{}, false
	}
	videoID := str(dig(mr, "playlistItemData", "videoId"))
	if requireVideoID && videoID == "" {
		return engine.Candidate{}, false
	}
	artist := cleanText(flexText(cols, 1))
	if artist == "" || strings.Contains(strings.ToLower(artist), "subscriber") {
		artist = fallbackArtist
	}
	// Artist pages put a play count where album pages put the album name.
	album := cleanText(flexText(cols, 2))
	if album == "" || playsRe.MatchString(album) {
		album = fallbackAlbum
	}
	return engine.Candidate{
		Track:       title,
		Artist:      artist,
		Album:       album,
		VideoID:     videoID,
		DurationSec: parseClock(fixedText(mr, 0)),
		ArtURL:      rowThumb(mr),
	}, true
}

// fixedText returns the text of fixedColumns[i] (where YTM puts durations).
func fixedText(mr map[string]interface{}, i int) string {
	cols, _ := mr["fixedColumns"].([]interface{})
	if i < 0 || i >= len(cols) {
		return ""
	}
	return runText(dig(cols[i], "musicResponsiveListItemFixedColumnRenderer", "text", "runs"))
}

// rowThumb returns the largest thumbnail URL attached to a row, if any.
func rowThumb(mr map[string]interface{}) string {
	return thumbsBest(dig(mr, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
}

// thumbsBest picks the largest thumbnail URL from a `thumbnails` array value.
func thumbsBest(v interface{}) string {
	thumbs, _ := v.([]interface{})
	best, bestArea := "", 0
	for _, th := range thumbs {
		u := str(dig(th, "url"))
		w, _ := dig(th, "width").(float64)
		h, _ := dig(th, "height").(float64)
		if area := int(w * h); u != "" && area >= bestArea {
			best, bestArea = u, area
		}
	}
	return best
}

// parseClock converts "3:42" or "1:02:09" to seconds (0 if unparseable).
func parseClock(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}
