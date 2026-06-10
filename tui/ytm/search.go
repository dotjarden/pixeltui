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
	Album  Album // input album, with Year filled from the page when missing
	Tracks []engine.Candidate
	ArtURL string // album cover (largest header thumbnail)
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
	// Header metadata: year (when search didn't carry one) and cover art. New
	// album layouts nest the header inside contents, so find it wherever it is.
	if d.Album.Year == "" {
		if h := findHeader(root); h != nil {
			d.Album.Year = yearFromRuns(dig(h, "subtitle", "runs"))
		}
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
