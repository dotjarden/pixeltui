package lastfm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://ws.audioscrobbler.com/2.0/"

// FlexFloat unmarshals both JSON floats and string-encoded floats.
// Last.fm is inconsistent about which it returns depending on the endpoint.
type FlexFloat float64

func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexFloat(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = FlexFloat(n)
	return nil
}

// FlexInt unmarshals both JSON ints and string-encoded ints.
type FlexInt int

func (i *FlexInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*i = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*i = 0
		return nil
	}
	n64, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*i = 0
		return nil
	}
	*i = FlexInt(n64)
	return nil
}

type Client struct {
	APIKey string
	http   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		http: &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				// Allow all concurrent phase-1 calls to reuse the same
				// connection to Last.fm, avoiding redundant TCP handshakes.
				MaxIdleConnsPerHost: 12,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false, // keep gzip — Last.fm honours it
			},
		},
	}
}

func (c *Client) get(params url.Values) ([]byte, error) {
	params.Set("api_key", c.APIKey)
	params.Set("format", "json")

	resp, err := c.http.Get(baseURL + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Last.fm returns errors as JSON with an "error" field
	var errCheck struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errCheck); err == nil && errCheck.Error != 0 {
		return nil, fmt.Errorf("%s", errCheck.Message)
	}

	return body, nil
}

// SimilarTrack is returned by track.getSimilar.
type SimilarTrack struct {
	Name   string    `json:"name"`
	Match  FlexFloat `json:"match"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}

// Tag is a genre/mood label attached to a track or artist.
type Tag struct {
	Name string `json:"name"`
}

// SimilarArtist is returned by artist.getSimilar.
type SimilarArtist struct {
	Name  string    `json:"name"`
	Match FlexFloat `json:"match"`
}

// TopTrack is returned by artist.getTopTracks.
type TopTrack struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
	Playcount FlexInt `json:"playcount"`
	Listeners FlexInt `json:"listeners"`
}

func (t *TopTrack) ListenerCount() int { return int(t.Listeners) }

// GetSimilarTracks returns tracks similar to the given seed.
func (c *Client) GetSimilarTracks(artist, track string, limit int) ([]SimilarTrack, error) {
	body, err := c.get(url.Values{
		"method": {"track.getSimilar"},
		"artist": {artist},
		"track":  {track},
		"limit":  {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		SimilarTracks struct {
			Track []SimilarTrack `json:"track"`
		} `json:"similartracks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.SimilarTracks.Track, nil
}

// GetTrackTags returns the top genre/mood tags for a track.
func (c *Client) GetTrackTags(artist, track string) ([]string, error) {
	body, err := c.get(url.Values{
		"method": {"track.getInfo"},
		"artist": {artist},
		"track":  {track},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Track struct {
			TopTags struct {
				Tag []Tag `json:"tag"`
			} `json:"toptags"`
		} `json:"track"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(resp.Track.TopTags.Tag))
	for _, t := range resp.Track.TopTags.Tag {
		if t.Name != "" {
			tags = append(tags, t.Name)
		}
	}
	return tags, nil
}

// GetSimilarArtists returns artists similar to the given one.
func (c *Client) GetSimilarArtists(artist string, limit int) ([]SimilarArtist, error) {
	body, err := c.get(url.Values{
		"method": {"artist.getSimilar"},
		"artist": {artist},
		"limit":  {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		SimilarArtists struct {
			Artist []SimilarArtist `json:"artist"`
		} `json:"similarartists"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.SimilarArtists.Artist, nil
}

// ArtistInfo is the headline metadata from artist.getInfo.
type ArtistInfo struct {
	Name      string
	Listeners int
	Playcount int
	Tags      []string
	Summary   string // first bio paragraph, plain text
}

// GetArtistInfo returns listeners/playcount/tags/bio for an artist.
func (c *Client) GetArtistInfo(artist string) (*ArtistInfo, error) {
	body, err := c.get(url.Values{
		"method": {"artist.getInfo"},
		"artist": {artist},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Artist struct {
			Name  string `json:"name"`
			Stats struct {
				Listeners FlexInt `json:"listeners"`
				Playcount FlexInt `json:"playcount"`
			} `json:"stats"`
			Tags struct {
				Tag []Tag `json:"tag"`
			} `json:"tags"`
			Bio struct {
				Summary string `json:"summary"`
			} `json:"bio"`
		} `json:"artist"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	info := &ArtistInfo{
		Name:      resp.Artist.Name,
		Listeners: int(resp.Artist.Stats.Listeners),
		Playcount: int(resp.Artist.Stats.Playcount),
		Summary:   stripBioLink(resp.Artist.Bio.Summary),
	}
	for _, t := range resp.Artist.Tags.Tag {
		if t.Name != "" {
			info.Tags = append(info.Tags, t.Name)
		}
	}
	return info, nil
}

// stripBioLink removes the trailing "<a href…>Read more…</a>" Last.fm appends
// to bio summaries, leaving plain text.
func stripBioLink(s string) string {
	if i := strings.Index(s, "<a href"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// GetArtistTopTracks returns an artist's most-played tracks.
// The response includes per-track listener counts, which we use for popularity scoring.
func (c *Client) GetArtistTopTracks(artist string, limit int) ([]TopTrack, error) {
	body, err := c.get(url.Values{
		"method": {"artist.getTopTracks"},
		"artist": {artist},
		"limit":  {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		TopTracks struct {
			Track []TopTrack `json:"track"`
		} `json:"toptracks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.TopTracks.Track, nil
}
