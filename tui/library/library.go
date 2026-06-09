// Package library manages a portable, interoperable on-disk music library
// under ~/.pixeltui/library/. Everything is stored in open standard formats
// (M3U8, XSPF, ListenBrainz JSONL) so other apps can read it too. Pure stdlib.
package library

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// likedPlaylist is the reserved playlist name backing Like/Unlike/Liked.
const likedPlaylist = "Liked Songs"

// LikedName is the reserved "Liked Songs" playlist name (exported so callers can
// skip/protect it when listing or editing user playlists).
const LikedName = likedPlaylist

// Store is a handle to the on-disk library rooted at <dataDir>/library/.
type Store struct {
	root      string // <dataDir>/library
	playlists string // <root>/playlists
	history   string // <root>/history.jsonl
	session   string // <root>/session.json

	mu sync.Mutex // serializes writes (history append, like edits, sessions)
}

// Open creates <dataDir>/library/ (and its playlists subdir) if missing and
// returns a Store. dataDir is expected already expanded (e.g. "~/.pixeltui"
// resolved to an absolute path).
func Open(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "library")
	playlists := filepath.Join(root, "playlists")
	if err := os.MkdirAll(playlists, 0o755); err != nil {
		return nil, fmt.Errorf("library: create dirs: %w", err)
	}
	return &Store{
		root:      root,
		playlists: playlists,
		history:   filepath.Join(root, "history.jsonl"),
		session:   filepath.Join(root, "session.json"),
	}, nil
}

// ---------- helpers ----------

// sanitize strips path separators and other risky bytes from a playlist name
// so it maps to a single safe filename. Kept deliberately simple.
func sanitize(name string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		}
		return r
	}
	out := strings.Map(repl, strings.TrimSpace(name))
	out = strings.Trim(out, ". ")
	if out == "" {
		out = "untitled"
	}
	return out
}

// playlistPath returns the .m3u8 path for a (sanitized) playlist name.
func (s *Store) playlistPath(name string) string {
	return filepath.Join(s.playlists, sanitize(name)+".m3u8")
}

// uri returns the canonical playable/identifying URI for a candidate:
// direct StreamURL if set, else the YouTube Music watch URL, else "".
func uri(c engine.Candidate) string {
	if c.StreamURL != "" {
		return c.StreamURL
	}
	if c.VideoID != "" {
		return "https://music.youtube.com/watch?v=" + c.VideoID
	}
	return ""
}

// likeKey is the dedupe key for likes: lowercased Track+"|"+Artist.
func likeKey(c engine.Candidate) string {
	return strings.ToLower(c.Track) + "|" + strings.ToLower(c.Artist)
}

// ---------- Playlists (M3U8) ----------

// ListPlaylists returns the names (without extension) of all stored playlists,
// sorted. The reserved "Liked Songs" playlist is included if present.
func (s *Store) ListPlaylists() ([]string, error) {
	entries, err := os.ReadDir(s.playlists)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".m3u8") {
			continue
		}
		names = append(names, strings.TrimSuffix(n, ".m3u8"))
	}
	sort.Strings(names)
	return names, nil
}

// DeletePlaylist removes a user playlist. The reserved "Liked Songs" is refused.
func (s *Store) DeletePlaylist(name string) error {
	if name == likedPlaylist {
		return fmt.Errorf("can't delete the reserved %q playlist", likedPlaylist)
	}
	return os.Remove(s.playlistPath(name))
}

// RenamePlaylist renames a user playlist. The reserved "Liked Songs" is refused
// as either source or destination.
func (s *Store) RenamePlaylist(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("empty playlist name")
	}
	if oldName == likedPlaylist || newName == likedPlaylist {
		return fmt.Errorf("%q is reserved", likedPlaylist)
	}
	return os.Rename(s.playlistPath(oldName), s.playlistPath(newName))
}

