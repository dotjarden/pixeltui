package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/party"
)

// Party endpoints back shared listening sessions. The room (tui/party) holds the
// authoritative shared queue + transport position; it never plays or proxies
// audio — each member's device plays its OWN resolved stream kept in lockstep
// with the snapshot (synced independent playback). Members are identified by the
// authenticated device id; every response is a full room Snapshot.

// partySnapshot is the client-facing room state: like party.Snapshot but with
// tracks as playable trackDTOs (carrying stream ids) instead of raw candidates,
// so any client can play them via /api/stream.
type partySnapshot struct {
	Code           string         `json:"code"`
	Rev            uint64         `json:"rev"`
	Track          *trackDTO      `json:"track,omitempty"`
	Playing        bool           `json:"playing"`
	Paused         bool           `json:"paused"`
	Position       float64        `json:"position"`
	Queue          []trackDTO     `json:"queue"`
	Members        []party.Member `json:"members"`
	SnapshotUnixMs int64          `json:"snapshot_unix_ms"`
}

func clientSnapshot(s party.Snapshot) partySnapshot {
	ps := partySnapshot{
		Code: s.Code, Rev: s.Rev, Playing: s.Playing, Paused: s.Paused,
		Position: s.Position, Members: s.Members, SnapshotUnixMs: s.SnapshotUnixMs,
		Queue: toDTOs(s.Queue),
	}
	if s.Track.Track != "" {
		if d, ok := toDTO(s.Track); ok {
			ps.Track = &d
		}
	}
	return ps
}

// roomOr resolves the room for code, writing a 4xx and returning nil if missing.
func (s *server) roomOr(w http.ResponseWriter, code string) *party.Room {
	if strings.TrimSpace(code) == "" {
		http.Error(w, "missing party code", http.StatusBadRequest)
		return nil
	}
	r := s.party.Get(code)
	if r == nil {
		http.Error(w, "no such party", http.StatusNotFound)
		return nil
	}
	return r
}

func memberName(name, id string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "guest-" + id
}

// partyReq is the common {code} request body for the transport endpoints.
type partyReq struct {
	Code string `json:"code"`
}

func (s *server) partyCode(r *http.Request) string {
	var body partyReq
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body.Code
}

// handlePartyCreate (POST {name?}) opens a new room and joins the creator.
func (s *server) handlePartyCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	room := s.party.CreateRandom(randCode)
	room.Join(deviceID(r), memberName(body.Name, deviceID(r)))
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

// handlePartyJoin (POST {code, name?}) joins an existing room.
func (s *server) handlePartyJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	room := s.roomOr(w, body.Code)
	if room == nil {
		return
	}
	room.Join(deviceID(r), memberName(body.Name, deviceID(r)))
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

// handlePartyLeave (POST {code}) leaves; closes the room when the last member exits.
func (s *server) handlePartyLeave(w http.ResponseWriter, r *http.Request) {
	code := s.partyCode(r)
	room := s.roomOr(w, code)
	if room == nil {
		return
	}
	if room.Leave(deviceID(r)) {
		s.party.Close(code)
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handlePartyState (GET ?code=) returns the current snapshot (polling fallback
// for clients between SSE frames).
func (s *server) handlePartyState(w http.ResponseWriter, r *http.Request) {
	room := s.roomOr(w, r.URL.Query().Get("code"))
	if room == nil {
		return
	}
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

// handlePartyEnqueue (POST {code, tracks:[{id,track,artist,...}]}) appends to the
// shared queue. Tracks use the same id-based payload as the rest of the API, so
// any client can add a song from the stream id it already has; unresolvable
// entries are skipped.
func (s *server) handlePartyEnqueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code   string         `json:"code"`
		Tracks []trackPayload `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	room := s.roomOr(w, body.Code)
	if room == nil {
		return
	}
	cands := make([]engine.Candidate, 0, len(body.Tracks))
	for _, t := range body.Tracks {
		if c, err := t.candidate(); err == nil {
			cands = append(cands, c)
		}
	}
	room.Enqueue(cands...)
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

func (s *server) handlePartyNext(w http.ResponseWriter, r *http.Request) {
	room := s.roomOr(w, s.partyCode(r))
	if room == nil {
		return
	}
	room.Next()
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

func (s *server) handlePartyPause(w http.ResponseWriter, r *http.Request) {
	room := s.roomOr(w, s.partyCode(r))
	if room == nil {
		return
	}
	room.Pause()
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

func (s *server) handlePartyResume(w http.ResponseWriter, r *http.Request) {
	room := s.roomOr(w, s.partyCode(r))
	if room == nil {
		return
	}
	room.Resume()
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

// handlePartySeek (POST {code, pos}) sets the shared position (seconds).
func (s *server) handlePartySeek(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string  `json:"code"`
		Pos  float64 `json:"pos"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	room := s.roomOr(w, body.Code)
	if room == nil {
		return
	}
	room.Seek(body.Pos)
	writeJSON(w, clientSnapshot(room.Snapshot()))
}

// handlePartyEvents (GET ?code=) streams room snapshots as SSE (event: party).
// Each device opens one and re-syncs its own local stream to every snapshot.
func (s *server) handlePartyEvents(w http.ResponseWriter, r *http.Request) {
	room := s.roomOr(w, r.URL.Query().Get("code"))
	if room == nil {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := room.Subscribe()
	defer unsub()
	writePartyFrame(w, room.Snapshot()) // seed with the current full state
	fl.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			writePartyFrame(w, ev.Snapshot)
			fl.Flush()
			if ev.Kind == party.RoomClosed {
				return
			}
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writePartyFrame(w http.ResponseWriter, snap party.Snapshot) {
	b, err := json.Marshal(clientSnapshot(snap))
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: party\ndata: %s\n\n", b)
}
