// Package server exposes pixeltui's library and sources over HTTP so a phone
// (or any client) can browse, search, and stream "from anywhere" via a BYO
// tunnel. It's the backend for the companion app — opt in with `pixeltui serve`.
//
// Transport: plain REST for actions + Server-Sent Events for live state (no
// WebSocket dependency). Auth: per-device bearer tokens, paired once via a QR
// the command prints. Streaming: Subsonic/local are proxied/served directly
// (range-aware); YouTube resolves to a pre-signed m4a CDN URL (innertube.go)
// and is proxied the same way.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/lastfm"
	"github.com/dotjarden/pixeltui/tui/library"
	"github.com/dotjarden/pixeltui/tui/local"
	"github.com/dotjarden/pixeltui/tui/subsonic"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// StreamURLCache caches resolved CDN URLs (implemented by *store.Cache).
type StreamURLCache interface {
	GetStreamURL(key string) (string, bool)
	PutStreamURL(key, url string, expire int64)
}

// Config holds the server's dependencies and bind settings.
type Config struct {
	DataDir     string
	Name        string // shown to clients (defaults to hostname)
	Addr        string // bind address, e.g. ":8787"
	URL         string // public base URL for the pairing QR (override for tunnels)
	Library     *library.Store
	Subsonic    *subsonic.Client
	LocalDirs   []string
	StreamCache StreamURLCache // optional: makes YouTube replays resolve instantly
	Lastfm      *lastfm.Client // optional: artist listener stats on artist pages
}

type server struct {
	cfg     Config
	devices *deviceStore
	sse     *sseHub

	// Pairing codes are single-use: rotated after every successful pair and
	// after maxPairFails bad attempts (with a growing per-attempt delay), so
	// the 6-char code can't be brute-forced or reused.
	pairMu    sync.Mutex
	code      string // current pairing code
	pairFails int    // consecutive bad attempts against the current code

	// Collapses concurrent stream-URL resolutions for the same track (AVPlayer
	// opens several range requests at once) onto a single resolve.
	resolveGroup singleflight.Group
}

// maxPairFails rotates the pairing code after this many consecutive bad codes.
const maxPairFails = 5

// Run starts the HTTP server (blocking). It prints pairing instructions + a QR.
func Run(cfg Config) error {
	if cfg.Addr == "" {
		cfg.Addr = ":8787"
	}
	if cfg.Name == "" {
		if h, err := osHostname(); err == nil {
			cfg.Name = h
		} else {
			cfg.Name = "pixeltui"
		}
	}
	s := &server{
		cfg:     cfg,
		devices: openDeviceStore(cfg.DataDir),
		code:    randCode(),
		sse:     newSSEHub(),
	}

	s.printPairing()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// handler builds the HTTP routes (separated so tests can exercise it directly).
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/pair", s.handlePair)
	mux.HandleFunc("/api/sources", s.auth(s.handleSources))
	mux.HandleFunc("/api/search", s.auth(s.handleSearch))
	mux.HandleFunc("/api/search/entities", s.auth(s.handleSearchEntities))
	mux.HandleFunc("/api/liked", s.auth(s.handleLiked))
	mux.HandleFunc("/api/playlists", s.auth(s.handlePlaylists))
	mux.HandleFunc("/api/playlist", s.auth(s.handlePlaylist))
	mux.HandleFunc("/api/local", s.auth(s.handleLocal))
	mux.HandleFunc("/api/subsonic/starred", s.auth(s.handleSubStarred))
	mux.HandleFunc("/api/subsonic/playlists", s.auth(s.handleSubPlaylists))
	mux.HandleFunc("/api/subsonic/playlist", s.auth(s.handleSubPlaylist))
	mux.HandleFunc("/api/stream", s.auth(s.handleStream))
	mux.HandleFunc("/api/art", s.auth(s.handleArt))
	mux.HandleFunc("/api/lyrics", s.auth(s.handleLyrics))
	mux.HandleFunc("/api/charts", s.auth(s.handleCharts))
	mux.HandleFunc("/api/artist", s.auth(s.handleArtist))
	mux.HandleFunc("/api/album", s.auth(s.handleAlbum))
	mux.HandleFunc("/api/devices", s.auth(s.handleDevices))
	mux.HandleFunc("/api/devices/revoke", s.auth(s.handleRevoke))
	mux.HandleFunc("/events", s.auth(s.handleEvents))
	return withCORS(mux)
}

