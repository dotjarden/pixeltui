package ytm

import (
	"fmt"

	"github.com/raitonoberu/ytmusic"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ArtistHit and Album are lightweight entity results for typed ("bang") searches.
type ArtistHit struct{ Name, BrowseID string }

type Album struct{ Title, Artist, Year, BrowseID string }

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
		out = append(out, ArtistHit{Name: cleanText(a.Artist), BrowseID: a.BrowseID})
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
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ArtistTopSongs browses an artist page and returns its top songs.
func ArtistTopSongs(browseID string, limit int) ([]engine.Candidate, error) {
	root, err := browse(map[string]interface{}{"browseId": browseID, "context": innerContext("US")})
	if err != nil {
		return nil, err
	}
	out := parseTrackRows(root, limit)
	if len(out) == 0 {
		return nil, fmt.Errorf("artist: no songs found")
	}
	return out, nil
}

// AlbumTracks browses an album and returns its tracks. Album rows carry no
// per-row artist (it's implied), so the album's artist + title are filled in.
func AlbumTracks(a Album, limit int) ([]engine.Candidate, error) {
	root, err := browse(map[string]interface{}{"browseId": a.BrowseID, "context": innerContext("US")})
	if err != nil {
		return nil, err
	}
	var out []engine.Candidate
	var walk func(v interface{})
	walk = func(v interface{}) {
		if limit > 0 && len(out) >= limit {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			if mr, ok := t["musicResponsiveListItemRenderer"].(map[string]interface{}); ok {
				cols, _ := mr["flexColumns"].([]interface{})
				if title := cleanTitle(cleanText(flexText(cols, 0))); title != "" {
					out = append(out, engine.Candidate{Track: title, Artist: a.Artist, Album: a.Title})
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
	if len(out) == 0 {
		return nil, fmt.Errorf("album: no tracks found")
	}
	return out, nil
}
