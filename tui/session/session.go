// Package session is pixeltui's headless playback controller: it owns a play
// queue and drives a player stream — advance, autoplay-fill, repeat modes,
// pause/seek — and emits Events for a front-end to render. It has no dependency
// on the terminal UI, so the pocket hardware client and the party server embed
// it directly. (The TUI keeps its own model-driven orchestration for now; this
// mirrors that proven behaviour for the headless front-ends.)
//
// The player is behind the Engine/Handle interfaces so the controller is
// unit-testable without mpv; NewPlayerEngine wires the real player package.
package session

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/player"
)

// RepeatMode mirrors the TUI: off → all (loop the queue) → one (repeat track).
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// Handle controls one active stream. *player.Stream satisfies it.
type Handle interface {
	Pause()
	IsPaused() bool
	Seek(sec float64)
	SetVolume(v int)
	Position() float64
	Duration() float64
	Ended() bool
	Stop()
}

// Engine starts playback of a candidate and returns a control handle plus the
// (possibly enriched) candidate. Production: NewPlayerEngine. Tests: a fake.
type Engine interface {
	Start(c engine.Candidate, preloadedURL string) (Handle, engine.Candidate, error)
}

// Recommender fills the queue when it drains (autoplay). Satisfied by
// *engine.Recommender; nil disables autofill.
type Recommender interface {
	Recommend(artist, track string, n int) ([]engine.Candidate, error)
}

// EventKind classifies a controller Event.
type EventKind int

const (
	Started      EventKind = iota // a new track began (Track set)
	Progress                      // position tick (Pos/Dur set)
	StateChanged                  // paused/repeat/autoplay/queue changed
	Stopped                       // playback fully stopped (queue exhausted)
	Failed                        // a track failed to start (Err set)
)

// Event is emitted on the Events channel as playback progresses.
type Event struct {
	Kind   EventKind
	Track  engine.Candidate
	Pos    float64
	Dur    float64
	Paused bool
	Repeat RepeatMode
	Queue  []engine.Candidate // snapshot (len only meaningful for StateChanged)
	Err    error
}

// Controller is the headless playback orchestrator. Construct with New; call
// Run in a goroutine for autonomous advance, or drive pollOnce yourself.
type Controller struct {
	mu       sync.Mutex
	eng      Engine
	rec      Recommender
	h        Handle
	cur      engine.Candidate
	queue    []engine.Candidate
	repeat   RepeatMode
	autoplay bool
	filling  bool // an autofill fetch is in flight
	vol      int  // last-set volume 0..100 (surfaced via Volume)

	events chan Event
}

// New returns a controller. rec may be nil (autofill disabled). autoplay sets
// the initial autoplay state (the TUI defaults this on).
func New(eng Engine, rec Recommender, autoplay bool) *Controller {
	return &Controller{
		eng:      eng,
		rec:      rec,
		autoplay: autoplay,
		vol:      100,
		events:   make(chan Event, 32),
	}
}

// NewPlayerEngine returns the production Engine backed by the player package.
func NewPlayerEngine() Engine { return playerEngine{} }

type playerEngine struct{}

func (playerEngine) Start(c engine.Candidate, preloadedURL string) (Handle, engine.Candidate, error) {
	s, enriched, err := player.Start(c, preloadedURL)
	if err != nil {
		return nil, enriched, err
	}
	return s, enriched, nil
}

// Events is the stream of playback events. Progress events may be dropped under
// backpressure; Current/Queue always reflect live state.
func (c *Controller) Events() <-chan Event { return c.events }

func (c *Controller) emit(e Event) {
	select {
	case c.events <- e:
	default: // drop rather than block the caller / poll loop
	}
}

// ── public control surface ──────────────────────────────────────────────────

// Current returns the now-playing track (zero value if stopped).
func (c *Controller) Current() engine.Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// Queue returns a copy of the upcoming tracks.
func (c *Controller) Queue() []engine.Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]engine.Candidate(nil), c.queue...)
}