// withCORS allows browser/PWA clients (and Flutter web) to call the API, and
// answers preflight requests. iOS/Android native clients are unaffected.
func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ── auth ────────────────────────────────────────────────────────────────────

// deviceIDKey carries the authenticated device's id through the request context.
type ctxKey int

const deviceIDKey ctxKey = iota

// deviceID returns the authenticated device id for a request ("" if none).
func deviceID(r *http.Request) string {
	id, _ := r.Context().Value(deviceIDKey).(string)
	return id
}

// auth wraps a handler, requiring a valid device token (Authorization: Bearer
// <token>, or ?token= for media elements that can't set headers).
func (s *server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		id, ok := s.devices.valid(tok)
		if !ok {
			http.Error(w, "unauthorized — pair this device first", http.StatusUnauthorized)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), deviceIDKey, id)))
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "name": s.cfg.Name, "service": "pixeltui"})
}

// handlePair exchanges the current pairing code for a durable device token.
// Codes are single-use; bad attempts are slowed down and eventually rotate
// the code entirely.
func (s *server) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct{ Code, Name string }
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Code == "" {
		body.Code = r.URL.Query().Get("code")
	}

	s.pairMu.Lock()
	if !constantEqual(body.Code, s.code) {
		s.pairFails++
		delay := time.Duration(s.pairFails) * 300 * time.Millisecond
		rotated := false
		if s.pairFails >= maxPairFails {
			s.code = randCode()
			s.pairFails = 0
			rotated = true
		}
		s.pairMu.Unlock()
		time.Sleep(delay) // slow down guessing without holding the lock
		if rotated {
			fmt.Println("\n  Too many bad pairing attempts — code rotated.")
			s.printPairing()
		}
		http.Error(w, "bad pairing code", http.StatusForbidden)
		return
	}
	// Success: the code is spent — rotate it for the next device.
	s.code = randCode()
	s.pairFails = 0
	s.pairMu.Unlock()

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "device"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	tok, id := s.devices.add(name)
	fmt.Printf("\n  ✓ Paired %q (device %s) — next code:\n", name, id)
	s.printPairing()
	writeJSON(w, map[string]any{"token": tok, "device_id": id, "server": s.cfg.Name})
}

// handleDevices lists paired devices (no token material).
func (s *server) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"devices": s.devices.list(deviceID(r))})
}

// handleRevoke unpairs a device by id; its token stops working immediately.
func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ ID string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.ID == "" {
		body.ID = r.URL.Query().Get("id")
	}
	if body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if !s.devices.revoke(body.ID) {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ── catalog ─────────────────────────────────────────────────────────────────

func (s *server) handleSources(w http.ResponseWriter, _ *http.Request) {
	srcs := []string{"youtube"}
	if s.cfg.Subsonic != nil {
		srcs = append(srcs, "subsonic")
	}
	if len(s.cfg.LocalDirs) > 0 {
		srcs = append(srcs, "local")
	}
	writeJSON(w, map[string]any{"sources": srcs, "name": s.cfg.Name})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	var (
		res []engine.Candidate
		err error
	)
	switch r.URL.Query().Get("source") {
	case "subsonic":
		if s.cfg.Subsonic == nil {
			http.Error(w, "subsonic not configured", http.StatusBadRequest)
			return
		}
		res, err = s.cfg.Subsonic.Search(q, 40)
	case "local":
		res, err = s.localSearch(q)
	default:
		// YouTube search is the slow path (~1s) — short TTL cache makes
		// repeat/debounced queries instant.
		key := strings.ToLower(strings.TrimSpace(q))
		if v, ok := searchCache.get(key); ok {
			res = v.([]engine.Candidate)
			break
		}
		res, err = ytm.Search(q, 40)
		if err == nil {
			searchCache.put(key, res)
		}
	}
	s.writeTracks(w, res, err)
}

func (s *server) handleLiked(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Library == nil {
		s.writeTracks(w, nil, nil)
		return
	}
	s.writeTracks(w, s.cfg.Library.Liked(), nil)
}

func (s *server) handlePlaylists(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Library == nil {
		writeJSON(w, map[string]any{"playlists": []string{}})
		return
	}
	names, _ := s.cfg.Library.ListPlaylists()
	writeJSON(w, map[string]any{"playlists": names})
}

func (s *server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if s.cfg.Library == nil || name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	tracks, err := s.cfg.Library.LoadPlaylist(name)
	s.writeTracks(w, tracks, err)
}

func (s *server) handleLocal(w http.ResponseWriter, _ *http.Request) {
	tracks, ok := local.Cached(s.cfg.DataDir)
	if !ok {
		tracks, _ = local.Scan(s.cfg.DataDir, s.cfg.LocalDirs)
	}
	s.writeTracks(w, tracks, nil)
}

func (s *server) handleSubStarred(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Subsonic == nil {
		http.Error(w, "subsonic not configured", http.StatusBadRequest)
		return
	}
	tracks, err := s.cfg.Subsonic.Starred()
	s.writeTracks(w, tracks, err)
}

func (s *server) handleSubPlaylists(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Subsonic == nil {
		http.Error(w, "subsonic not configured", http.StatusBadRequest)
		return
	}
	pls, err := s.cfg.Subsonic.Playlists()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"playlists": pls})
}

