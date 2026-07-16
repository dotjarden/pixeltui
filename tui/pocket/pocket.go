package pocket

import (
	"context"
	"image"
	"log"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/party"
	"github.com/dotjarden/pixeltui/tui/session"
)

// Button is a physical button. On the Pirate Audio the four sit at the screen
// corners — A top-left, B bottom-left, X top-right, Y bottom-right — and what
// each does is context-sensitive to the current screen (see handle).
type Button int

const (
	BtnA Button = iota // top-left
	BtnB               // bottom-left
	BtnX               // top-right
	BtnY               // bottom-right
)

// Display renders a frame to the panel and is closed on shutdown.
type Display interface {
	Push(img *image.RGBA) error
	Close() error
}

// Press is a button event: which button, and whether it was a long-press (hold).
type Press struct {
	Btn  Button
	Long bool
}

// Buttons delivers physical button presses until closed.
type Buttons interface {
	Events() <-chan Press
	Close() error
}

// Controller is the slice of session.Controller the pocket UI drives (an
// interface so the app loop is testable with a fake).
type Controller interface {
	Current() engine.Candidate
	Queue() []engine.Candidate
	Playing() bool
	TogglePause()
	Next()
	Prev()
	Play(engine.Candidate) error
	PlayAll([]engine.Candidate, int)
	Enqueue(...engine.Candidate)
	Shuffle()
	SetRepeat(session.RepeatMode)
	SetAutoplay(bool)
	SetVolume(int)
	Seek(float64)
	Stop()
	Events() <-chan session.Event
}

// CoverFunc decodes album art for a track (may be slow; called off the UI loop).
type CoverFunc func(engine.Candidate) image.Image

const listVisible = 7 // rows shown per list screen

// Debug, when set (via POCKET_DEBUG in cmdPocket), logs each button press so
// tap-vs-hold detection can be verified on-device over SSH / journalctl.
var Debug bool

// App is the on-device player: a navigation stack of screens (menus, lists, now
// playing, volume) driven by the four buttons, rendered to the panel.
type App struct {
	ctrl  Controller
	disp  Display
	btns  Buttons
	src   Sources
	cover CoverFunc

	stack    []*screen
	np       View // now-playing state, kept current from controller events
	npKey    string
	vol      int
	repeat   session.RepeatMode
	autoplay bool
	offline  bool
	host     func()      // injected: start hosting a party (cmdPocket wires serve + room)
	do       chan func() // mutations posted from goroutines (async loads, art, connectivity)

	// Party: while hosting, the room is the source of truth. The local player
	// follows it (applyRoom), the shared queue lives in the room, and the pocket's
	// own controls drive the room (playFrom/togglePause/skipNext). nil = standalone.
	party      *party.Room
	partyCh    <-chan party.RoomEvent // room events; nil until hosting (nil chan never selected)
	partyUnsub func()                 // unsubscribe from the room
	partyTrack string                 // key of the track the bridge last started locally

	frame int     // animation frame (marquee scroll, download spinner)
	dl    dlState // current on-screen download (spinner → result), if any
}

// download phases for the on-screen download overlay.
const (
	dlNone = iota
	dlRunning
	dlDone
	dlFail
)

// dlState is the download shown as an overlay: a spinner while running, then a
// green/red result that auto-dismisses.
type dlState struct {
	title string
	msg   string
	phase int
	ticks int // animation frames the result has been shown (auto-dismiss)
}

// NewApp builds the pocket app, opening on the home menu. cover/src may be nil.
func NewApp(ctrl Controller, disp Display, btns Buttons, src Sources, cover CoverFunc) *App {
	a := &App{
		ctrl: ctrl, disp: disp, btns: btns, src: src, cover: cover,
		vol: 100, autoplay: true,
		np: View{Status: "nothing playing"},
		do: make(chan func(), 8),
	}
	a.stack = []*screen{a.home()}
	return a
}

