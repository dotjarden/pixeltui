package pocket

import (
	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/session"
)

type screenKind int

const (
	kindList screenKind = iota
	kindNowPlaying
	kindVolume
	kindParty
)

// item is one row of a list screen; run is invoked on Select (nil = inert).
type item struct {
	label string
	run   func(*App)
}

type screen struct {
	kind   screenKind
	title  string
	items  []item
	cursor int
	top    int
	tracks []engine.Candidate // track lists: aligned with items; enables per-track actions
	qr     string             // kindParty: QR payload to display
	cap    string             // kindParty: caption under the QR
}

func trackLabel(c engine.Candidate) string {
	if c.Artist != "" {
		return c.Track + " - " + c.Artist
	}
	return c.Track
}

// ── screen builders ────────────────────────────────────────────────────────────

func (a *App) home() *screen {
	return &screen{kind: kindList, title: "pixeltui", items: []item{
		{"Now Playing", (*App).goNowPlaying},
		{"Liked", func(a *App) { a.openTracks("Liked", a.src.Liked) }},
		{"Downloads", func(a *App) { a.openTracksAsync("Downloads", a.src.Downloaded) }},
		{"Playlists", (*App).openPlaylists},
		{"Queue", (*App).openQueue},
		{"Charts", func(a *App) { a.openTracksAsync("Charts", a.src.Charts) }},
		{"Recent", func(a *App) { a.openTracks("Recent", a.src.History) }},
		{"Host Party", func(a *App) {
			if a.host != nil {
				a.host()
			}
		}},
		{"Settings", func(a *App) { a.push(a.settings()) }},
	}}
}

func (a *App) goNowPlaying()             { a.push(a.nowPlayingScreen()) }
func (a *App) nowPlayingScreen() *screen { return &screen{kind: kindNowPlaying, title: "Now Playing"} }

// trackList builds a list where selecting a row plays from that index and queues
// the rest (iPod-style), then jumps to Now Playing.
func (a *App) trackList(title string, tracks []engine.Candidate) *screen {
	s := &screen{kind: kindList, title: title, tracks: tracks}
	for i := range tracks {
		i, t := i, tracks[i]
		s.items = append(s.items, item{trackLabel(t), func(a *App) {
			a.playFrom(tracks, i)
			a.push(a.nowPlayingScreen())
		}})
	}
	if len(s.items) == 0 {
		s.items = []item{{"(empty)", nil}}
	}
	return s
}

func (a *App) openTracks(title string, load func() []engine.Candidate) {
	var tracks []engine.Candidate
	if load != nil {
		tracks = load()
	}
	a.push(a.trackList(title, tracks))
}

// openTracksAsync shows a "loading" screen, fetches off the UI loop (network),
// then swaps in the results — for Charts and anything else that hits the wire.
func (a *App) openTracksAsync(title string, load func() []engine.Candidate) {
	loading := &screen{kind: kindList, title: title, items: []item{{"loading...", nil}}}
	a.push(loading)
	go func() {
		var tracks []engine.Candidate
		if load != nil {
			tracks = load()
		}
		a.do <- func() {
			if a.cur() == loading { // still on top → replace it
				a.stack[len(a.stack)-1] = a.trackList(title, tracks)
			}
		}
	}()
}

func (a *App) openPlaylists() {
	s := &screen{kind: kindList, title: "Playlists"}
	var names []string
	if a.src.Playlists != nil {
		names = a.src.Playlists()
	}
	for _, n := range names {
		n := n
		s.items = append(s.items, item{n, func(a *App) {
			a.openTracks(n, func() []engine.Candidate {
				if a.src.Playlist == nil {
					return nil
				}
				return a.src.Playlist(n)
			})
		}})
	}
	if len(s.items) == 0 {
		s.items = []item{{"(no playlists)", nil}}
	}
	a.push(s)
}