func (s *server) handleSubPlaylist(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if s.cfg.Subsonic == nil || id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	tracks, err := s.cfg.Subsonic.PlaylistTracks(id)
	s.writeTracks(w, tracks, err)
}

func (s *server) localSearch(q string) ([]engine.Candidate, error) {
	all, ok := local.Cached(s.cfg.DataDir)
	if !ok {
		var err error
		if all, err = local.Scan(s.cfg.DataDir, s.cfg.LocalDirs); err != nil {
			return nil, err
		}
	}
	ql := strings.ToLower(q)
	var out []engine.Candidate
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Track), ql) || strings.Contains(strings.ToLower(c.Artist), ql) {
			out = append(out, c)
		}
	}
	return out, nil
}

// ── streaming ───────────────────────────────────────────────────────────────

// handleStream serves audio for a track id: lo:<b64 path> (local file, range),
// su:<songid> (Subsonic proxy), yt:<videoid> (transcode — later phase).
func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	kind, val, ok := splitID(id)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	switch kind {
	case "lo":
		path, err := base64.URLEncoding.DecodeString(val)
		if err != nil || !s.localAllowed(string(path)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.ServeFile(w, r, string(path)) // range-aware
	case "su":
		if s.cfg.Subsonic == nil {
			http.Error(w, "subsonic not configured", http.StatusBadRequest)
			return
		}
		s.proxy(w, r, s.cfg.Subsonic.StreamURL(val))
	case "yt":
		s.streamYouTube(w, r, val)
	default:
		http.Error(w, "unknown source", http.StatusBadRequest)
	}
}

// handleArt proxies a Subsonic cover (keeps server creds off the client). For
// other sources the client uses the public art URL in the track payload.
func (s *server) handleArt(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	kind, val, ok := splitID(id)
	switch {
	case ok && kind == "su" && s.cfg.Subsonic != nil:
		s.proxy(w, r, s.cfg.Subsonic.CoverArtURL(val))
	case ok && kind == "lo":
		s.localArt(w, r, val)
	default:
		http.Error(w, "bad id", http.StatusBadRequest)
	}
}