// Run renders and reacts until ctx is cancelled. The caller starts the
// Controller's own poll loop (session.Controller.Run) so progress events flow.
func (a *App) Run(ctx context.Context) error {
	defer a.disp.Close()
	defer a.btns.Close()
	a.draw()
	anim := time.NewTicker(100 * time.Millisecond)
	defer anim.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-a.btns.Events():
			if ok {
				a.handle(p)
				a.draw()
			}
		case ev := <-a.ctrl.Events():
			a.applyEvent(ev)
			a.draw()
		case ev, ok := <-a.partyCh: // nil until hosting → never selected
			if ok {
				a.applyRoom(ev)
				a.draw()
			}
		case fn := <-a.do:
			fn()
			a.draw()
		case <-anim.C:
			// Only repaint when something is actually animating (a marquee or a
			// download), so static screens don't churn the SPI bus.
			if a.needsAnim() {
				a.frame++
				a.tickDownload()
				a.draw()
			}
		}
	}
}

// needsAnim reports whether the current screen has motion to repaint: a download
// overlay, or a now-playing name too long to fit (so it scrolls).
func (a *App) needsAnim() bool {
	if a.dl.phase != dlNone {
		return true
	}
	if a.cur().kind == kindNowPlaying {
		return textWidth(a.np.Track.Track, 2) > Size-16 || textWidth(a.np.Track.Artist, 1) > Size-16
	}
	return false
}

// tickDownload auto-dismisses a finished download overlay after a few seconds.
func (a *App) tickDownload() {
	if a.dl.phase == dlDone || a.dl.phase == dlFail {
		a.dl.ticks++
		if a.dl.ticks > 30 { // ~3s at 100ms
			a.dl = dlState{}
		}
	}
}

// startDownload kicks off a download and shows the overlay (spinner → result).
// Surfacing the error matters: a missing yt-dlp on the device otherwise looks
// like the action silently did nothing.
func (a *App) startDownload(c engine.Candidate) {
	if a.src.Download == nil {
		a.dl = dlState{title: trackLabel(c), phase: dlFail, msg: "downloads unavailable"}
		return
	}
	a.dl = dlState{title: trackLabel(c), phase: dlRunning}
	go func() {
		err := a.src.Download(c)
		a.do <- func() {
			if err != nil {
				a.dl = dlState{title: trackLabel(c), phase: dlFail, msg: shortErr(err)}
			} else {
				a.dl = dlState{title: trackLabel(c), phase: dlDone, msg: "Saved"}
			}
		}
	}()
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func (a *App) cur() *screen   { return a.stack[len(a.stack)-1] }
func (a *App) push(s *screen) { a.stack = append(a.stack, s) }
func (a *App) pop() {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
}

// handle maps a button press to an action based on the current screen. On Now
// Playing a long-press (hold) of A/B adjusts volume, and hold-Y opens the action
// menu (like/download/queue) for the current track.
func (a *App) handle(p Press) {
	if Debug {
		log.Printf("pocket: press btn=%d long=%v screen=%d cursor=%d tracks=%d",
			p.Btn, p.Long, a.cur().kind, a.cur().cursor, len(a.cur().tracks))
	}
	switch a.cur().kind {
	case kindNowPlaying:
		if p.Long { // hold: A/B = volume, Y = actions for the current track
			switch p.Btn {
			case BtnA:
				a.setVol(a.vol + 5)
			case BtnB:
				a.setVol(a.vol - 5)
			case BtnY:
				if a.np.Track.Track != "" {
					a.push(a.trackActions(a.np.Track))
				}
			}
			return
		}
		switch p.Btn {
		case BtnA:
			a.togglePause()
		case BtnB:
			a.skipNext()
		case BtnX:
			a.ctrl.Prev() // local restart; "previous" isn't a shared-room concept
		case BtnY:
			a.pop() // back to the menu it came from
		}
	case kindVolume:
		switch p.Btn {
		case BtnA:
			a.setVol(a.vol + 5)
		case BtnB:
			a.setVol(a.vol - 5)
		default: // X / Y → back
			a.pop()
		}
	case kindParty:
		a.pop() // any button leaves the join-QR screen
	default: // kindList
		switch p.Btn {
		case BtnA:
			a.moveCursor(-1)
		case BtnB:
			a.moveCursor(1)
		case BtnX:
			a.pop()
		case BtnY:
			if p.Long {
				a.openItemActions() // hold Y on a track → its actions menu
			} else {
				a.selectItem()
			}
		}
	}
}

func (a *App) moveCursor(d int) {
	s := a.cur()
	if len(s.items) == 0 {
		return
	}
	s.cursor = (s.cursor + d + len(s.items)) % len(s.items)
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+listVisible {
		s.top = s.cursor - listVisible + 1
	}
}

func (a *App) selectItem() {
	s := a.cur()
	if s.cursor >= 0 && s.cursor < len(s.items) && s.items[s.cursor].run != nil {
		s.items[s.cursor].run(a)
	}
}

func (a *App) setVol(v int) {
	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}
	a.vol = v
	a.ctrl.SetVolume(v)
}

