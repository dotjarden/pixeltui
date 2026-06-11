// Package scrobble submits plays to Last.fm and ListenBrainz.
//
// Last.fm uses its authenticated (signed) API: every write call carries an
// api_sig — the MD5 of the alphabetically-sorted params concatenated with the
// shared secret. A one-time desktop auth flow (GetToken → user approves in the
// browser → GetSession) yields a long-lived session key stored in config.
package scrobble

import (
	"crypto/md5" //nolint:gosec // Last.fm's API signature scheme mandates MD5
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const lastfmAPI = "https://ws.audioscrobbler.com/2.0/"

// Lastfm is a client for Last.fm's authenticated (write) API.
type Lastfm struct {
	APIKey     string
	Secret     string
	SessionKey string // empty until the user authorizes (GetSession)
	http       *http.Client
}

// NewLastfm builds a Last.fm write client. SessionKey may be empty for the
// auth-flow calls (GetToken/GetSession).
func NewLastfm(apiKey, secret, sessionKey string) *Lastfm {
	return &Lastfm{
		APIKey:     apiKey,
		Secret:     secret,
		SessionKey: sessionKey,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// sign computes api_sig: md5(k1v1k2v2…secret) over alphabetically-sorted params,
// excluding "format" and "callback" per the spec.
func (l *Lastfm) sign(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "callback" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params.Get(k))
	}
	b.WriteString(l.Secret)
	sum := md5.Sum([]byte(b.String())) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

// call POSTs a signed method and decodes the JSON reply into out (out may be nil).
func (l *Lastfm) call(method string, params url.Values, out interface{}) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("method", method)
	params.Set("api_key", l.APIKey)
	params.Set("api_sig", l.sign(params))
	params.Set("format", "json")

	resp, err := l.http.PostForm(lastfmAPI, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var errCheck struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &errCheck) == nil && errCheck.Error != 0 {
		return fmt.Errorf("last.fm: %s (code %d)", errCheck.Message, errCheck.Error)
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

// GetToken starts the desktop auth flow. It returns the request token and the
// URL the user must open to authorize this application.
func (l *Lastfm) GetToken() (token, authURL string, err error) {
	var resp struct {
		Token string `json:"token"`
	}
	if err := l.call("auth.getToken", nil, &resp); err != nil {
		return "", "", err
	}
	if resp.Token == "" {
		return "", "", fmt.Errorf("last.fm: empty auth token")
	}
	u := "https://www.last.fm/api/auth/?api_key=" + url.QueryEscape(l.APIKey) +
		"&token=" + url.QueryEscape(resp.Token)
	return resp.Token, u, nil
}

// GetSession exchanges an authorized token for a long-lived session key and the
// account's username. Call after the user approved the token in the browser.
func (l *Lastfm) GetSession(token string) (sessionKey, user string, err error) {
	var resp struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := l.call("auth.getSession", url.Values{"token": {token}}, &resp); err != nil {
		return "", "", err
	}
	if resp.Session.Key == "" {
		return "", "", fmt.Errorf("last.fm: no session key returned (token not authorized yet?)")
	}
	l.SessionKey = resp.Session.Key
	return resp.Session.Key, resp.Session.Name, nil
}

// trackParams assembles the shared artist/track/album/duration params.
func trackParams(artist, track, album string, durationSec int) url.Values {
	p := url.Values{
		"artist": {artist},
		"track":  {track},
	}
	if album != "" {
		p.Set("album", album)
	}
	if durationSec > 0 {
		p.Set("duration", strconv.Itoa(durationSec))
	}
	return p
}

// UpdateNowPlaying tells Last.fm what's playing right now (not a scrobble).
func (l *Lastfm) UpdateNowPlaying(artist, track, album string, durationSec int) error {
	if l.SessionKey == "" {
		return fmt.Errorf("last.fm: not authorized")
	}
	p := trackParams(artist, track, album, durationSec)
	p.Set("sk", l.SessionKey)
	return l.call("track.updateNowPlaying", p, nil)
}

// Love marks a track as loved on the user's Last.fm profile (track.love).
func (l *Lastfm) Love(artist, track string) error {
	if l.SessionKey == "" {
		return fmt.Errorf("last.fm: not authorized")
	}
	p := url.Values{"artist": {artist}, "track": {track}, "sk": {l.SessionKey}}
	return l.call("track.love", p, nil)
}

// Scrobble submits one play that started at startedAt.
func (l *Lastfm) Scrobble(artist, track, album string, durationSec int, startedAt time.Time) error {
	if l.SessionKey == "" {
		return fmt.Errorf("last.fm: not authorized")
	}
	p := trackParams(artist, track, album, durationSec)
	p.Set("sk", l.SessionKey)
	p.Set("timestamp", strconv.FormatInt(startedAt.Unix(), 10))
	return l.call("track.scrobble", p, nil)
}