// localArt serves the embedded cover of a local file, extracting it with
// ffmpeg on first request and caching it under <dataDir>/artcache. Files with
// no embedded art get a cached negative marker so we don't re-run ffmpeg.
func (s *server) localArt(w http.ResponseWriter, r *http.Request, encPath string) {
	raw, err := base64.URLEncoding.DecodeString(encPath)
	if err != nil || !s.localAllowed(string(raw)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	path := string(raw)

	dir := filepath.Join(s.cfg.DataDir, "artcache")
	_ = os.MkdirAll(dir, 0o755)
	sum := sha256.Sum256(raw)
	cached := filepath.Join(dir, hex.EncodeToString(sum[:8])+".jpg")
	negative := cached + ".none"

	if _, err := os.Stat(negative); err == nil {
		http.Error(w, "no embedded art", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(cached); err != nil {
		ff, lerr := exec.LookPath("ffmpeg")
		if lerr != nil {
			http.Error(w, "ffmpeg not available", http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		tmp := cached + ".tmp.jpg"
		// -an drops audio; the attached picture stream becomes a JPEG.
		cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error",
			"-i", path, "-an", "-frames:v", "1", "-q:v", "4", "-y", tmp)
		if cmd.Run() != nil {
			_ = os.WriteFile(negative, nil, 0o644)
			os.Remove(tmp) //nolint:errcheck
			http.Error(w, "no embedded art", http.StatusNotFound)
			return
		}
		_ = os.Rename(tmp, cached)
	}
	w.Header().Set("Cache-Control", "max-age=86400")
	http.ServeFile(w, r, cached)
}

// localAllowed reports whether path is a real audio file under a configured dir.
func (s *server) localAllowed(path string) bool {
	if !local.IsAudio(path) {
		return false
	}
	clean := filepath.Clean(path)
	for _, d := range s.cfg.LocalDirs {
		base := filepath.Clean(d)
		if clean == base || strings.HasPrefix(clean, base+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// proxy forwards a GET (honouring Range) to target and streams it back.
func (s *server) proxy(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad upstream", http.StatusBadGateway)
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	// googlevideo rejects requests with no UA; harmless for other upstreams.
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Accept-Ranges", "Content-Range"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// ── SSE ─────────────────────────────────────────────────────────────────────

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.sse.add()
	defer s.sse.remove(ch)
	fmt.Fprintf(w, "event: hello\ndata: %q\n\n", s.cfg.Name)
	fl.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprint(w, msg)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// ── track DTO ───────────────────────────────────────────────────────────────

type trackDTO struct {
	ID       string `json:"id"` // opaque stream id (lo:/su:/yt:)
	Track    string `json:"track"`
	Artist   string `json:"artist"`
	Album    string `json:"album,omitempty"`
	Duration int    `json:"duration"`
	Art      string `json:"art,omitempty"`
	Source   string `json:"source"`
}

func (s *server) writeTracks(w http.ResponseWriter, cs []engine.Candidate, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"tracks": toDTOs(cs)})
}

// toDTO converts a candidate to a client-safe payload, deriving an opaque stream
// id and never leaking server-side credentials (Subsonic auth URLs).
func toDTO(c engine.Candidate) (trackDTO, bool) {
	d := trackDTO{Track: c.Track, Artist: c.Artist, Album: c.Album, Duration: c.DurationSec, Source: c.Source}
	switch {
	case c.Source == "subsonic":
		if id := queryParam(c.StreamURL, "id"); id != "" {
			d.ID = "su:" + id
			if cov := queryParam(c.ArtURL, "id"); cov != "" {
				d.Art = "/api/art?id=su:" + cov
			}
		}
	case c.Source == "local":
		if c.StreamURL != "" {
			d.ID = "lo:" + base64.URLEncoding.EncodeToString([]byte(c.StreamURL))
			d.Source = "local"
			d.Art = "/api/art?id=" + d.ID // embedded cover, extracted on demand
		}
	case c.VideoID != "":
		d.ID = "yt:" + c.VideoID
		d.Art = c.ArtURL // public thumbnail URL
		d.Source = "youtube"
	}
	return d, d.ID != ""
}

// ── helpers ─────────────────────────────────────────────────────────────────

func splitID(id string) (kind, val string, ok bool) {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

func queryParam(rawurl, key string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// baseURL is the address advertised in the pairing QR.
func (s *server) baseURL() string {
	if s.cfg.URL != "" {
		return strings.TrimRight(s.cfg.URL, "/")
	}
	port := s.cfg.Addr
	if strings.HasPrefix(port, ":") {
		// keep as ":8787"
	} else if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i:]
	}
	return fmt.Sprintf("http://%s%s", lanIP(), port)
}

func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}
