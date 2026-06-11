package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/library"
)

// Library write API: likes and playlist edits from clients land in the same
// on-disk library (~/.pixeltui/library M3U8 files) the TUI uses, so every
// client and the TUI see one library. library.Store reads files per call and
// serializes writes, so concurrent clients (and a running TUI) stay
// consistent — last write wins per playlist file, which is the right
// granularity for a personal library.

// trackPayload is the client→server track description (mirrors trackDTO).
type trackPayload struct {
	ID       string `json:"id"`
	Track    string `json:"track"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"`
	Art      string `json:"art"`
}

// candidate converts the payload back to the engine's native shape — the
// inverse of toDTO, so a track round-trips client→library→client unchanged.
func (p trackPayload) candidate() (engine.Candidate, error) {
	kind, val, ok := splitID(p.ID)
	if !ok {
		return engine.Candidate{}, fmt.Errorf("bad track id")
	}
	c := engine.Candidate{
		Track:       p.Track,
		Artist:      p.Artist,
		Album:       p.Album,
		DurationSec: p.Duration,
		ArtURL:      p.Art,
		Path:        "client",
	}
	switch kind {
	case "yt":
		c.VideoID = val
		c.Source = "youtube"
		// Server-relative art refs are useless in the shared library.
		if strings.HasPrefix(c.ArtURL, "/") {
			c.ArtURL = ""
		}
	case "lo":
		path, err := base64.URLEncoding.DecodeString(val)
		if err != nil {
			return engine.Candidate{}, fmt.Errorf("bad local id")
		}
		c.StreamURL = string(path)
		c.Source = "local"
		c.ArtURL = ""
	case "su":
		c.Source = "subsonic"
		c.ArtURL = ""
	default:
		return engine.Candidate{}, fmt.Errorf("unknown source %q", kind)
	}
	if c.Track == "" {
		return engine.Candidate{}, fmt.Errorf("missing track title")
	}
	return c, nil
}

// matchesID reports whether a stored candidate corresponds to a client id.
func matchesID(c engine.Candidate, id string) bool {
	d, ok := toDTO(c)
	return ok && d.ID == id
}

func (s *server) requireLibrary(w http.ResponseWriter) bool {
	if s.cfg.Library == nil {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return v, false
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return v, false
	}
	return v, true
}

// notifyLibrary tells connected clients (SSE) the library changed.
func (s *server) notifyLibrary(what string) {
	s.sse.broadcast(fmt.Sprintf("event: library\ndata: %q\n\n", what))
}

// handleLike likes/unlikes a track.
// POST {liked: bool, id, track, artist, album, duration, art}
func (s *server) handleLike(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Liked bool `json:"liked"`
		trackPayload
	}](w, r)
	if !ok {
		return
	}
	c, err := body.candidate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Liked {
		err = s.cfg.Library.Like(c)
	} else {
		err = s.cfg.Library.Unlike(c)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if body.Liked {
		s.cfg.Scrobbler.Love(c) // nil-safe; mirrors the like to Last.fm
	}
	s.notifyLibrary("liked")
	writeJSON(w, map[string]any{"ok": true, "liked": body.Liked})
}

// handlePlaylistCreate creates an empty playlist. POST {name}
func (s *server) handlePlaylistCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || strings.EqualFold(name, library.LikedName) {
		http.Error(w, "bad playlist name", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Library.SavePlaylist(name, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.notifyLibrary("playlists")
	writeJSON(w, map[string]any{"ok": true})
}

// handlePlaylistRename renames a playlist. POST {name, new_name}
func (s *server) handlePlaylistRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Name    string `json:"name"`
		NewName string `json:"new_name"`
	}](w, r)
	if !ok {
		return
	}
	newName := strings.TrimSpace(body.NewName)
	if newName == "" || strings.EqualFold(body.Name, library.LikedName) ||
		strings.EqualFold(newName, library.LikedName) {
		http.Error(w, "bad playlist name", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Library.RenamePlaylist(body.Name, newName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.notifyLibrary("playlists")
	writeJSON(w, map[string]any{"ok": true})
}

// handlePlaylistDelete deletes a playlist. POST {name}
func (s *server) handlePlaylistDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if strings.EqualFold(body.Name, library.LikedName) {
		http.Error(w, "cannot delete Liked Songs", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Library.DeletePlaylist(body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.notifyLibrary("playlists")
	writeJSON(w, map[string]any{"ok": true})
}

// handlePlaylistAdd appends a track to a playlist (created if missing,
// deduped by id). POST {name, id, track, artist, album, duration, art}
func (s *server) handlePlaylistAdd(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Name string `json:"name"`
		trackPayload
	}](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.EqualFold(body.Name, library.LikedName) {
		http.Error(w, "bad playlist name", http.StatusBadRequest)
		return
	}
	c, err := body.candidate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tracks, _ := s.cfg.Library.LoadPlaylist(body.Name)
	for _, t := range tracks {
		if matchesID(t, body.ID) {
			writeJSON(w, map[string]any{"ok": true}) // already there
			return
		}
	}
	if err := s.cfg.Library.SavePlaylist(body.Name, append(tracks, c)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.notifyLibrary("playlists")
	writeJSON(w, map[string]any{"ok": true})
}

// handlePlaylistRemove removes tracks from a playlist by client id.
// POST {name, ids: [..]}
func (s *server) handlePlaylistRemove(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibrary(w) {
		return
	}
	body, ok := decodeBody[struct {
		Name string   `json:"name"`
		IDs  []string `json:"ids"`
	}](w, r)
	if !ok {
		return
	}
	if strings.EqualFold(body.Name, library.LikedName) {
		http.Error(w, "use /api/like for Liked Songs", http.StatusBadRequest)
		return
	}
	tracks, err := s.cfg.Library.LoadPlaylist(body.Name)
	if err != nil {
		http.Error(w, "no such playlist", http.StatusNotFound)
		return
	}
	drop := make(map[string]struct{}, len(body.IDs))
	for _, id := range body.IDs {
		drop[id] = struct{}{}
	}
	kept := tracks[:0]
	for _, t := range tracks {
		if d, ok := toDTO(t); ok {
			if _, gone := drop[d.ID]; gone {
				continue
			}
		}
		kept = append(kept, t)
	}
	if err := s.cfg.Library.SavePlaylist(body.Name, kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.notifyLibrary("playlists")
	writeJSON(w, map[string]any{"ok": true})
}
