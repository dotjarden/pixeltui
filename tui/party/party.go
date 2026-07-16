// Package party is pixeltui's shared-session coordinator: a Room holds the
// authoritative shared queue and transport state for a group listening session.
// The room never plays or proxies audio — it only coordinates state, which the
// server fans out to members over SSE. The HOST device renders the audio (e.g. a
// pocket is the speaker); other members act as REMOTES that see what's playing,
// add to the queue, and drive transport. (A member could also play in lockstep
// with Position for SharePlay-style synced playback, but the model is
// host-plays, others-remote.)
package party

import (
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Member is a participant in a room.
type Member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Snapshot is the full room state clients need. Position is the authoritative
// playback offset (seconds into Track) at SnapshotUnixMs: the playing host stays
// aligned to it, and remotes use it to show progress.
type Snapshot struct {
	Code           string             `json:"code"`
	Rev            uint64             `json:"rev"`
	Track          engine.Candidate   `json:"track"`
	Playing        bool               `json:"playing"`
	Paused         bool               `json:"paused"`
	Position       float64            `json:"position"`
	Queue          []engine.Candidate `json:"queue"`
	Members        []Member           `json:"members"`
	SnapshotUnixMs int64              `json:"snapshot_unix_ms"`
}

// RoomEventKind classifies a room change for SSE fan-out.
type RoomEventKind int

const (
	MemberJoined RoomEventKind = iota
	MemberLeft
	QueueChanged
	NowPlaying
	Transport // play / pause / resume / seek
	RoomClosed
)

// RoomEvent is emitted on every state change; the server turns it into an SSE
// frame. It carries a fresh Snapshot so a client can fully resync from any event.
type RoomEvent struct {
	Kind     RoomEventKind
	Snapshot Snapshot
}

// Room is one shared listening session. Safe for concurrent use.
type Room struct {
	mu      sync.Mutex
	code    string
	members map[string]*Member
	order   []string // join order, for stable Members()
	queue   []engine.Candidate
	cur     engine.Candidate
	playing bool
	paused  bool
	baseOff float64   // seconds into cur at the anchor
	anchor  time.Time // wall-clock when baseOff was last set (while playing)
	rev     uint64
	now     func() time.Time
	subs    map[chan RoomEvent]struct{}
}

func newRoom(code string, now func() time.Time) *Room {
	if now == nil {
		now = time.Now
	}
	return &Room{
		code:    code,
		members: map[string]*Member{},
		now:     now,
		subs:    map[chan RoomEvent]struct{}{},
	}
}

// Code returns the room's join code.
func (r *Room) Code() string { return r.code }

// Subscribe registers a new listener for room changes and returns its channel
// plus an unsubscribe func. The server opens one per connected SSE client; each
// event carries a full Snapshot, so a dropped event (slow consumer) self-heals
// on the next one.
func (r *Room) Subscribe() (<-chan RoomEvent, func()) {
	ch := make(chan RoomEvent, 16)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// elapsedLocked is the authoritative position into the current track.
func (r *Room) elapsedLocked() float64 {
	switch {
	case !r.playing:
		return 0
	case r.paused:
		return r.baseOff
	default:
		return r.baseOff + r.now().Sub(r.anchor).Seconds()
	}
}

func (r *Room) snapshotLocked() Snapshot {
	members := make([]Member, 0, len(r.order))
	for _, id := range r.order {
		if m := r.members[id]; m != nil {
			members = append(members, *m)
		}
	}
	return Snapshot{
		Code:           r.code,
		Rev:            r.rev,
		Track:          r.cur,
		Playing:        r.playing,
		Paused:         r.paused,
		Position:       r.elapsedLocked(),
		Queue:          append([]engine.Candidate(nil), r.queue...),
		Members:        members,
		SnapshotUnixMs: r.now().UnixMilli(),
	}
}

// Snapshot returns the current full state.
func (r *Room) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *Room) bumpLocked(kind RoomEventKind) {
	r.rev++
	ev := RoomEvent{Kind: kind, Snapshot: r.snapshotLocked()}
	for ch := range r.subs {
		select {
		case ch <- ev:
		default: // drop for a slow consumer; the next event carries a fresh snapshot
		}
	}
}

// Join adds (or re-adds) a member and returns it.
func (r *Room) Join(id, name string) Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.members[id]; !ok {
		r.order = append(r.order, id)
	}
	m := &Member{ID: id, Name: name}
	r.members[id] = m
	r.bumpLocked(MemberJoined)
	return *m
}