// applyEvent folds a controller event into the now-playing view, loading art
// asynchronously on a track change.
func (a *App) applyEvent(ev session.Event) {
	if ev.Kind == session.Stopped {
		// In a party, autoplay is off, so a natural track end surfaces as Stopped:
		// advance the room so every member moves together. Guard against advancing
		// an already-idle room (that would loop with applyRoom's Stop path).
		if a.party != nil && a.party.Snapshot().Playing {
			a.party.Next()
		}
		a.np.Track = engine.Candidate{}
		a.np.Pos, a.np.Dur, a.np.Paused = 0, 0, false
		a.np.Cover = nil
		a.np.Status = "nothing playing"
		a.npKey = ""
		return
	}
	a.np.Track = ev.Track
	a.np.Paused = ev.Paused
	a.np.Pos = ev.Pos
	if ev.Dur > 0 {
		a.np.Dur = ev.Dur
	}
	a.np.Status = ""

	key := ev.Track.Artist + "\x00" + ev.Track.Track
	if ev.Track.Track != "" && key != a.npKey {
		a.npKey = key
		a.np.Cover = nil
		if a.cover != nil {
			go func(c engine.Candidate, k string) {
				img := a.cover(c)
				if img == nil {
					return
				}
				a.do <- func() {
					if a.npKey == k {
						a.np.Cover = img
					}
				}
			}(ev.Track, key)
		}
	}
}

// npLine is the one-line "now playing" status shown at the bottom of list screens.
func (a *App) npLine() string {
	if a.np.Track.Track == "" {
		return ""
	}
	tag := "> "
	if a.np.Paused {
		tag = "|| "
	}
	return tag + trackLabel(a.np.Track)
}

// render draws the current screen, overlaying the connection icon and any
// in-progress download.
func (a *App) render() *image.RGBA {
	a.np.Frame = a.frame
	var img *image.RGBA
	switch a.cur().kind {
	case kindNowPlaying:
		img = renderNowPlaying(a.np, a.vol)
	case kindVolume:
		img = renderVolume(a.vol)
	case kindParty:
		img = renderQR(a.cur().qr, a.cur().cap)
	default:
		img = renderList(a.cur(), a.npLine())
	}
	if a.cur().kind != kindParty { // the QR screen stays clean for scanning
		drawConnection(img, !a.offline)
	}
	if a.dl.phase != dlNone {
		drawDownloadToast(img, a.dl.title, a.dl.msg, a.dl.phase, a.frame)
	}
	return img
}

func (a *App) draw() {
	a.disp.Push(a.render()) //nolint:errcheck // a dropped frame self-heals on the next redraw
}

// SetOffline updates the connectivity badge (driven by a connectivity.Monitor).
// Safe to call from another goroutine — it routes through the render loop.
func (a *App) SetOffline(b bool) {
	select {
	case a.do <- func() { a.offline = b }:
	default:
	}
}

// SetHost injects the party-hosting action invoked by the "Host Party" menu item.
func (a *App) SetHost(fn func()) { a.host = fn }

