package ytm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ArtistPage is a full artist landing: top songs plus the album/single shelves,
// like the YouTube Music artist page it's parsed from.
type ArtistPage struct {
	Name        string
	Description string
	TopSongs    []engine.Candidate
	Albums      []Album
	Singles     []Album
}

// BrowseArtist fetches and parses an artist page by channel browse id (UC…).
func BrowseArtist(browseID string) (*ArtistPage, error) {
	root, err := browse(map[string]interface{}{"browseId": browseID, "context": innerContext("US")})
	if err != nil {
		return nil, err
	}
	page := &ArtistPage{}

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
				}
				return
			}
			if car, ok := t["musicCarouselShelfRenderer"].(map[string]interface{}); ok {
				title := strings.ToLower(runText(dig(car,
					"header", "musicCarouselShelfBasicHeaderRenderer", "title", "runs")))
				switch {
				case strings.Contains(title, "single"): // "Singles", "Singles & EPs"
					page.Singles = append(page.Singles, albumCards(car, page.Name)...)
				case strings.Contains(title, "album"): // "Albums", "Albums & Singles"
					page.Albums = append(page.Albums, albumCards(car, page.Name)...)
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