// SavePlaylist writes tracks to <playlists>/<name>.m3u8 in standard EXTM3U
// form, with an extra #PIXELTUI comment per track to round-trip our metadata.
// Tracks with no derivable URI are skipped.
func (s *Store) SavePlaylist(name string, tracks []engine.Candidate) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, c := range tracks {
		if c.Track == "" && c.Artist == "" {
			continue // genuinely empty; nothing to record
		}
		// Standard, widely-understood metadata line.
		fmt.Fprintf(&b, "#EXTINF:%d,%s - %s\n", c.DurationSec, c.Artist, c.Track)
		// Our extra fields, as an ignored comment for other players.
		fmt.Fprintf(&b, "#PIXELTUI:videoId=%s;source=%s;art=%s\n",
			escapeKV(c.VideoID), escapeKV(c.Source), escapeKV(c.ArtURL))
		// URI line only when we have one; a bare-metadata track (e.g. a liked
		// recommendation not yet resolved) still round-trips via the lines above.
		if u := uri(c); u != "" {
			b.WriteString(u + "\n")
		}
	}
	return atomicWrite(s.playlistPath(name), []byte(b.String()))
}

// LoadPlaylist parses <playlists>/<name>.m3u8 back into candidates. It uses our
// #PIXELTUI line when present and always parses standard #EXTINF; files with
// only standard EXTINF (no PIXELTUI) still yield Artist/Track/DurationSec.
func (s *Store) LoadPlaylist(name string) ([]engine.Candidate, error) {
	f, err := os.Open(s.playlistPath(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []engine.Candidate
	var cur engine.Candidate
	var have bool // a track is being accumulated (saw an EXTINF or PIXELTUI line)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "" || line == "#EXTM3U":
			continue
		case strings.HasPrefix(line, "#EXTINF:"):
			if have { // previous track had no URI line — flush it
				out = append(out, cur)
				cur = engine.Candidate{}
			}
			dur, artist, track := parseExtinf(line[len("#EXTINF:"):])
			cur.DurationSec = dur
			cur.Artist = artist
			cur.Track = track
			have = true
		case strings.HasPrefix(line, "#PIXELTUI:"):
			parsePixeltui(line[len("#PIXELTUI:"):], &cur)
			have = true
		case strings.HasPrefix(line, "#"):
			continue // unknown comment from another player; ignore
		default:
			// A URI line terminates the current track.
			cur.StreamURL, cur.VideoID = splitURI(line, cur.VideoID)
			out = append(out, cur)
			cur = engine.Candidate{}
			have = false
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// A trailing EXTINF/PIXELTUI with no URI line still yields a track.
	if have {
		out = append(out, cur)
	}
	return out, nil
}

// parseExtinf parses "<seconds>,<Artist> - <Track>" (the part after EXTINF:).
// Falls back gracefully when the "Artist - Track" split is absent.
func parseExtinf(s string) (dur int, artist, track string) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return 0, "", strings.TrimSpace(s)
	}
	// Duration may carry attributes (e.g. `123 tvg-id="x"`); take leading int.
	durField := strings.TrimSpace(s[:comma])
	if sp := strings.IndexByte(durField, ' '); sp >= 0 {
		durField = durField[:sp]
	}
	dur, _ = strconv.Atoi(durField)
	title := strings.TrimSpace(s[comma+1:])
	if i := strings.Index(title, " - "); i >= 0 {
		return dur, strings.TrimSpace(title[:i]), strings.TrimSpace(title[i+3:])
	}
	return dur, "", title
}

// parsePixeltui parses "k=v;k=v" pairs from our comment into c.
func parsePixeltui(s string, c *engine.Candidate) {
	for _, kv := range strings.Split(s, ";") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(kv[:eq])
		v := unescapeKV(kv[eq+1:])
		switch k {
		case "videoId":
			c.VideoID = v
		case "source":
			c.Source = v
		case "art":
			c.ArtURL = v
		}
	}
}

// splitURI assigns a playlist URI line to the right field. A YouTube Music
// watch URL populates VideoID (if not already known); anything else becomes a
// direct StreamURL. Returns (streamURL, videoID).
func splitURI(line, knownVideoID string) (string, string) {
	const yt = "https://music.youtube.com/watch?v="
	if strings.HasPrefix(line, yt) {
		vid := line[len(yt):]
		if amp := strings.IndexByte(vid, '&'); amp >= 0 {
			vid = vid[:amp]
		}
		if vid == "" {
			vid = knownVideoID
		}
		return "", vid
	}
	return line, knownVideoID
}