// SetParty enters or leaves party mode. In a party the ROOM is the source of
// truth: the local player follows the room (applyRoom), so the pocket stays in
// sync with phones; the shared queue lives in the room; and the pocket's own
// controls drive the room. Pass nil to leave. Call from a goroutine OTHER than
// the one running Run — it hands the change to the render loop and waits for it
// (so it must not be called from a button handler, which runs on the Run loop).
func (a *App) SetParty(room *party.Room) {
	var (
		ch    <-chan party.RoomEvent
		unsub func()
	)
	if room != nil {
		ch, unsub = room.Subscribe()
	}
	done := make(chan struct{})
	a.do <- func() {
		if a.partyUnsub != nil {
			a.partyUnsub()
		}
		a.party, a.partyCh, a.partyUnsub, a.partyTrack = room, ch, unsub, ""
		// The room advances the shared queue; local autoplay must be off in a party
		// so the controller doesn't also auto-advance and fight it. Restored on exit.
		a.ctrl.SetAutoplay(room == nil && a.autoplay)
		close(done)
	}
	<-done
}

// applyRoom drives the local player from the room (the source of truth in a
// party): play what the room plays, mirror pause/seek, and stop when it stops.
// Advancing is handled in applyEvent (a local track end → room.Next).
func (a *App) applyRoom(ev party.RoomEvent) {
	s := ev.Snapshot
	switch ev.Kind {
	case party.NowPlaying:
		if s.Track.Track == "" { // room idle / queue drained
			if a.ctrl.Playing() {
				a.ctrl.Stop()
			}
			a.partyTrack = ""
			return
		}
		if k := partyTrackKey(s.Track); k != a.partyTrack {
			a.partyTrack = k
			_ = a.ctrl.Play(s.Track)
		}
	case party.Transport:
		switch {
		case s.Paused != a.np.Paused: // someone toggled pause
			a.ctrl.TogglePause()
		case s.Position-a.np.Pos > 2 || a.np.Pos-s.Position > 2: // someone seeked
			a.ctrl.Seek(s.Position - a.np.Pos) // relative; ignore small drift
		}
	}
}

// partyTrackKey identifies a track for change detection in the bridge.
func partyTrackKey(c engine.Candidate) string {
	switch {
	case c.VideoID != "":
		return "yt:" + c.VideoID
	case c.StreamURL != "":
		return c.StreamURL
	default:
		return c.Artist + "\x00" + c.Track
	}
}

// ── playback routing: the room (in a party) vs the local controller (standalone) ──

// playFrom plays a list from index i: into the shared room when hosting (adds
// i..end to the shared queue), else locally (iPod-style "play from here").
func (a *App) playFrom(tracks []engine.Candidate, i int) {
	if a.party != nil {
		a.party.Enqueue(tracks[i:]...)
		return
	}
	a.ctrl.PlayAll(tracks, i)
}

// playOne plays a single track: enqueue to the room in a party, else play now.
func (a *App) playOne(c engine.Candidate) {
	if a.party != nil {
		a.party.Enqueue(c)
		return
	}
	_ = a.ctrl.Play(c)
}

// enqueueTrack adds one track to the shared room (party) or the local queue.
func (a *App) enqueueTrack(c engine.Candidate) {
	if a.party != nil {
		a.party.Enqueue(c)
		return
	}
	a.ctrl.Enqueue(c)
}

// togglePause flips pause for the whole room (party) or just locally.
func (a *App) togglePause() {
	if a.party != nil {
		if a.party.Snapshot().Paused {
			a.party.Resume()
		} else {
			a.party.Pause()
		}
		return
	}
	a.ctrl.TogglePause()
}

// skipNext advances the room (party) or the local queue.
func (a *App) skipNext() {
	if a.party != nil {
		a.party.Next()
		return
	}
	a.ctrl.Next()
}

// ShowParty pushes the join-QR screen (called once hosting is up). Safe from
// another goroutine — it routes through the render loop.
func (a *App) ShowParty(qr, caption string) {
	select {
	case a.do <- func() { a.push(&screen{kind: kindParty, title: "Party", qr: qr, cap: caption}) }:
	default:
	}
}