// Playing reports whether a stream is active and not ended.
func (c *Controller) Playing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.h != nil && !c.h.Ended()
}

// Play stops any current stream and starts cand immediately.
func (c *Controller) Play(cand engine.Candidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startLocked(cand)
}

// Enqueue appends tracks. If nothing is playing and autoplay is on, the first
// enqueued track starts immediately.
func (c *Controller) Enqueue(cands ...engine.Candidate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = append(c.queue, cands...)
	c.emit(Event{Kind: StateChanged, Queue: c.queueCopyLocked()})
	if c.h == nil && c.autoplay && len(c.queue) > 0 {
		c.advanceLocked()
	}
}

// Next skips to the next queued track (or autofills if empty). Honors nothing —
// it's an explicit user skip, so it always advances if possible.
func (c *Controller) Next() { c.mu.Lock(); defer c.mu.Unlock(); c.advanceLocked() }

// Prev restarts the current track (matches the TUI/OS "previous" → restart).
func (c *Controller) Prev() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h != nil {
		c.h.Seek(-1e9) // clamp to 0 (seek far-negative absolute-relative ≈ restart)
	}
}

// TogglePause flips pause on the active stream.
func (c *Controller) TogglePause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h != nil {
		c.h.Pause()
		c.emit(Event{Kind: StateChanged, Track: c.cur, Paused: c.h.IsPaused()})
	}
}

// Seek moves playback by delta seconds.
func (c *Controller) Seek(delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h != nil {
		c.h.Seek(delta)
	}
}

// SetVolume sets the output volume (clamped 0..100) and remembers it.
func (c *Controller) SetVolume(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}
	c.vol = v
	if c.h != nil {
		c.h.SetVolume(v)
	}
}

// Volume returns the last-set output volume (0..100).
func (c *Controller) Volume() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vol
}

// PlayAll replaces the queue with tracks and plays from index start (the rest
// become the upcoming queue) — iPod-style "play this list from here".
func (c *Controller) PlayAll(tracks []engine.Candidate, start int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(tracks) == 0 {
		return
	}
	if start < 0 || start >= len(tracks) {
		start = 0
	}
	c.queue = append([]engine.Candidate(nil), tracks[start+1:]...)
	c.emit(Event{Kind: StateChanged, Queue: c.queueCopyLocked()})
	c.startLocked(tracks[start])
}

// Shuffle randomizes the upcoming queue order (leaves the current track playing).
func (c *Controller) Shuffle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	rand.Shuffle(len(c.queue), func(i, j int) { c.queue[i], c.queue[j] = c.queue[j], c.queue[i] })
	c.emit(Event{Kind: StateChanged, Queue: c.queueCopyLocked()})
}

// SetRepeat sets the repeat mode.
func (c *Controller) SetRepeat(m RepeatMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repeat = m
	c.emit(Event{Kind: StateChanged, Track: c.cur, Repeat: m})
}

// SetAutoplay toggles autoplay (queue-empty autofill + auto-advance on end).
func (c *Controller) SetAutoplay(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.autoplay = on
	c.emit(Event{Kind: StateChanged, Track: c.cur})
}

// Stop halts playback and clears the current track (the queue is kept).
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
	c.emit(Event{Kind: Stopped})
}

// ── run loop ──────────────────────────────────────────────────────────────────

// Run drives the controller until ctx is cancelled: it polls the active stream,
// emits Progress, and advances when a track ends. Call once in a goroutine.
func (c *Controller) Run(ctx context.Context, tick time.Duration) {
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.stopLocked()
			c.mu.Unlock()
			return
		case <-t.C:
			c.pollOnce()
		}
	}
}