// escapeKV / unescapeKV keep our `k=v;k=v` comment grammar intact when values
// contain ';' or '=' (notably in URLs).
func escapeKV(v string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\,", "=", "\\e", "\n", " ")
	return r.Replace(v)
}

func unescapeKV(v string) string {
	r := strings.NewReplacer("\\\\", "\\", "\\,", ";", "\\e", "=")
	return r.Replace(v)
}

// ---------- XSPF export ----------

// xspf mirrors the http://xspf.org/ns/0/ schema for marshaling.
type xspf struct {
	XMLName   xml.Name    `xml:"playlist"`
	Version   string      `xml:"version,attr"`
	XMLNS     string      `xml:"xmlns,attr"`
	Title     string      `xml:"title,omitempty"`
	TrackList []xspfTrack `xml:"trackList>track"`
}

type xspfTrack struct {
	Title    string `xml:"title,omitempty"`
	Creator  string `xml:"creator,omitempty"`
	Duration int    `xml:"duration,omitempty"` // milliseconds per spec
	Location string `xml:"location,omitempty"`
	Image    string `xml:"image,omitempty"`
}

// ExportXSPF writes the named playlist as a valid XSPF 1.0 document to w.
func (s *Store) ExportXSPF(name string, w io.Writer) error {
	tracks, err := s.LoadPlaylist(name)
	if err != nil {
		return err
	}
	doc := xspf{Version: "1", XMLNS: "http://xspf.org/ns/0/", Title: name}
	for _, c := range tracks {
		doc.TrackList = append(doc.TrackList, xspfTrack{
			Title:    c.Track,
			Creator:  c.Artist,
			Duration: c.DurationSec * 1000,
			Location: uri(c),
			Image:    c.ArtURL,
		})
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err = io.WriteString(w, "\n")
	return err
}

// ---------- Likes ----------

// Like adds c to the reserved "Liked Songs" playlist (deduped by likeKey).
func (s *Store) Like(c engine.Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	liked, _ := s.LoadPlaylist(likedPlaylist) // missing file -> empty
	key := likeKey(c)
	for _, e := range liked {
		if likeKey(e) == key {
			return nil // already liked
		}
	}
	liked = append(liked, c)
	return s.SavePlaylist(likedPlaylist, liked)
}

// Unlike removes c (matched by likeKey) from "Liked Songs".
func (s *Store) Unlike(c engine.Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	liked, err := s.LoadPlaylist(likedPlaylist)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	key := likeKey(c)
	out := liked[:0]
	for _, e := range liked {
		if likeKey(e) != key {
			out = append(out, e)
		}
	}
	return s.SavePlaylist(likedPlaylist, out)
}

// IsLiked reports whether c is in "Liked Songs" (matched by likeKey).
func (s *Store) IsLiked(c engine.Candidate) bool {
	liked, err := s.LoadPlaylist(likedPlaylist)
	if err != nil {
		return false
	}
	key := likeKey(c)
	for _, e := range liked {
		if likeKey(e) == key {
			return true
		}
	}
	return false
}

// Liked returns all liked tracks (insertion order, oldest first).
func (s *Store) Liked() []engine.Candidate {
	liked, _ := s.LoadPlaylist(likedPlaylist)
	return liked
}

// ---------- Listening history (ListenBrainz-compatible) ----------

// lbListen is one ListenBrainz listen object, used for both JSONL lines and
// the submit-listens payload.
type lbListen struct {
	ListenedAt    int64   `json:"listened_at"`
	TrackMetadata lbTrack `json:"track_metadata"`
}

type lbTrack struct {
	ArtistName     string `json:"artist_name"`
	TrackName      string `json:"track_name"`
	AdditionalInfo lbInfo `json:"additional_info"`
}

type lbInfo struct {
	MusicService string `json:"music_service"`
	OriginURL    string `json:"origin_url,omitempty"`
	DurationMS   int    `json:"duration_ms,omitempty"`
}

// listenOf builds the ListenBrainz listen object for a play of c at time at.
func listenOf(c engine.Candidate, at time.Time) lbListen {
	svc := c.Source
	if svc == "" {
		svc = "youtube"
	}
	return lbListen{
		ListenedAt: at.Unix(),
		TrackMetadata: lbTrack{
			ArtistName: c.Artist,
			TrackName:  c.Track,
			AdditionalInfo: lbInfo{
				MusicService: svc,
				OriginURL:    uri(c),
				DurationMS:   c.DurationSec * 1000,
			},
		},
	}
}

// AddListen appends one ListenBrainz-shaped JSON line to history.jsonl.
func (s *Store) AddListen(c engine.Candidate, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(listenOf(c, at))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.history, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// History returns up to limit listens, most-recent first. limit <= 0 means all.
func (s *Store) History(limit int) ([]engine.Candidate, error) {
	listens, err := s.Listens(limit)
	if err != nil {
		return nil, err
	}
	out := make([]engine.Candidate, len(listens))
	for i, l := range listens {
		out[i] = l.Candidate
	}
	return out, nil
}

// Listen is one history entry with its timestamp.
type Listen struct {
	Candidate engine.Candidate
	At        time.Time
}

// Listens returns up to limit history entries with timestamps, most-recent
// first. limit <= 0 means all.
func (s *Store) Listens(limit int) ([]Listen, error) {
	f, err := os.Open(s.history)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Listen
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var l lbListen
		if json.Unmarshal([]byte(line), &l) != nil {
			continue // skip malformed lines rather than failing the whole read
		}
		out = append(out, Listen{Candidate: candidateOf(l), At: time.Unix(l.ListenedAt, 0)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// Reverse to most-recent first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// candidateOf reconstructs a Candidate from a stored listen (best-effort:
// the URL distinguishes YouTube VideoID vs direct StreamURL).
func candidateOf(l lbListen) engine.Candidate {
	c := engine.Candidate{
		Track:       l.TrackMetadata.TrackName,
		Artist:      l.TrackMetadata.ArtistName,
		DurationSec: l.TrackMetadata.AdditionalInfo.DurationMS / 1000,
		Source:      l.TrackMetadata.AdditionalInfo.MusicService,
	}
	c.StreamURL, c.VideoID = splitURI(l.TrackMetadata.AdditionalInfo.OriginURL, "")
	return c
}

// lbSubmit is the ListenBrainz submit-listens document for export.
type lbSubmit struct {
	ListenType string     `json:"listen_type"`
	Payload    []lbListen `json:"payload"`
}

// ExportListenBrainz writes the entire history as a ListenBrainz submit-listens
// JSON document (listen_type "import") to w.
func (s *Store) ExportListenBrainz(w io.Writer) error {
	f, err := os.Open(s.history)
	doc := lbSubmit{ListenType: "import", Payload: []lbListen{}}
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var l lbListen
			if json.Unmarshal([]byte(line), &l) == nil {
				doc.Payload = append(doc.Payload, l)
			}
		}
		if err := sc.Err(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// ---------- Session resume ----------

// Session is a resumable playback snapshot.
type Session struct {
	Queue       []engine.Candidate `json:"queue"`
	NowPlaying  engine.Candidate   `json:"now_playing"`
	PositionSec float64            `json:"position_sec"`
}

// SaveSession writes the session snapshot to session.json.
func (s *Store) SaveSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.session, data)
}

// LoadSession reads session.json. ok is false when no session exists.
func (s *Store) LoadSession() (Session, bool) {
	data, err := os.ReadFile(s.session)
	if err != nil {
		return Session{}, false
	}
	var sess Session
	if json.Unmarshal(data, &sess) != nil {
		return Session{}, false
	}
	return sess, true
}

// ---------- io ----------

// atomicWrite writes data to path via a temp file + rename so readers never
// observe a half-written file.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
