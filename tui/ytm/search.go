package ytm

import (
	"fmt"

	"github.com/raitonoberu/ytmusic"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ArtistHit and Album are lightweight entity results for typed ("bang") searches.
type ArtistHit struct{ Name, BrowseID, ArtURL string }

type Album struct{ Title, Artist, Year, BrowseID, ArtURL string }

// SearchArtists returns artist entities matching the query (most-relevant first).
func SearchArtists(query string, limit int) ([]ArtistHit, error) {
	res, err := ytmusic.ArtistSearch(query).Next()
	if err != nil {
		return nil, err
	}
	out := make([]ArtistHit, 0, limit)
	for _, a := range res.Artists {
		if a.BrowseID == "" || a.Artist == "" {
			continue
		}
		out = append(out, ArtistHit{
			Name:     cleanText(a.Artist),
			BrowseID: a.BrowseID,
			ArtURL:   bestThumb(a.Thumbnails),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// SearchAlbums returns album entities matching the query.
func SearchAlbums(query string, limit int) ([]Album, error) {
	res, err := ytmusic.AlbumSearch(query).Next()
	if err != nil {
		return nil, err
	}
	out := make([]Album, 0, limit)
	for _, a := range res.Albums {
		if a.BrowseID == "" || a.Title == "" {
			continue
		}
		out = append(out, Album{
			Title:    cleanText(a.Title),
			Artist:   joinArtists(a.Artists),
			Year:     a.Year,
			BrowseID: a.BrowseID,
			ArtURL:   bestThumb(a.Thumbnails),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// AlbumDetail is a fully-parsed album page: ordered tracks + header metadata.
type AlbumDetail struct {
	Album       Album // input album, with Year filled from the page when missing
	Tracks      []engine.Candidate
	ArtURL      string // album cover (largest header thumbnail)
	Description string
	IsExplicit  bool
}

// BrowseAlbum fetches an album page by browse id and parses its tracklist and
// header metadata.
func BrowseAlbum(a Album, limit int) (*AlbumDetail, error) {
	root, err := browse(map[string]interface{}{"browseId": a.BrowseID, "context": innerContext("US")})
	if err != nil {
		return nil, err
	}
	out := parseRichTrackRows(root, a.Artist, a.Title, limit, false)
	if len(out) == 0 {
		return nil, fmt.Errorf("album: no tracks found")
	}
	d := &AlbumDetail{Album: a, Tracks: out}
	// Header metadata: year (when search didn't carry one), explicit badge, and
	// cover art. New album layouts nest the header inside contents.
	if h := findHeader(root); h != nil {
		if d.Album.Year == "" {
			d.Album.Year = yearFromRuns(dig(h, "subtitle", "runs"))
		}
		d.IsExplicit = explicitFromHeader(h)
	}
	if d.Description == "" {
		d.Description = albumDescription(root)
	}
	d.ArtURL = thumbsBest(dig(root, "background", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
	if d.ArtURL == "" {
		d.ArtURL = thumbsBest(dig(root, "microformat", "microformatDataRenderer", "thumbnail", "thumbnails"))
	}
	// Tracks inherit the album cover when their rows carry none.
	for i := range d.Tracks {
		if d.Tracks[i].ArtURL == "" {
			d.Tracks[i].ArtURL = d.ArtURL
		}
	}
	return d, nil
}

// albumDescription returns the first musicDescriptionShelfRenderer description
// text found on an album page (YTM sometimes surfaces a short description).
func albumDescription(root interface{}) string {
	var text string
	var walk func(v interface{})
	walk = func(v interface{}) {
		if text != "" {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			if d, ok := t["musicDescriptionShelfRenderer"].(map[string]interface{}); ok {
				text = cleanText(runText(dig(d, "description", "runs")))
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
	return text
}

// explicitFromHeader checks the header's badges for the explicit-content badge.
func explicitFromHeader(h map[string]interface{}) bool {
	badges, _ := h["badges"].([]interface{})
	for _, b := range badges {
		if bm, ok := b.(map[string]interface{}); ok {
			if icon := str(dig(bm, "musicInlineBadgeRenderer", "icon", "iconType")); icon == "MUSIC_EXPLICIT_BADGE" {
				return true
			}
		}
	}
	return false
}

// findHeader locates the album/playlist detail header renderer anywhere in the
// response (its nesting differs between YTM layout generations).
func findHeader(root interface{}) map[string]interface{} {
	var found map[string]interface{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		if found != nil {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			for _, k := range []string{"musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer"} {
				if h, ok := t[k].(map[string]interface{}); ok {
					found = h
					return
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
	return found
}
