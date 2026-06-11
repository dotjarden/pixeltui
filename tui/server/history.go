package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Play history API: clients report plays here so phone listening lands in the
// same history.jsonl the TUI writes — feeding Recently Played, listening
// stats, recommendations, and scrobbling (Last.fm / ListenBrainz) exactly as
// if the track had played in the TUI.

// handleNowPlaying announces the client's current track to the configured
// scrobble services (fire-and-forget, nothing is recorded).
// POST {id, track, artist, album, duration, art}
func (s *server) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody[trackPayload](w, r)
	if !ok {
		return
	}
	c, err := body.candidate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.Scrobbler.NowPlaying(c) // nil-safe
	writeJSON(w, map[string]any{"ok": true})
}

// handlePlayed records one qualified play (the client enforces the
// 50%-or-4-minutes rule, same as the TUI): appends to the shared history and
// scrobbles to every configured service.
// POST {id, track, artist, album, duration, art, started_at?}
func (s *server) handlePlayed(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody[struct {
		trackPayload
		StartedAt int64 `json:"started_at"` // unix seconds; 0 = now
	}](w, r)
	if !ok {
		return
	}
	c, err := body.candidate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	at := time.Now()
	if body.StartedAt > 0 {
		at = time.Unix(body.StartedAt, 0)
	}
	if s.cfg.Library != nil {
		if err := s.cfg.Library.AddListen(c, at); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.cfg.Scrobbler.Scrobble(c, at) // nil-safe, async, spools on failure
	s.notifyLibrary("history")
	writeJSON(w, map[string]any{"ok": true})
}

// historyDTO is a track plus when it was played.
type historyDTO struct {
	trackDTO
	PlayedAt int64 `json:"played_at"`
}

// handleHistory returns recent listens, most-recent first — the shared
// Recently Played across the TUI and every client.
// Query: limit (default 50, max 500), unique=1 to collapse repeat plays.
func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Library == nil {
		writeJSON(w, map[string]any{"tracks": []historyDTO{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	unique := r.URL.Query().Get("unique") == "1"

	// Over-read when collapsing duplicates so heavy repeats still fill limit.
	read := limit
	if unique {
		read = limit * 4
	}
	listens, err := s.cfg.Library.Listens(read)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	seen := map[string]struct{}{}
	flat := make([]trackDTO, 0, limit)
	ats := make([]int64, 0, limit)
	for _, l := range listens {
		d, ok := toDTO(l.Candidate)
		if !ok {
			continue
		}
		if unique {
			key := strings.ToLower(d.Artist + "|" + d.Track)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
		flat = append(flat, d)
		ats = append(ats, l.At.Unix())
		if len(flat) == limit {
			break
		}
	}
	// History lines never store art — swap in the official covers.
	s.fillResolvedArt(flat)
	out := make([]historyDTO, len(flat))
	for i := range flat {
		out[i] = historyDTO{trackDTO: flat[i], PlayedAt: ats[i]}
	}
	writeJSON(w, map[string]any{"tracks": out})
}

// statsEntry is one ranked artist or track with its play count.
type statsEntry struct {
	Name     string `json:"name"`
	Artist   string `json:"artist,omitempty"` // set for tracks
	Plays    int    `json:"plays"`
	Art      string `json:"art,omitempty"`
	StreamID string `json:"id,omitempty"` // playable id for tracks, when known
}

// handleStats computes listening stats from the shared history — the same
// numbers the TUI's Listening Stats view shows.
// Query: days (0 = all time, e.g. 7 or 30).
func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Library == nil {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().AddDate(0, 0, -days)
	}
	listens, err := s.cfg.Library.Listens(0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type agg struct {
		entry statsEntry
		plays int
	}
	var (
		plays      int
		seconds    int
		artists    = map[string]*agg{}
		tracks     = map[string]*agg{}
		trackKeys  = map[string]struct{}{}
		artistKeys = map[string]struct{}{}
	)
	for _, l := range listens {
		if !cutoff.IsZero() && l.At.Before(cutoff) {
			continue
		}
		c := l.Candidate
		plays++
		seconds += c.DurationSec
		ak := strings.ToLower(c.Artist)
		tk := ak + "|" + strings.ToLower(c.Track)
		artistKeys[ak] = struct{}{}
		trackKeys[tk] = struct{}{}
		if a := artists[ak]; a != nil {
			a.plays++
		} else if c.Artist != "" {
			artists[ak] = &agg{entry: statsEntry{Name: c.Artist}, plays: 1}
		}
		if t := tracks[tk]; t != nil {
			t.plays++
		} else if c.Track != "" {
			e := statsEntry{Name: c.Track, Artist: c.Artist, Art: c.ArtURL}
			if d, ok := toDTO(c); ok {
				e.StreamID = d.ID
				if e.Art == "" {
					e.Art = d.Art
				}
			}
			// Prefer the official cover over a derived video thumbnail.
			if e.Art == "" || isVideoThumb(e.Art) {
				if art, ok := s.lookupArt(c.Artist, c.Track); ok && art != "" {
					e.Art = art
				} else if !ok {
					s.queueArtResolve(c.Artist, c.Track)
				}
			}
			tracks[tk] = &agg{entry: e, plays: 1}
		}
	}

	top := func(m map[string]*agg, n int) []statsEntry {
		all := make([]*agg, 0, len(m))
		for _, a := range m {
			all = append(all, a)
		}
		// Selection of the top n by play count (n is small).
		out := make([]statsEntry, 0, n)
		for len(out) < n && len(all) > 0 {
			best := 0
			for i, a := range all {
				if a.plays > all[best].plays {
					best = i
				}
			}
			all[best].entry.Plays = all[best].plays
			out = append(out, all[best].entry)
			all = append(all[:best], all[best+1:]...)
		}
		return out
	}

	writeJSON(w, map[string]any{
		"days":           days,
		"plays":          plays,
		"unique_tracks":  len(trackKeys),
		"unique_artists": len(artistKeys),
		"seconds":        seconds,
		"top_artists":    top(artists, 10),
		"top_tracks":     top(tracks, 10),
	})
}

// filterExcluded drops candidates whose artist is in the comma-separated
// exclude list (the client-side "mute artist" feature).
func filterExcluded(cands []engine.Candidate, exclude string) []engine.Candidate {
	if strings.TrimSpace(exclude) == "" {
		return cands
	}
	muted := map[string]struct{}{}
	for _, a := range strings.Split(exclude, ",") {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
			muted[a] = struct{}{}
		}
	}
	out := cands[:0]
	for _, c := range cands {
		if _, drop := muted[strings.ToLower(c.Artist)]; !drop {
			out = append(out, c)
		}
	}
	return out
}
