// Package subsonic is a minimal OpenSubsonic/Subsonic REST API client.
//
// It lets pixeltui browse and stream a self-hosted music server (Navidrome,
// Airsonic, etc.). Streaming returns a direct, already-playable audio URL, so
// mpv/ffplay can fetch it without yt-dlp.
//
// Auth uses the token scheme: a random salt plus token = md5(password+salt),
// generated once per client so that StreamURL/CoverArtURL strings stay valid.
package subsonic

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/engine"
)

const (
	apiVersion = "1.16.1"   // Subsonic API version we speak
	clientName = "pixeltui" // c= identifier sent to the server
)

// Client talks to a single Subsonic-compatible server. Construct with NewClient.
type Client struct {
	server string // base URL, e.g. "https://music.example.com" (no trailing /rest)
	user   string
	salt   string // stable per-client salt (hex)
	token  string // hex(md5(password + salt)), stable per-client
	http   *http.Client
}

// NewClient builds a client for server (like "https://music.example.com", no
// trailing /rest). The salt and token are generated once so URLs returned by
// StreamURL/CoverArtURL remain fetchable for the life of the client.
func NewClient(server, user, password string) *Client {
	salt := randomSalt()
	sum := md5.Sum([]byte(password + salt))
	return &Client{
		server: strings.TrimRight(server, "/"),
		user:   user,
		salt:   salt,
		token:  hex.EncodeToString(sum[:]),
		http:   &http.Client{Timeout: 8 * time.Second},
	}
}

// randomSalt returns a hex string from >= 6 random bytes.
func randomSalt() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should not fail; fall back to a time-derived value.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// authParams returns the shared auth/query parameters for every request.
func (c *Client) authParams() url.Values {
	v := url.Values{}
	v.Set("u", c.user)
	v.Set("t", c.token)
	v.Set("s", c.salt)
	v.Set("v", apiVersion)
	v.Set("c", clientName)
	v.Set("f", "json")
	return v
}

// endpoint builds a full {server}/rest/{method} URL with auth + extra params.
func (c *Client) endpoint(method string, extra url.Values) string {
	v := c.authParams()
	for key, vals := range extra {
		for _, val := range vals {
			v.Add(key, val)
		}
	}
	return fmt.Sprintf("%s/rest/%s?%s", c.server, method, v.Encode())
}

// --- Wire types ---------------------------------------------------------------

// envelope wraps every Subsonic JSON response.
type envelope struct {
	Resp struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		// Result containers (only the one matching the method is populated).
		SearchResult3 struct {
			Song []Child `json:"song"`
		} `json:"searchResult3"`
		Starred2 struct {
			Song []Child `json:"song"`
		} `json:"starred2"`
		Playlists struct {
			Playlist []playlistJSON `json:"playlist"`
		} `json:"playlists"`
		Playlist struct {
			Entry []Child `json:"entry"`
		} `json:"playlist"`
	} `json:"subsonic-response"`
}

// Child is a Subsonic song/media item (the "Child" type in the spec).
type Child struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"` // seconds
	CoverArt string `json:"coverArt"` // cover-art id
}

// playlistJSON is the wire form of a playlist summary.
type playlistJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"songCount"`
}

// Playlist is a public playlist summary.
type Playlist struct {
	ID        string
	Name      string
	SongCount int
}

// --- HTTP plumbing ------------------------------------------------------------

// do performs a GET for method, decodes the envelope, and checks status.
func (c *Client) do(method string, extra url.Values) (*envelope, error) {
	resp, err := c.http.Get(c.endpoint(method, extra))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subsonic: %s returned HTTP %d", method, resp.StatusCode)
	}
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("subsonic: decode %s: %w", method, err)
	}
	if env.Resp.Status != "ok" {
		if env.Resp.Error != nil {
			return nil, fmt.Errorf("subsonic: %s (code %d)", env.Resp.Error.Message, env.Resp.Error.Code)
		}
		return nil, errors.New("subsonic: request failed")
	}
	return &env, nil
}

// --- Public API ---------------------------------------------------------------

// Ping is a health/auth check.
func (c *Client) Ping() error {
	_, err := c.do("ping.view", nil)
	return err
}

// Search runs a search3 query and returns matching songs as candidates.
// limit caps songCount (artists/albums are not requested).
func (c *Client) Search(query string, limit int) ([]engine.Candidate, error) {
	if limit <= 0 {
		limit = 20
	}
	v := url.Values{}
	v.Set("query", query)
	v.Set("songCount", strconv.Itoa(limit))
	v.Set("artistCount", "0")
	v.Set("albumCount", "0")
	env, err := c.do("search3.view", v)
	if err != nil {
		return nil, err
	}
	return c.toCandidates(env.Resp.SearchResult3.Song), nil
}

// Starred returns the user's starred/loved songs via getStarred2.
func (c *Client) Starred() ([]engine.Candidate, error) {
	env, err := c.do("getStarred2.view", nil)
	if err != nil {
		return nil, err
	}
	return c.toCandidates(env.Resp.Starred2.Song), nil
}

// Playlists returns the user's playlists (summaries only).
func (c *Client) Playlists() ([]Playlist, error) {
	env, err := c.do("getPlaylists.view", nil)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0, len(env.Resp.Playlists.Playlist))
	for _, p := range env.Resp.Playlists.Playlist {
		out = append(out, Playlist{ID: p.ID, Name: p.Name, SongCount: p.SongCount})
	}
	return out, nil
}

// PlaylistTracks returns the songs of one playlist via getPlaylist.
func (c *Client) PlaylistTracks(id string) ([]engine.Candidate, error) {
	v := url.Values{}
	v.Set("id", id)
	env, err := c.do("getPlaylist.view", v)
	if err != nil {
		return nil, err
	}
	return c.toCandidates(env.Resp.Playlist.Entry), nil
}

// StreamURL builds a direct, auth-bearing stream URL for songID. It is not
// fetched here; mpv/ffplay can play the returned string directly.
func (c *Client) StreamURL(songID string) string {
	v := url.Values{}
	v.Set("id", songID)
	return c.endpoint("stream.view", v)
}

// CoverArtURL builds a direct, auth-bearing cover-art URL for coverID.
func (c *Client) CoverArtURL(coverID string) string {
	v := url.Values{}
	v.Set("id", coverID)
	return c.endpoint("getCoverArt.view", v)
}

// --- Conversion ---------------------------------------------------------------

// toCandidate maps a Subsonic Child song to an engine.Candidate, populating the
// Subsonic-specific Source/StreamURL/ArtURL fields. VideoID is left empty.
func (c *Client) toCandidate(s Child) engine.Candidate {
	cand := engine.Candidate{
		Track:       s.Title,
		Artist:      s.Artist,
		DurationSec: s.Duration,
		Source:      "subsonic",
		StreamURL:   c.StreamURL(s.ID),
	}
	if s.CoverArt != "" {
		cand.ArtURL = c.CoverArtURL(s.CoverArt)
	}
	return cand
}

// toCandidates maps a slice of songs.
func (c *Client) toCandidates(songs []Child) []engine.Candidate {
	out := make([]engine.Candidate, 0, len(songs))
	for _, s := range songs {
		out = append(out, c.toCandidate(s))
	}
	return out
}