// Leave removes a member; returns whether the room is now empty.
func (r *Room) Leave(id string) (empty bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.members[id]; ok {
		delete(r.members, id)
		for i, x := range r.order {
			if x == id {
				r.order = append(r.order[:i], r.order[i+1:]...)
				break
			}
		}
		r.bumpLocked(MemberLeft)
	}
	return len(r.members) == 0
}

// Members returns the participants in join order.
func (r *Room) Members() []Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked().Members
}

// Enqueue appends tracks to the shared queue. If nothing is playing, the first
// track starts immediately.
func (r *Room) Enqueue(cands ...engine.Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queue = append(r.queue, cands...)
	if !r.playing {
		r.startNextLocked()
	} else {
		r.bumpLocked(QueueChanged)
	}
}

// Next advances to the next queued track (or stops if the queue is empty).
func (r *Room) Next() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startNextLocked()
}

func (r *Room) startNextLocked() {
	if len(r.queue) == 0 {
		r.cur = engine.Candidate{}
		r.playing, r.paused, r.baseOff = false, false, 0
		r.bumpLocked(NowPlaying)
		return
	}
	r.cur = r.queue[0]
	r.queue = r.queue[1:]
	r.playing, r.paused, r.baseOff = true, false, 0
	r.anchor = r.now()
	r.bumpLocked(NowPlaying)
}

// Pause freezes the shared position.
func (r *Room) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.playing && !r.paused {
		r.baseOff = r.elapsedLocked()
		r.paused = true
		r.bumpLocked(Transport)
	}
}

// Resume continues from the frozen position.
func (r *Room) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.playing && r.paused {
		r.anchor = r.now()
		r.paused = false
		r.bumpLocked(Transport)
	}
}

// Seek sets the shared position (seconds into the current track).
func (r *Room) Seek(pos float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.playing {
		return
	}
	if pos < 0 {
		pos = 0
	}
	r.baseOff = pos
	r.anchor = r.now()
	r.bumpLocked(Transport)
}

// ── manager ───────────────────────────────────────────────────────────────────

// Manager holds active rooms keyed by join code.
type Manager struct {
	mu    sync.Mutex
	rooms map[string]*Room
	now   func() time.Time
}

// NewManager returns an empty room manager using the wall clock.
func NewManager() *Manager {
	return &Manager{rooms: map[string]*Room{}, now: time.Now}
}

// Create makes a room with the given (caller-supplied unique) join code.
func (mgr *Manager) Create(code string) *Room {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	r := newRoom(code, mgr.now)
	mgr.rooms[code] = r
	return r
}

// CreateRandom makes a room with a unique code drawn from gen (re-called until
// it yields an unused code), holding the manager lock so concurrent creates
// can't collide on a code.
func (mgr *Manager) CreateRandom(gen func() string) *Room {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	var code string
	for {
		code = gen()
		if _, exists := mgr.rooms[code]; !exists {
			break
		}
	}
	r := newRoom(code, mgr.now)
	mgr.rooms[code] = r
	return r
}

// Get returns the room for code, or nil if none.
func (mgr *Manager) Get(code string) *Room {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.rooms[code]
}

// Close removes a room, emitting a final RoomClosed event.
func (mgr *Manager) Close(code string) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if r := mgr.rooms[code]; r != nil {
		r.mu.Lock()
		r.bumpLocked(RoomClosed)
		r.mu.Unlock()
		delete(mgr.rooms, code)
	}
}