func (a *App) openQueue() {
	s := &screen{kind: kindList, title: "Queue"}
	cur, q := a.queueView()
	if cur.Track != "" {
		s.items = append(s.items, item{"> " + trackLabel(cur), (*App).goNowPlaying})
		s.tracks = append(s.tracks, cur)
	}
	party := a.party != nil
	for i := range q {
		i := i
		run := func(a *App) { a.playFrom(q, i); a.push(a.nowPlayingScreen()) }
		if party { // the shared queue is the room's — tapping views it, doesn't re-add
			run = (*App).goNowPlaying
		}
		s.items = append(s.items, item{trackLabel(q[i]), run})
		s.tracks = append(s.tracks, q[i])
	}
	if len(s.items) == 0 {
		s.items = []item{{"(queue empty)", nil}}
	}
	a.push(s)
}

// queueView returns the now-playing track and upcoming queue — from the shared
// room when in a party (so the Queue screen shows what everyone sees), else from
// the local controller.
func (a *App) queueView() (engine.Candidate, []engine.Candidate) {
	if a.party != nil {
		s := a.party.Snapshot()
		return s.Track, s.Queue
	}
	return a.ctrl.Current(), a.ctrl.Queue()
}

// ── per-track actions (hold Y on a track) ───────────────────────────────────────

// openItemActions opens the action menu for the highlighted track row (menu rows
// carry no track and are ignored).
func (a *App) openItemActions() {
	s := a.cur()
	if s.tracks == nil || s.cursor < 0 || s.cursor >= len(s.tracks) {
		return
	}
	a.push(a.trackActions(s.tracks[s.cursor]))
}

// trackActions is the per-track menu: play, like/unlike, download, add to queue.
func (a *App) trackActions(c engine.Candidate) *screen {
	liked := a.src.IsLiked != nil && a.src.IsLiked(c)
	likeLabel := "Like"
	if liked {
		likeLabel = "Unlike"
	}
	return &screen{kind: kindList, title: trackLabel(c), items: []item{
		{"Play", func(a *App) { a.playOne(c); a.push(a.nowPlayingScreen()) }},
		{likeLabel, func(a *App) {
			if liked {
				if a.src.Unlike != nil {
					a.src.Unlike(c)
				}
			} else if a.src.Like != nil {
				a.src.Like(c)
			}
			a.pop()
		}},
		{"Download", func(a *App) {
			a.startDownload(c) // shows a spinner + result overlay
			a.pop()
		}},
		{"Add to queue", func(a *App) { a.enqueueTrack(c); a.pop() }},
	}}
}

// ── settings ────────────────────────────────────────────────────────────────────

func (a *App) settings() *screen {
	return &screen{kind: kindList, title: "Settings", items: []item{
		{"Repeat: " + repeatName(a.repeat), (*App).cycleRepeat},
		{"Autoplay: " + onOff(a.autoplay), (*App).toggleAutoplay},
		{"Shuffle queue", func(a *App) { a.ctrl.Shuffle() }},
		{"Volume", func(a *App) { a.push(&screen{kind: kindVolume, title: "Volume"}) }},
	}}
}

func (a *App) cycleRepeat() {
	a.repeat = (a.repeat + 1) % 3
	a.ctrl.SetRepeat(a.repeat)
	a.refreshSettings()
}

func (a *App) toggleAutoplay() {
	a.autoplay = !a.autoplay
	a.ctrl.SetAutoplay(a.autoplay)
	a.refreshSettings()
}

// refreshSettings rebuilds the settings screen so its labels reflect new state,
// keeping the cursor where it was.
func (a *App) refreshSettings() {
	c := a.cur().cursor
	a.stack[len(a.stack)-1] = a.settings()
	a.cur().cursor = c
}

func repeatName(m session.RepeatMode) string {
	switch m {
	case session.RepeatAll:
		return "all"
	case session.RepeatOne:
		return "one"
	default:
		return "off"
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