// pollOnce inspects the active stream once: end-of-track → advance per repeat
// mode; otherwise emit a Progress tick. Exported-for-test via Run; safe to call
// directly in tests to step the loop deterministically.
func (c *Controller) pollOnce() {
	c.mu.Lock()
	if c.h == nil {
		c.mu.Unlock()
		return
	}
	if c.h.Ended() {
		c.onEndedLocked()
		c.mu.Unlock()
		return
	}
	e := Event{Kind: Progress, Track: c.cur, Pos: c.h.Position(), Dur: c.h.Duration(), Paused: c.h.IsPaused()}
	c.mu.Unlock()
	c.emit(e)
}

// ── internal (lock held) ───────────────────────────────────────────────────────

func (c *Controller) queueCopyLocked() []engine.Candidate {
	return append([]engine.Candidate(nil), c.queue...)
}

func (c *Controller) stopLocked() {
	if c.h != nil {
		c.h.Stop()
		c.h = nil
	}
	c.cur = engine.Candidate{}
}

// startLocked stops the current stream and starts cand.
func (c *Controller) startLocked(cand engine.Candidate) error {
	if c.h != nil {
		c.h.Stop()
		c.h = nil
	}
	h, enriched, err := c.eng.Start(cand, "")
	if err != nil {
		c.cur = engine.Candidate{}
		c.emit(Event{Kind: Failed, Track: cand, Err: err})
		return err
	}
	c.h = h
	c.cur = enriched
	c.emit(Event{Kind: Started, Track: enriched})
	return nil
}

// onEndedLocked handles a track playing to its natural end (mirrors the TUI's
// pollMsg.ended path): repeat-one replays, repeat-all cycles the finished track
// to the back, otherwise advance.
func (c *Controller) onEndedLocked() {
	finished := c.cur
	if c.h != nil {
		c.h.Stop()
		c.h = nil
	}
	switch c.repeat {
	case RepeatOne:
		c.startLocked(finished)
		return
	case RepeatAll:
		if finished.Track != "" {
			c.queue = append(c.queue, finished)
		}
	}
	if !c.autoplay {
		c.cur = engine.Candidate{}
		c.emit(Event{Kind: Stopped})
		return
	}
	c.advanceLocked()
}

// advanceLocked pops the queue head and plays it; if the queue is empty it kicks
// off an autofill (async, so the lock isn't held during the network call),
// seeded by the just-finished track. Emits Stopped only when no fill is possible.
func (c *Controller) advanceLocked() {
	if len(c.queue) == 0 {
		seed := c.cur // capture BEFORE clearing — it's the autofill seed
		c.cur = engine.Candidate{}
		if c.h != nil {
			c.h.Stop()
			c.h = nil
		}
		if !c.startFillLocked(seed) {
			c.emit(Event{Kind: Stopped})
		}
		return
	}
	next := c.queue[0]
	c.queue = c.queue[1:]
	c.emit(Event{Kind: StateChanged, Queue: c.queueCopyLocked()})
	c.startLocked(next)
}

// startFillLocked launches a background recommend to refill an empty queue,
// seeded by the just-finished track. Reports whether a fill was started.
func (c *Controller) startFillLocked(seed engine.Candidate) bool {
	if c.rec == nil || c.filling || !c.autoplay || seed.Track == "" {
		return false
	}
	c.filling = true
	go c.fill(seed)
	return true
}

// fill fetches recommendations for seed and enqueues them (re-acquiring the
// lock). Runs in its own goroutine; never holds the lock during the network call.
func (c *Controller) fill(seed engine.Candidate) {
	results, err := c.rec.Recommend(seed.Artist, seed.Track, 12)
	c.mu.Lock()
	c.filling = false
	if err != nil || len(results) == 0 {
		c.mu.Unlock()
		return
	}
	c.queue = append(c.queue, results...)
	startNow := c.h == nil && c.autoplay
	c.emit(Event{Kind: StateChanged, Queue: c.queueCopyLocked()})
	if startNow {
		c.advanceLocked()
	}
	c.mu.Unlock()
}
