package tui

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/sahilm/fuzzy"

	"github.com/dotjarden/pixeltui/tui/config"
	"github.com/dotjarden/pixeltui/tui/download"
	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/library"
	"github.com/dotjarden/pixeltui/tui/local"
	"github.com/dotjarden/pixeltui/tui/lyrics"
	"github.com/dotjarden/pixeltui/tui/subsonic"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// repeatMode cycles: off → all (loop the queue) → one (repeat track).
type repeatMode uint8

const (
	repeatOff repeatMode = iota
	repeatAll
	repeatOne
)

func (r repeatMode) String() string {
	switch r {
	case repeatAll:
		return "repeat all"
	case repeatOne:
		return "repeat one"
	default:
		return "repeat off"
	}
}

// ── palette ───────────────────────────────────────────────────────────────────

// theme defines the two accent colors that give pixeltui its personality; the
// semantic colors (text/dim/green/yellow/red/border) stay constant across themes.
type themeDef struct {
	accent, accent2 lipgloss.AdaptiveColor // headers/selection · secondary (now-playing, keys)
	grad1, grad2    string                 // seek-bar gradient endpoints (hex)
}

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// themes is the preset registry. Pick one with `theme` in config (or PIXELTUI_THEME).
var themes = map[string]themeDef{
	"default": {ac("57", "141"), ac("62", "183"), "#7D56F4", "#F25D94"},   // purple → pink
	"ocean":   {ac("31", "39"), ac("37", "45"), "#00B4D8", "#90E0EF"},     // teal/cyan
	"matrix":  {ac("28", "46"), ac("34", "84"), "#00FF66", "#9EFFC2"},     // green
	"amber":   {ac("130", "214"), ac("136", "222"), "#FF8C00", "#FFD166"}, // amber/gold
	"rose":    {ac("125", "211"), ac("162", "218"), "#FF2D95", "#FF8FB1"}, // hot pink
	"mono":    {ac("240", "250"), ac("244", "245"), "#B0B0B0", "#E0E0E0"}, // grayscale
}

// Themeable accent colors (reassigned by applyTheme).
var (
	cAccent  = ac("57", "141")
	cAccent2 = ac("62", "183")
	cBorderA = ac("57", "141")
	gradA    = "#7D56F4"
	gradB    = "#F25D94"
)

// Fixed semantic colors.
var (
	cText   = ac("236", "252")
	cDim    = ac("245", "243")
	cFaint  = ac("250", "239")
	cGreen  = ac("28", "84")
	cYellow = ac("136", "221")
	cRed    = ac("160", "203")
	cBorder = ac("250", "238")
)

// Accent-dependent styles (rebuilt by applyTheme); the rest are constant.
var (
	stTitle     lipgloss.Style
	stNowTitle  lipgloss.Style
	stSelBar    lipgloss.Style
	stHelpKey   lipgloss.Style
	stPaneFocus lipgloss.Style

	stDim     = lipgloss.NewStyle().Foreground(cDim)
	stGreen   = lipgloss.NewStyle().Foreground(cGreen)
	stGreenB  = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	stYellow  = lipgloss.NewStyle().Foreground(cYellow)
	stRed     = lipgloss.NewStyle().Foreground(cRed)
	stText    = lipgloss.NewStyle().Foreground(cText)
	stArtist  = lipgloss.NewStyle().Foreground(cDim)
	stSelText = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Bold(true)
	stPane    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder)
)

func init() { applyTheme("default") }

// applyTheme switches the accent palette and rebuilds accent-dependent styles.
// Unknown names fall back to "default".
func applyTheme(name string) {
	t, ok := themes[name]
	if !ok {
		t = themes["default"]
	}
	cAccent, cAccent2, cBorderA = t.accent, t.accent2, t.accent
	gradA, gradB = t.grad1, t.grad2
	stTitle = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stNowTitle = lipgloss.NewStyle().Foreground(cAccent2).Bold(true)
	stSelBar = lipgloss.NewStyle().Foreground(cAccent)
	stHelpKey = lipgloss.NewStyle().Foreground(cAccent2).Bold(true)
	stPaneFocus = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorderA)
}

// ThemeNames returns the available theme names (sorted), for setup/help.
func ThemeNames() []string {
	out := make([]string, 0, len(themes))
	for n := range themes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ── messages ──────────────────────────────────────────────────────────────────

type (
	playOKMsg struct {
		pb  *playback
		c   engine.Candidate
		gen int
	}
	playErrMsg struct{ err error }

	pollMsg struct {
		pos, dur float64
		paused   bool
		vol      int
		ended    bool
		gen      int
	}

	preloadMsg struct {
		key string
		c   engine.Candidate
		url string
		err error
	}
	mediaMsg struct { // OS / hardware transport command from mpv
		cmd    mediaCmd
		gen    int
		closed bool
	}
	discoverRecsMsg struct{ recs []engine.Candidate } // engine recs for For You
	forYouChartMsg  struct {                          // current-chart picks for For You
		tracks []engine.Candidate
		label  string
	}
	preloadArmMsg struct{ key string } // debounced "preload the resting selection"
	autoQueueMsg  struct{ results []engine.Candidate }
	searchMsg     struct {
		results []engine.Candidate
		err     error
		header  string // optional: relabel the Discover title (artist/album drills)
	}
	albumsMsg struct { // album entities from a "!album" search (chooser)
		albums []ytm.Album
		query  string
	}
	artMsg    []string
	lyricsMsg struct {
		key    string // trackKey the lyrics were fetched for
		text   string
		synced []lyrics.Line // timestamped lines (LRCLIB); empty → plain text
		err    error
	}
	lyricsTickMsg      struct{}                             // drives smooth synced-lyric scrolling while open
	browsePlaylistsMsg []browseEntry                        // Subsonic playlists fetched for the browse menu
	localRefreshMsg    struct{ results []engine.Candidate } // background local rescan finished
	downloadDoneMsg    struct {
		track string
		err   error
	}
)

// lyricsResult is a cached lyrics fetch (synced and/or plain).
type lyricsResult struct {
	synced []lyrics.Line
	text   string
}

// browseEntry is one row in the unified browse menu.
type browseEntry struct {
	label string
	kind  string // "liked" | "local" | "substarred" | "subplaylist"
	id    string // playlist id for "subplaylist"
}

// actionEntry is one row in the per-track actions menu.
type actionEntry struct {
	label string
	kind  string // "play" | "like" | "queue" | "next" | "playlist" | "download" | "station" | "dislike"
}

// ── list item + delegate ──────────────────────────────────────────────────────

type trackItem struct{ c engine.Candidate }

func (t trackItem) FilterValue() string { return t.c.Track + " " + t.c.Artist }

// sectionItem is a non-selectable header row used to label groups in the
// sectioned "For You" landing (navigation skips it).
type sectionItem struct{ label string }

func (s sectionItem) FilterValue() string { return "" }

// albumItem is a selectable album entity row (from a "!album" search); Enter
// drills into the album's tracks.
type albumItem struct{ a ytm.Album }

func (a albumItem) FilterValue() string { return a.a.Title + " " + a.a.Artist }

// renderState is shared (by pointer) with both delegates so row rendering can
// reflect focus, the now-playing track, and preload status without re-creating
// the delegate every frame.
type renderState struct {
	focusQueue bool
	nowKey     string
	paused     bool
	preloaded  map[string]string
	likedKeys  map[string]bool // trackKey → liked (for the ♥ marker; fast)
	dev        bool
	hideSel    bool // suppress the Discover selection bar (search box is active)
}

// Row rendering lives in tracklist.go (the custom smooth-scrolling list).

// ── keymap ────────────────────────────────────────────────────────────────────

type keyMap struct {
	Up, Down        key.Binding
	Play, Pause     key.Binding
	SeekL, SeekR    key.Binding
	Next, Tab       key.Binding
	Shuffle, Repeat key.Binding
	Sleep, Lyrics   key.Binding
	Auto, Search    key.Binding
	VolU, VolD      key.Binding
	// Track verbs — lowercase = highlighted, Shift = now-playing.
	Like, LikeNow       key.Binding
	AddQ, PlayNext      key.Binding
	QueueAll            key.Binding // e — queue the whole current list
	Playlist, PlaylistN key.Binding
	Download, DownloadN key.Binding
	Dislike, DislikeNow key.Binding
	// Queue (contextual) + menus + station.
	Remove, Clr         key.Binding
	Browse              key.Binding
	Filter              key.Binding
	Actions             key.Binding
	Station, StationNow key.Binding // o = station from selection · O = from now-playing
	Settings            key.Binding // , = settings overlay
	Help                key.Binding
	Quit, Esc           key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Play:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "play")),
		Pause:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "pause")),
		SeekL:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "-10s")),
		SeekR:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "+10s")),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next")),
		Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pane")),
		Shuffle: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shuffle")),
		Repeat:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repeat")),
		Sleep:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "sleep")),
		Lyrics:  key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "lyrics")),
		Auto:    key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "autoplay")),
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "find")),
		VolU:    key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "vol+")),
		VolD:    key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "vol-")),

		Like:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "like")),
		LikeNow:    key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "like playing")),
		AddQ:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add to queue")),
		PlayNext:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "play next")),
		QueueAll:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "queue all")),
		Playlist:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "add to playlist")),
		PlaylistN:  key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "playlist playing")),
		Download:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download")),
		DownloadN:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "download playing")),
		Dislike:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "mute artist")),
		DislikeNow: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "dislike playing")),

		Remove:     key.NewBinding(key.WithKeys("delete", "backspace"), key.WithHelp("del", "remove")),
		Clr:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
		Browse:     key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "browse")),
		Filter:     key.NewBinding(key.WithKeys("'"), key.WithHelp("'", "filter")),
		Actions:    key.NewBinding(key.WithKeys("."), key.WithHelp(".", "actions")),
		Station:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "station")),
		StationNow: key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "station playing")),
		Settings:   key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "all keys")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Esc:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Play, k.Pause, k.Search, k.Browse, k.Actions, k.Help, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Play, k.Pause},
		{k.SeekL, k.SeekR, k.Next, k.VolU, k.VolD},
		{k.Like, k.AddQ, k.Playlist, k.Download, k.Actions},
		{k.Remove, k.Shuffle, k.Repeat, k.Clr},
		{k.Search, k.Browse, k.Lyrics, k.Auto, k.Sleep},
		{k.Tab, k.Help, k.Esc, k.Quit},
	}
}

// contextHelp renders a focus-aware single-line footer: queue-pane keys when the
// Up Next list is focused, discover-pane keys otherwise.
type contextHelp struct {
	k          keyMap
	queueFocus bool
}

func (c contextHelp) ShortHelp() []key.Binding {
	k := c.k
	base := []key.Binding{k.Play, k.Pause}
	if c.queueFocus {
		return append(base, k.Next, k.Remove, k.Shuffle, k.Repeat, k.Clr, k.Actions, k.Tab, k.Help, k.Quit)
	}
	return append(base, k.Like, k.AddQ, k.QueueAll, k.Playlist, k.Actions, k.Search, k.Filter, k.Browse, k.Tab, k.Help, k.Quit)
}
func (c contextHelp) FullHelp() [][]key.Binding { return c.k.FullHelp() }

// ── config ────────────────────────────────────────────────────────────────────

type Config struct {
	Header        string
	SeedTags      []string
	Results       []engine.Candidate
	Dev           bool
	Rec           *engine.Recommender
	URLCache      urlCache         // disk cache for resolved stream URLs (optional)
	Library       *library.Store   // likes/playlists/history/resume (optional)
	Subsonic      *subsonic.Client // 2nd source: a Subsonic/Navidrome server (optional)
	LocalDirs     []string         // 3rd source: local audio folders (optional)
	DownloadDir   string           // where downloaded tracks are saved (optional)
	Theme         string           // accent theme name (default/ocean/matrix/amber/rose/mono)
	DataDir       string           // ~/.pixeltui (for caches like the local index)
	ChartsGlobal  bool             // show the worldwide Top chart
	ChartsCountry string           // country (name or 2-letter code) chart ("" = off)
	Explore       int              // discovery level 0..10 (default 5)
}

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	results tracklist
	queue   tracklist
	search  textinput.Model
	prog    progress.Model
	spin    spinner.Model
	help    help.Model
	keys    keyMap
	st      *renderState

	searching bool
	loading   bool
	autoQueue bool
	aqPending bool

	inflight map[string]bool // tracks in-progress preloads (by trackKey)

	now      *playback
	nowC     engine.Candidate
	position float64
	duration float64
	paused   bool
	volume   int
	hasMPV   bool // whether mpv is installed (enables pause/seek/volume)
	seeking  bool // brief visual flag right after a manual seek
	gen      int  // playback generation; bumps on every deliberate (re)play

	repeat  repeatMode
	sleepAt time.Time // zero = no sleep timer; else stop playback at this time

	// overlays
	showLyrics     bool
	lyricsVP       viewport.Model
	lyricsBusy     bool
	lyricsTrack    string                  // header shown above the lyrics
	lyricsSynced   []lyrics.Line           // timestamped lines (karaoke view); nil → plain
	lyricsCache    map[string]lyricsResult // trackKey → fetched lyrics (prefetch/reopen)
	posAt          time.Time               // wall-clock when m.position was last set (interpolation)
	showHelp       bool                    // full shortcuts page
	showStats      bool                    // listening stats page
	stats          statResult              // computed when the stats page opens
	showSettings   bool                    // in-app settings overlay
	settingsCursor int                     // selected settings row
	themeName      string                  // current accent theme (live-editable)
	explore        int                     // discovery level 0..10 (live-editable)

	// browse menu (unified source picker)
	showBrowse   bool
	browseItems  []browseEntry // currently displayed (filtered) entries
	browseAll    []browseEntry // full unfiltered set (source of truth)
	browseCursor int
	browseFilter string // live fuzzy filter query ("/" inside browse)
	browseSearch bool   // typing into the browse filter

	// actions menu (per-track verb list, opened with '.')
	showActions   bool
	actionsItems  []actionEntry
	actionsCursor int
	actionsTrack  engine.Candidate

	art []string

	status string
	isErr  bool

	header       string
	seedTags     []string
	dev          bool
	rec          *engine.Recommender
	lib          *library.Store
	sub          *subsonic.Client
	localDirs    []string
	downloadDir  string
	dataDir      string
	searchSource string // "" = YouTube, "subsonic", "local" — what / searches

	// Text-prompt mode (textinput is shared): "" = plain search, else a
	// playlist op capturing a name. promptTrack/promptOld carry context.
	promptMode  string // "savequeue" | "addtrack" | "rename" | "filter"
	promptTrack engine.Candidate
	promptOld   string
	filterBack  []engine.Candidate // unfiltered discover items, while filtering
	localAll    []engine.Candidate // cached local index for live fuzzy "/" on the Local tab

	// Unified "/" (search+filter) and "'" (filter-only) state.
	staticList       bool // current view is a fixed list (Liked/playlist) → "/" only filters
	searchFilterOnly bool // this session only filters (opened via ') — never fetches online
	searchOnInput    bool // search box is the active element (no result row selected yet)

	// One-level back stack (album chooser → tracks; esc restores the chooser).
	backItems  []list.Item
	backHeader string

	// "For You" discover landing (sectioned: local + engine recs + genre chart).
	forYouSeed      engine.Candidate
	forYouRecsTried bool
	fyLocal         []engine.Candidate // your listening (top played + recent + liked)
	fyRecs          []engine.Candidate // engine recommendations (async)
	fyChart         []engine.Candidate // current-chart picks (async)
	fyChartLabel    string             // label for the chart section ("Top Charts" / "<Country> Top")

	// Charts (current global/country from YouTube Music; no API key needed).
	charts        chartFetcher // chart source (always set)
	chartsGlobal  bool         // show the worldwide Top chart
	chartsCountry string       // country (name or 2-letter code) chart ("" = off)

	width, height int
}

func trackKey(c engine.Candidate) string {
	return strings.ToLower(c.Track) + "|" + strings.ToLower(c.Artist)
}

func toItems(cs []engine.Candidate) []list.Item {
	items := make([]list.Item, len(cs))
	for i, c := range cs {
		items[i] = trackItem{c}
	}
	return items
}

// ── construction ──────────────────────────────────────────────────────────────

func newModel(cfg Config) model {
	st := &renderState{preloaded: map[string]string{}, likedKeys: map[string]bool{}, dev: cfg.Dev}
	if cfg.Library != nil {
		for _, c := range cfg.Library.Liked() {
			st.likedKeys[trackKey(c)] = true
		}
	}

	mkList := func(items []list.Item, isQueue bool) tracklist {
		return newTrackList(items, st, isQueue)
	}

	ti := textinput.New()
	ti.Placeholder = "search songs, artists…"
	ti.Prompt = "/ "
	ti.CharLimit = 80

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cAccent)

	m := model{
		results:     mkList(toItems(cfg.Results), false),
		queue:       mkList(nil, true),
		search:      ti,
		prog:        progress.New(progress.WithGradient(gradA, gradB), progress.WithoutPercentage()),
		spin:        sp,
		help:        help.New(),
		keys:        newKeyMap(),
		st:          st,
		inflight:    map[string]bool{},
		lyricsCache: map[string]lyricsResult{},
		autoQueue:   true,
		volume:      -1,
		hasMPV:      mpvAvailable(),
		lyricsVP:    viewport.New(0, 0),
		header:      cfg.Header,
		seedTags:    cfg.SeedTags,
		dev:         cfg.Dev,
		rec:         cfg.Rec,
		lib:         cfg.Library,
		sub:         cfg.Subsonic,
		localDirs:   cfg.LocalDirs,
		downloadDir: cfg.DownloadDir,
		dataDir:     cfg.DataDir,

		charts:        ytmCharts{}, // current charts via YouTube Music (no key)
		chartsGlobal:  cfg.ChartsGlobal,
		chartsCountry: cfg.ChartsCountry,
		themeName:     cfg.Theme,
		explore:       cfg.Explore,
	}
	if m.themeName == "" {
		m.themeName = "default"
	}

	// Restore the previous session's queue (Up Next) so it survives restarts.
	if m.lib != nil {
		if sess, ok := m.lib.LoadSession(); ok && len(sess.Queue) > 0 {
			m.queue.SetItems(toItems(sess.Queue))
		}
	}

	// With no seed results, show a "For You" discover landing built from what we
	// already have (history + likes). If there's nothing yet, fall back to
	// search-first so `pixeltui` with no args lands straight in search.
	if len(cfg.Results) == 0 {
		if forYou := m.buildForYou(); len(forYou) > 0 {
			m.fyLocal = forYou
			m.header = "FOR YOU"
			m.forYouSeed = forYou[0] // top track seeds engine recs (fetched async)
			m.rebuildForYou()
		} else {
			m.searching = true
			m.search.Focus()
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	if m.searching {
		cmds = append(cmds, textinput.Blink)
	}
	// Warm the first track so the very first play is fast (skip section headers).
	for _, it := range m.results.Items() {
		if ti, ok := it.(trackItem); ok {
			cmds = append(cmds, cmdPreload(ti.c))
			break
		}
	}
	// Async: enrich the For You landing with engine recommendations (best-effort;
	// no-op without a recommender / Last.fm key, never blocks the UI).
	if m.header == "FOR YOU" && m.rec != nil &&
		(m.forYouSeed.Artist != "" || m.forYouSeed.Track != "") {
		cmds = append(cmds, cmdDiscoverRecs(m.rec, m.forYouSeed.Artist, m.forYouSeed.Track))
	}
	// Async: load the current chart (country if set, else global) for its For You
	// section. Best-effort; never blocks.
	if m.header == "FOR YOU" && (m.chartsGlobal || m.chartsCountry != "") {
		cmds = append(cmds, cmdForYouChart(m.charts, m.chartsCountry, m.chartsGlobal))
	}
	return tea.Batch(cmds...)
}

// ── update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		// Drives the harmonica spring animation of the seek bar.
		pm, cmd := m.prog.Update(msg)
		m.prog = pm.(progress.Model)
		return m, cmd

	case pollMsg:
		// Ignore polls from a track the user has already replaced/skipped — this
		// is what prevents a deliberate play from being hijacked by the old
		// track's "ended" event advancing the queue.
		if msg.gen != m.gen {
			return m, nil
		}
		if msg.ended {
			ended := m.nowC
			m.now.stop()
			m.now = nil
			m.st.nowKey = ""
			m.position, m.duration, m.paused = 0, 0, false
			m.art = nil
			switch m.repeat {
			case repeatOne:
				return m, m.replay(ended) // loop the same track
			case repeatAll:
				m.appendQueue([]engine.Candidate{ended}) // cycle to the back
			}
			return m, m.advance()
		}
		// Sleep timer: stop playback once the deadline passes.
		if !m.sleepAt.IsZero() && time.Now().After(m.sleepAt) {
			m.sleepAt = time.Time{}
			m.autoQueue = false
			m.now.stop()
			m.now = nil
			m.st.nowKey = ""
			m.position, m.duration = 0, 0
			m.art = nil
			m.status = "Sleep timer — stopped"
			m.isErr = false
			return m, nil
		}
		m.position = msg.pos
		m.posAt = time.Now() // anchor for between-poll interpolation
		if msg.dur > 0 {
			m.duration = msg.dur
		}
		m.paused = msg.paused
		m.st.paused = msg.paused
		m.seeking = false
		if msg.vol >= 0 {
			m.volume = msg.vol
		}
		// Glide the Charm progress bar toward the true position.
		return m, tea.Batch(cmdPoll(m.now, m.gen), m.prog.SetPercent(m.ratio()))

	case playOKMsg:
		// A play the user already superseded (pressed Enter again, skipped, …):
		// discard it so we don't leak the process or clobber the current track.
		if msg.gen != m.gen {
			msg.pb.stop()
			return m, nil
		}
		m.loading = false
		m.now = msg.pb
		m.nowC = msg.c
		m.st.nowKey = trackKey(msg.c)
		m.st.paused = false
		m.position = 0
		m.posAt = time.Now()
		m.duration = float64(msg.c.DurationSec)
		m.paused = false
		m.art = nil
		m.status = ""
		m.isErr = false
		m.lyricsSynced = nil // belongs to the previous track

		cmds := []tea.Cmd{cmdPoll(msg.pb, msg.gen)}
		if c := waitMedia(msg.pb, msg.gen); c != nil {
			cmds = append(cmds, c) // listen for OS next/prev/play-pause
		}
		if msg.c.ArtURL != "" && m.artWidth() > 0 {
			cmds = append(cmds, cmdArt(msg.c.ArtURL, artCols, artRows))
		}
		// Warm the next couple of queued tracks so auto-advance is gapless.
		cmds = append(cmds, m.preloadQueue(2))
		// Prefetch lyrics in the background so pressing `y` is instant.
		if _, ok := m.lyricsCache[m.st.nowKey]; !ok {
			cmds = append(cmds, cmdLyrics(msg.c, m.st.nowKey))
		}
		// If the lyrics overlay is open, refetch for the new track.
		if m.showLyrics {
			m.lyricsBusy = true
			m.lyricsTrack = msg.c.Track + " — " + msg.c.Artist
			m.lyricsVP.SetContent("")
		}
		go notifyNowPlaying(msg.c.Artist, msg.c.Track)
		if m.lib != nil {
			m.lib.AddListen(msg.c, time.Now()) // ListenBrainz-style history
		}
		if len(m.queue.Items()) == 0 && !m.aqPending {
			m.aqPending = true
			cmds = append(cmds, m.refill(msg.c))
		}
		return m, tea.Batch(cmds...)

	case mediaMsg:
		// OS / hardware transport command. Ignore stale (a newer track took over)
		// or a closed channel (mpv exited); a new track resubscribes on playOKMsg.
		if msg.closed || msg.gen != m.gen || m.now == nil {
			return m, nil
		}
		switch msg.cmd {
		case mediaNext:
			return m, m.advanceForce() // bumps gen + plays next → resubscribes
		case mediaPrev:
			m.now.Restart()
			m.position, m.posAt = 0, time.Now()
			return m, waitMedia(m.now, m.gen)
		case mediaPlayPause:
			m.now.Pause()
			m.paused = !m.paused
			m.st.paused = m.paused
			m.posAt = time.Now()
			return m, waitMedia(m.now, m.gen)
		}
		return m, waitMedia(m.now, m.gen)

	case discoverRecsMsg:
		m.forYouRecsTried = true
		m.fyRecs = msg.recs
		if m.onForYou() {
			m.rebuildForYou() // refresh the "Recommended for You" section
		}
		return m, nil

	case forYouChartMsg:
		m.fyChart = msg.tracks
		m.fyChartLabel = msg.label
		if m.onForYou() {
			m.rebuildForYou() // refresh the current-chart section
		}
		return m, nil

	case playErrMsg:
		m.loading = false
		m.now = nil
		m.st.nowKey = ""
		m.status = firstLine(msg.err.Error())
		m.isErr = true
		return m, nil

	case preloadMsg:
		delete(m.inflight, msg.key)
		if msg.err == nil && msg.url != "" {
			m.st.preloaded[msg.key] = msg.url
			m.enrichQueue(msg.c)
		}
		return m, nil

	case preloadArmMsg:
		// Debounce fired: only preload if the selection still rests here.
		if c, ok := m.selected(); ok && trackKey(c) == msg.key {
			return m, m.preload(c)
		}
		return m, nil

	case autoQueueMsg:
		m.aqPending = false
		if len(msg.results) > 0 {
			m.appendQueue(msg.results)
			if m.now == nil {
				return m, m.advance()
			}
			return m, m.preloadQueue(2)
		}
		return m, nil

	case searchMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "search: " + firstLine(msg.err.Error())
			m.isErr = true
			return m, nil
		}
		m.results.SetItems(toItems(msg.results))
		m.results.Select(0)
		m.st.focusQueue = false
		m.staticList = false // search results are re-searchable
		if msg.header != "" {
			m.header = msg.header // artist/album drills relabel the Discover title
		}
		m.status = fmt.Sprintf("%d results", len(msg.results))
		m.isErr = false
		// Warm the top results so whichever of the first few they pick is instant.
		return m, m.preloadResultsTop(3)

	case albumsMsg:
		m.loading = false
		items := make([]list.Item, 0, len(msg.albums))
		for _, a := range msg.albums {
			items = append(items, albumItem{a})
		}
		m.results.SetItems(items)
		m.results.Select(0)
		m.st.focusQueue = false
		m.staticList = true // entity list: "/" filters it
		m.header = "ALBUMS · " + msg.query
		if len(msg.albums) == 0 {
			m.status, m.isErr = "no albums found", true
		} else {
			m.status, m.isErr = fmt.Sprintf("%d albums · ↵ open", len(msg.albums)), false
		}
		return m, nil

	case artMsg:
		m.art = []string(msg)
		m.layout() // recompute seek-bar width now that art occupies a column
		return m, nil

	case browsePlaylistsMsg:
		if m.showBrowse && len(msg) > 0 {
			m.browseAll = append(m.browseAll, msg...)
			m.applyBrowseFilter() // keep the (possibly filtered) view in sync
		}
		return m, nil

	case downloadDoneMsg:
		if msg.err != nil {
			m.status = "Download failed: " + firstLine(msg.err.Error())
			m.isErr = true
		} else {
			m.status = "⬇ Saved “" + truncate(msg.track, 40) + "” to your library"
			m.isErr = false
		}
		return m, nil

	case localRefreshMsg:
		// Quietly swap in the refreshed index, but only if the user is still on
		// the Local view (and not mid-search/filter), to avoid clobbering them.
		if m.searchSource == "local" && !m.searching &&
			strings.HasPrefix(m.header, "LOCAL FILES") && len(msg.results) > 0 {
			sel := m.results.Index()
			m.results.SetItems(toItems(msg.results))
			if sel >= len(msg.results) {
				sel = len(msg.results) - 1
			}
			m.results.Select(sel)
			m.header = fmt.Sprintf("LOCAL FILES · %d", len(msg.results))
			if m.status == "refreshing…" {
				m.status = ""
			}
		}
		return m, nil

	case lyricsMsg:
		// Cache for instant reopen / prefetch (success or definitive "none").
		if msg.err == nil {
			m.lyricsCache[msg.key] = lyricsResult{synced: msg.synced, text: msg.text}
		}
		// Only update the view if the overlay is open for this exact track.
		if !m.showLyrics || msg.key != m.st.nowKey {
			return m, nil
		}
		m.lyricsBusy = false
		m.lyricsSynced = msg.synced
		switch {
		case len(msg.synced) > 0:
			// Synced lyrics render in viewLyrics (auto-follows playback).
		case msg.err != nil:
			m.lyricsVP.SetContent("  Couldn't load lyrics:\n  " + firstLine(msg.err.Error()))
		case strings.TrimSpace(msg.text) == "":
			m.lyricsVP.SetContent("  No lyrics found for this track.")
		default:
			m.lyricsVP.SetContent(msg.text)
		}
		m.lyricsVP.GotoTop()
		return m, nil

	case lyricsTickMsg:
		if m.showLyrics {
			return m, lyricsTick() // re-arm; each tick re-renders the synced view
		}
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateKeys(msg)
	}

	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		// Tab cycles the catalog source. From a fixed list it switches into a
		// search context (so you can still search YouTube from e.g. Liked). Not
		// available in a filter-only ("'") session.
		if m.promptMode == "" && !m.searchFilterOnly {
			m.staticList = false
			m.searchSource = nextSource(m.searchSource, m.searchSources())
			m.setSearchPrompt()
			if m.searchSource == "local" {
				if len(m.localAll) == 0 {
					if all, ok := local.Cached(m.dataDir); ok {
						m.localAll = all
					}
				}
				m.filterBack = m.localAll
			} else {
				m.filterBack = nil // catalog: nothing offline to filter until fetched
			}
			m.applyFilter(m.search.Value())
		}
		return m, nil
	case "esc":
		// Restore the unfiltered list (live filter/search narrowed it).
		if m.filterBack != nil {
			m.results.SetItems(toItems(m.filterBack))
			m.results.Select(0)
		}
		m.closeSearchInput()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.search.Value())

		// Playlist-name prompts capture a name instead of searching/filtering.
		if mode := m.promptMode; mode == "savequeue" || mode == "addtrack" || mode == "rename" {
			m.closeSearchInput()
			if m.lib == nil || (val == "" && mode != "rename") {
				return m, nil
			}
			switch mode {
			case "savequeue":
				m.savePlaylistFromQueue(val)
			case "addtrack":
				m.addTrackToPlaylist(val, m.promptTrack)
			case "rename":
				m.renamePlaylist(m.promptOld, val)
			}
			return m, nil
		}

		// Enter on the search box (no row picked) escalates to an online catalog
		// search; on a highlighted result it plays that result.
		online := !m.searchFilterOnly && !m.staticList &&
			(m.searchSource == "youtube" || m.searchSource == "subsonic")
		if m.searchOnInput && online && val != "" {
			kind, rest := parseBang(val)
			if rest == "" {
				kind, rest = "track", val
			}
			m.closeSearchInput()
			m.backItems = nil // fresh top-level search clears the back stack
			m.loading = true
			m.status = ""
			switch kind {
			case "artist":
				m.header = "ARTIST · " + rest
				return m, tea.Batch(cmdArtistTracks(rest), m.spin.Tick)
			case "album":
				m.header = "ALBUMS · " + rest
				return m, tea.Batch(cmdAlbumSearch(rest), m.spin.Tick)
			default:
				m.header = "SEARCH · " + rest
				return m, tea.Batch(m.searchCmd(rest), m.spin.Tick)
			}
		}
		if m.searchOnInput {
			m.results.Select(0) // no row picked → act on the top match
		}
		m.closeSearchInput()
		if _, ok := m.results.SelectedItem().(trackItem); ok {
			return m.playSelected()
		}
		return m, nil
	case "down", "ctrl+n", "pgdown":
		// First ↓ moves from the input into the results (row 1); then scroll.
		if m.promptMode != "" {
			return m, nil
		}
		if m.searchOnInput {
			m.searchOnInput = false
			m.st.hideSel = false
			m.results.Select(0)
			m.skipResultSections(tea.KeyMsg{Type: tea.KeyDown})
			return m, m.armPreload()
		}
		var c tea.Cmd
		m.results, c = m.results.Update(msg)
		m.skipResultSections(msg)
		return m, tea.Batch(c, m.armPreload())
	case "up", "ctrl+p", "pgup":
		if m.promptMode != "" || m.searchOnInput {
			return m, nil
		}
		if m.results.Index() == 0 { // leaving the first row → back to the input
			m.searchOnInput = true
			m.st.hideSel = true
			return m, nil
		}
		var c tea.Cmd
		m.results, c = m.results.Update(msg)
		m.skipResultSections(msg)
		return m, tea.Batch(c, m.armPreload())
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	if m.promptMode == "" {
		// Editing the query re-activates the input (no row selected).
		m.searchOnInput = true
		m.st.hideSel = true
		m.applyFilter(m.search.Value()) // live fuzzy across the board
	}
	return m, cmd
}

// applyFilter narrows the Discover list to fuzzy matches of q against the
// backed-up unfiltered set (empty q restores everything).
func (m *model) applyFilter(q string) {
	if strings.TrimSpace(q) == "" {
		m.results.SetItems(toItems(m.filterBack))
		m.results.Select(0)
		return
	}
	src := make([]string, len(m.filterBack))
	for i, c := range m.filterBack {
		src[i] = c.Track + " " + c.Artist
	}
	matches := fuzzy.Find(q, src)
	out := make([]engine.Candidate, 0, len(matches))
	for _, mt := range matches {
		out = append(out, m.filterBack[mt.Index])
	}
	m.results.SetItems(toItems(out))
	m.results.Select(0)
}

// closeSearchInput tears down the "/" / "'" input back to the idle state.
func (m *model) closeSearchInput() {
	m.searching = false
	m.promptMode = ""
	m.searchFilterOnly = false
	m.searchOnInput = false
	m.st.hideSel = false
	m.filterBack = nil
	m.search.Blur()
	m.search.Reset()
	m.search.Prompt = "/ "
	m.search.Placeholder = "search songs, artists…"
}

// buildForYou assembles the default "Discover" landing for the left pane from
// what we already have locally — most-played first (a personal chart), then
// recently played, then liked highlights. Returns nil when there's nothing yet
// (fresh install) so the caller can fall back to search-first.
func (m model) buildForYou() []engine.Candidate {
	if m.lib == nil {
		return nil
	}
	hist, _ := m.lib.History(500) // most-recent-first
	type agg struct {
		c     engine.Candidate
		plays int
		order int // first-seen index (0 = most recent)
	}
	seen := map[string]*agg{}
	order := 0
	for _, c := range hist {
		k := trackKey(c)
		if k == "|" {
			continue
		}
		if a, ok := seen[k]; ok {
			a.plays++
			continue
		}
		seen[k] = &agg{c: c, plays: 1, order: order}
		order++
	}

	ranked := make([]*agg, 0, len(seen))
	for _, a := range seen {
		ranked = append(ranked, a)
	}
	// Most-played first; ties broken by recency.
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].plays != ranked[j].plays {
			return ranked[i].plays > ranked[j].plays
		}
		return ranked[i].order < ranked[j].order
	})

	const maxRows = 40
	out := make([]engine.Candidate, 0, maxRows)
	used := map[string]bool{}
	// push adds c if new+valid; returns whether there's still room (so callers
	// keep going past duplicates instead of stopping on the first one).
	push := func(c engine.Candidate) bool {
		k := trackKey(c)
		if k != "|" && !used[k] {
			used[k] = true
			out = append(out, c)
		}
		return len(out) < maxRows
	}

	// "On repeat": tracks played 2+ times, ranked.
	for _, a := range ranked {
		if a.plays < 2 {
			break
		}
		if !push(a.c) {
			return out
		}
	}
	// Recently played (dedup), most recent first.
	for _, c := range hist {
		if !push(c) {
			return out
		}
	}
	// Round out with liked tracks if the history is thin.
	for _, c := range m.lib.Liked() {
		if !push(c) {
			return out
		}
	}
	return out
}

// onForYou reports whether the For You landing is the active foreground view.
func (m model) onForYou() bool {
	return m.header == "FOR YOU" && !m.searching &&
		!m.showBrowse && !m.showHelp && !m.showActions && !m.showLyrics
}

// rebuildForYou rebuilds the sectioned landing from its parts (local listening,
// engine recommendations, genre chart) into labeled, de-duplicated groups —
// nothing is blended into one list. Sections appear only when they have content.
func (m *model) rebuildForYou() {
	const perSection = 10
	seen := map[string]bool{}
	var items []list.Item
	add := func(label string, cs []engine.Candidate) {
		var rows []list.Item
		for _, c := range cs {
			if len(rows) >= perSection {
				break
			}
			k := trackKey(c)
			if k == "|" || seen[k] {
				continue
			}
			seen[k] = true
			rows = append(rows, trackItem{c})
		}
		if len(rows) == 0 {
			return
		}
		if len(items) > 0 {
			items = append(items, sectionItem{""}) // blank spacer between sections
		}
		items = append(items, sectionItem{fmt.Sprintf("%s · %d", label, len(rows))})
		items = append(items, rows...)
	}
	add("Your Music", m.fyLocal)
	add("Recommended for You", m.fyRecs)
	if len(m.fyChart) > 0 {
		label := m.fyChartLabel
		if label == "" {
			label = "Top Charts"
		}
		add(label, m.fyChart)
	}
	// Preserve the highlighted track across async section updates; else pick the
	// first real track (skipping the leading section header).
	var keep string
	if cur, ok := m.results.SelectedItem().(trackItem); ok {
		keep = trackKey(cur.c)
	}
	m.results.SetItems(items)
	sel := -1
	for i, it := range items {
		ti, ok := it.(trackItem)
		if !ok {
			continue
		}
		if sel < 0 {
			sel = i
		}
		if keep != "" && trackKey(ti.c) == keep {
			sel = i
			break
		}
	}
	if sel >= 0 {
		m.results.Select(sel)
	}
}

// skipResultSections nudges the Discover selection off a non-selectable section
// header in the direction the user was moving.
func (m *model) skipResultSections(msg tea.KeyMsg) {
	items := m.results.Items()
	n := len(items)
	if n == 0 {
		return
	}
	isSec := func(i int) bool { _, ok := items[i].(sectionItem); return ok }
	if !isSec(m.results.Index()) {
		return
	}
	dir := 1
	switch msg.String() {
	case "up", "k", "ctrl+p", "shift+tab":
		dir = -1
	}
	for i := m.results.Index() + dir; i >= 0 && i < n; i += dir {
		if !isSec(i) {
			m.results.Select(i)
			return
		}
	}
	for i := m.results.Index() - dir; i >= 0 && i < n; i -= dir { // hit an edge → reverse
		if !isSec(i) {
			m.results.Select(i)
			return
		}
	}
}

// currentResults extracts the candidates currently shown in the Discover list.
func (m model) currentResults() []engine.Candidate {
	items := m.results.Items()
	out := make([]engine.Candidate, 0, len(items))
	for _, it := range items {
		if ti, ok := it.(trackItem); ok {
			out = append(out, ti.c)
		}
	}
	return out
}

// openSearch opens the unified "/" (filterOnly=false) or "'" (filterOnly=true)
// bar. Both live-fuzzy-filter the current view across the board; "/" additionally
// fetches from the catalog (YouTube/Subsonic) on Enter. The filter base is the
// current Discover list — or, on the Local tab, the full local index.
func (m model) openSearch(filterOnly bool) (tea.Model, tea.Cmd) {
	m.searching = true
	m.promptMode = ""
	m.searchFilterOnly = filterOnly
	m.search.Reset()

	if m.searchSource == "local" {
		if len(m.localAll) == 0 {
			if all, ok := local.Cached(m.dataDir); ok {
				m.localAll = all
			}
		}
		m.filterBack = m.localAll
	} else {
		m.filterBack = m.currentResults()
	}

	if filterOnly || m.staticList {
		m.search.Prompt = "⌕ filter ▸ "
		m.search.Placeholder = "filter these…"
	} else {
		if m.searchSource == "" {
			m.searchSource = "youtube"
		}
		m.setSearchPrompt()
	}
	m.applyFilter("")      // show the full base; narrows live as you type
	m.searchOnInput = true // start on the input; ↓ moves into the results
	m.st.hideSel = true
	m.search.Focus()
	return m, textinput.Blink
}

// startPrompt opens the text input in a playlist-op mode with a placeholder
// (and optional prefilled value).
func (m *model) startPrompt(mode, placeholder, prefill string) tea.Cmd {
	m.promptMode = mode
	m.searching = true
	m.search.Reset()
	if mode == "filter" {
		m.search.Prompt = "⌕ filter ▸ "
	} else {
		m.search.Prompt = "» "
	}
	m.search.Placeholder = placeholder
	if prefill != "" {
		m.search.SetValue(prefill)
	}
	m.search.Focus()
	return textinput.Blink
}

func (m *model) savePlaylistFromQueue(name string) {
	tracks := make([]engine.Candidate, 0, len(m.queue.Items()))
	for _, it := range m.queue.Items() {
		tracks = append(tracks, it.(trackItem).c)
	}
	if err := m.lib.SavePlaylist(name, tracks); err != nil {
		m.status, m.isErr = "save failed: "+firstLine(err.Error()), true
		return
	}
	m.status, m.isErr = fmt.Sprintf("Saved playlist “%s” (%d tracks)", name, len(tracks)), false
}

func (m *model) addTrackToPlaylist(name string, c engine.Candidate) {
	existing, _ := m.lib.LoadPlaylist(name) // missing → empty
	k := trackKey(c)
	for _, e := range existing {
		if trackKey(e) == k {
			m.status, m.isErr = fmt.Sprintf("Already in “%s”", name), false
			return
		}
	}
	existing = append(existing, c)
	if err := m.lib.SavePlaylist(name, existing); err != nil {
		m.status, m.isErr = "add failed: "+firstLine(err.Error()), true
		return
	}
	m.status, m.isErr = fmt.Sprintf("Added to “%s” (%d)", name, len(existing)), false
}

func (m *model) renamePlaylist(oldName, newName string) {
	if err := m.lib.RenamePlaylist(oldName, newName); err != nil {
		m.status, m.isErr = "rename failed: "+firstLine(err.Error()), true
		return
	}
	m.status, m.isErr = fmt.Sprintf("Renamed → “%s”", newName), false
}

// searchSources lists the search sources available right now, in cycle order.
func (m model) searchSources() []string {
	s := []string{"youtube"}
	if m.sub != nil {
		s = append(s, "subsonic")
	}
	if len(m.localDirs) > 0 {
		s = append(s, "local")
	}
	return s
}

// sourceLabel is the human name for a search source ("" == YouTube).
func sourceLabel(src string) string {
	switch src {
	case "subsonic":
		return "Subsonic"
	case "local":
		return "Local"
	default:
		return "YouTube"
	}
}

// nextSource returns the source after cur in avail (wrapping). "" == youtube.
func nextSource(cur string, avail []string) string {
	if cur == "" {
		cur = "youtube"
	}
	for i, s := range avail {
		if s == cur {
			return avail[(i+1)%len(avail)]
		}
	}
	return avail[0]
}

// setSearchPrompt updates the input's prompt + placeholder to show (and, when
// more than one source exists, advertise switching) the current search source.
func (m *model) setSearchPrompt() {
	label := sourceLabel(m.searchSource)
	m.search.Prompt = "⌕ " + label + " ▸ "
	ph := "search " + label + "…"
	if m.searchSource == "youtube" {
		ph += "   ·   !a artist · !al album"
	}
	if len(m.searchSources()) > 1 {
		ph += "   ·   tab: source"
	}
	m.search.Placeholder = ph
}

// searchCmd dispatches a search to the active source (shown in the prompt).
func (m model) searchCmd(query string) tea.Cmd {
	switch m.searchSource {
	case "subsonic":
		if m.sub != nil {
			return cmdSubsonicSearch(m.sub, query)
		}
	case "local":
		if len(m.localDirs) > 0 {
			return cmdLocalSearch(m.dataDir, m.localDirs, query)
		}
	}
	return cmdSearch(query) // YouTube Music (default)
}

func (m model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys

	// Shortcuts page captures input: any of ? / esc closes, q quits.
	if m.showHelp {
		switch {
		case key.Matches(msg, k.Help), key.Matches(msg, k.Esc):
			m.showHelp = false
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		}
		return m, nil
	}

	// Listening stats page captures input: b / esc closes, q quits.
	if m.showStats {
		switch {
		case key.Matches(msg, k.Browse), key.Matches(msg, k.Esc):
			m.showStats = false
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		}
		return m, nil
	}

	// Settings overlay captures input (navigate + change values live).
	if m.showSettings {
		return m.updateSettings(msg)
	}

	// Browse menu captures input: navigate, enter opens, b/esc closes.
	if m.showBrowse {
		// Live fuzzy filter while typing (started with "/").
		if m.browseSearch {
			switch msg.Type {
			case tea.KeyEsc:
				m.browseSearch = false
				m.browseFilter = ""
				m.applyBrowseFilter()
				return m, nil
			case tea.KeyEnter:
				return m.selectBrowse()
			case tea.KeyUp:
				if m.browseCursor > 0 {
					m.browseCursor--
				}
				return m, nil
			case tea.KeyDown:
				if m.browseCursor < len(m.browseItems)-1 {
					m.browseCursor++
				}
				return m, nil
			case tea.KeyBackspace:
				if r := []rune(m.browseFilter); len(r) > 0 {
					m.browseFilter = string(r[:len(r)-1])
					m.applyBrowseFilter()
				}
				return m, nil
			case tea.KeySpace:
				m.browseFilter += " "
				m.applyBrowseFilter()
				return m, nil
			case tea.KeyRunes:
				m.browseFilter += string(msg.Runes)
				m.applyBrowseFilter()
				return m, nil
			}
			return m, nil
		}
		switch {
		case key.Matches(msg, k.Browse), key.Matches(msg, k.Esc):
			m.showBrowse = false
			return m, nil
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		case key.Matches(msg, k.Search): // "/" — filter the browse list
			m.browseSearch = true
			m.browseFilter = ""
			m.applyBrowseFilter()
			return m, nil
		case key.Matches(msg, k.Up):
			if m.browseCursor > 0 {
				m.browseCursor--
			}
			return m, nil
		case key.Matches(msg, k.Down):
			if m.browseCursor < len(m.browseItems)-1 {
				m.browseCursor++
			}
			return m, nil
		case key.Matches(msg, k.Play):
			return m.selectBrowse()
		case key.Matches(msg, k.Remove): // delete the highlighted user playlist
			return m.deleteBrowsePlaylist()
		case key.Matches(msg, k.Playlist): // 'p' = rename the highlighted playlist
			if e := m.browseSel(); e != nil && e.kind == "playlist" {
				m.showBrowse = false
				m.promptOld = e.id
				return m, m.startPrompt("rename", "new name for “"+e.id+"”…", e.id)
			}
		}
		return m, nil
	}

	// Actions menu captures input: navigate, enter runs, ./esc closes.
	if m.showActions {
		switch {
		case key.Matches(msg, k.Actions), key.Matches(msg, k.Esc):
			m.showActions = false
			return m, nil
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		case key.Matches(msg, k.Up):
			if m.actionsCursor > 0 {
				m.actionsCursor--
			}
			return m, nil
		case key.Matches(msg, k.Down):
			if m.actionsCursor < len(m.actionsItems)-1 {
				m.actionsCursor++
			}
			return m, nil
		case key.Matches(msg, k.Play):
			return m.selectAction()
		}
		return m, nil
	}

	// Lyrics overlay: y/esc close, q quits, transport controls still work, and
	// (for plain lyrics only) other keys scroll the viewport.
	if m.showLyrics {
		switch {
		case key.Matches(msg, k.Lyrics), key.Matches(msg, k.Esc):
			m.showLyrics = false
			return m, nil
		case key.Matches(msg, k.Quit):
			return m, tea.Quit
		case key.Matches(msg, k.Pause):
			if m.now != nil && m.hasMPV {
				m.now.Pause()
				m.paused = !m.paused
				m.st.paused = m.paused
				m.posAt = time.Now()
			}
			return m, nil
		case key.Matches(msg, k.SeekL):
			return m.seek(-10)
		case key.Matches(msg, k.SeekR):
			return m.seek(10)
		case key.Matches(msg, k.Next):
			if len(m.queue.Items()) > 0 {
				return m, m.advanceForce()
			}
			return m, nil
		case key.Matches(msg, k.VolU):
			m.volume = mini(100, maxi(0, m.volume)+5)
			if m.now != nil {
				m.now.SetVolume(m.volume)
			}
			return m, nil
		case key.Matches(msg, k.VolD):
			m.volume = maxi(0, maxi(0, m.volume)-5)
			if m.now != nil {
				m.now.SetVolume(m.volume)
			}
			return m, nil
		}
		// Synced lyrics auto-follow; only plain lyrics need manual scrolling.
		if len(m.lyricsSynced) > 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.lyricsVP, cmd = m.lyricsVP.Update(msg)
		return m, cmd
	}

	switch {

	case key.Matches(msg, k.Quit):
		return m, tea.Quit

	case key.Matches(msg, k.Esc):
		// Back out of an album's tracklist to the album chooser.
		if m.backItems != nil {
			m.results.SetItems(m.backItems)
			m.results.Select(0)
			m.header = m.backHeader
			m.backItems = nil
			m.staticList = true
			return m, nil
		}
		return m, nil

	case key.Matches(msg, k.Help):
		m.showHelp = true
		return m, nil

	case key.Matches(msg, k.Settings):
		m.showSettings = true
		m.settingsCursor = 0
		return m, nil

	case key.Matches(msg, k.Search):
		// "/" — live fuzzy filter of the current view, + Enter fetches from the
		// catalog (YouTube/Subsonic). On a fixed list (Liked/playlist) it filters.
		return m.openSearch(false)

	case key.Matches(msg, k.Tab):
		m.st.focusQueue = !m.st.focusQueue
		return m, nil

	case key.Matches(msg, k.Play):
		// Enter always = play the selected track.
		return m.playSelected()

	case key.Matches(msg, k.Pause):
		// Space always = play/pause. Idle → play selection; playing → toggle.
		if m.now == nil {
			return m.playSelected()
		}
		if !m.hasMPV {
			m.status = "Install mpv for pause/seek →  make stream-setup"
			m.isErr = false
			return m, nil
		}
		m.now.Pause()
		m.paused = !m.paused
		m.st.paused = m.paused
		return m, nil

	case key.Matches(msg, k.SeekL):
		return m.seek(-10)

	case key.Matches(msg, k.SeekR):
		return m.seek(10)

	case key.Matches(msg, k.Next):
		if len(m.queue.Items()) > 0 {
			return m, m.advanceForce()
		}
		m.status = "Queue empty"
		return m, nil

	// ── volume ─────────────────────────────────────────────────────────────────
	case key.Matches(msg, k.VolU):
		m.volume = mini(100, maxi(0, m.volume)+5)
		if m.now != nil {
			m.now.SetVolume(m.volume)
		}
		return m, nil
	case key.Matches(msg, k.VolD):
		m.volume = maxi(0, maxi(0, m.volume)-5)
		if m.now != nil {
			m.now.SetVolume(m.volume)
		}
		return m, nil

	// ── track verbs: lowercase = highlighted, Shift = now-playing ──────────────
	case key.Matches(msg, k.Like):
		return m.likeCand(m.verbTarget(false))
	case key.Matches(msg, k.LikeNow):
		return m.likeCand(m.verbTarget(true))
	case key.Matches(msg, k.AddQ):
		return m.addToQueueCand(m.verbTarget(false))
	case key.Matches(msg, k.QueueAll):
		return m.queueAll()
	case key.Matches(msg, k.PlayNext):
		// Shift+a = play next (insert selection at the front). The pure
		// "queue the now-playing track" would be a no-op, so Shift means front.
		if c, ok := m.verbTarget(false); ok {
			m.insertQueueHead(c)
			m.status, m.isErr = "Playing next", false
			return m, m.preload(c)
		}
		return m, nil
	case key.Matches(msg, k.Playlist):
		return m.addToPlaylistFor(m.verbTarget(false))
	case key.Matches(msg, k.PlaylistN):
		return m.addToPlaylistFor(m.verbTarget(true))
	case key.Matches(msg, k.Download):
		return m.downloadCand(m.verbTarget(false))
	case key.Matches(msg, k.DownloadN):
		return m.downloadCand(m.verbTarget(true))
	case key.Matches(msg, k.Dislike):
		return m.muteCand(m.verbTarget(false))
	case key.Matches(msg, k.DislikeNow):
		return m.muteCand(m.verbTarget(true))

	// ── station · actions menu · browse ────────────────────────────────────────
	case key.Matches(msg, k.Station):
		return m.stationCand(m.verbTarget(false))
	case key.Matches(msg, k.StationNow):
		return m.stationCand(m.verbTarget(true))
	case key.Matches(msg, k.Actions):
		return m.openActions()
	case key.Matches(msg, k.Browse):
		return m.openBrowse()
	case key.Matches(msg, k.Filter):
		// "'" — filter-only: live fuzzy of the current view, never fetches online.
		if m.st.focusQueue {
			return m, nil
		}
		return m.openSearch(true)

	// ── queue (contextual) ─────────────────────────────────────────────────────
	case key.Matches(msg, k.Remove):
		if m.st.focusQueue {
			m.removeQueueAt(m.queue.Index())
		}
		return m, nil
	case key.Matches(msg, k.Clr):
		m.queue.SetItems(nil)
		m.status = "Queue cleared"
		return m, nil
	case key.Matches(msg, k.Shuffle):
		if len(m.queue.Items()) > 1 {
			m.shuffleQueue()
			m.status, m.isErr = "Queue shuffled", false
			return m, m.preloadQueue(2)
		}
		return m, nil
	case key.Matches(msg, k.Repeat):
		m.repeat = (m.repeat + 1) % 3
		m.status, m.isErr = m.repeat.String(), false
		return m, nil

	// ── modes ──────────────────────────────────────────────────────────────────
	case key.Matches(msg, k.Sleep):
		m.cycleSleep()
		return m, nil
	case key.Matches(msg, k.Lyrics):
		return m.toggleLyrics()
	case key.Matches(msg, k.Auto):
		m.autoQueue = !m.autoQueue
		if m.autoQueue {
			m.status = "Autoplay ON"
		} else {
			m.status = "Autoplay OFF"
		}
		m.isErr = false
		return m, nil
	}

	// In the queue pane, j/k (and J/K, Shift+↑/↓) REORDER the selected track;
	// use ↑/↓ to navigate. In the discover pane, j/k navigate as usual.
	if m.st.focusQueue {
		switch msg.String() {
		case "k", "K", "shift+up":
			m.moveQueue(m.queue.Index(), -1)
			return m, nil
		case "j", "J", "shift+down":
			m.moveQueue(m.queue.Index(), +1)
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.st.focusQueue {
		m.queue, cmd = m.queue.Update(msg)
	} else {
		m.results, cmd = m.results.Update(msg)
		m.skipResultSections(msg)
	}
	return m, tea.Batch(cmd, m.armPreload())
}

// ── library / engine actions ──────────────────────────────────────────────────

// verbTarget resolves which track a verb acts on: now-playing when now is true
// (Shift), otherwise the highlighted selection in the focused pane.
func (m model) verbTarget(now bool) (engine.Candidate, bool) {
	if now {
		if m.now != nil && m.nowC.Track != "" {
			return m.nowC, true
		}
		return engine.Candidate{}, false
	}
	return m.selected()
}

func (m model) likeCand(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if m.lib == nil || !ok {
		return m, nil
	}
	k := trackKey(c)
	if m.st.likedKeys[k] {
		m.lib.Unlike(c)
		delete(m.st.likedKeys, k)
		m.status = "Removed from Liked — " + truncate(c.Track, 30)
	} else {
		m.lib.Like(c)
		m.st.likedKeys[k] = true
		m.status = "♥ Liked — " + truncate(c.Track, 30)
	}
	m.isErr = false
	return m, nil
}

func (m model) addToQueueCand(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m, nil
	}
	first := len(m.queue.Items()) == 0
	m.appendQueue([]engine.Candidate{c})
	m.status, m.isErr = fmt.Sprintf("Added · queue %d", len(m.queue.Items())), false
	if first {
		if h := m.queueHead(); h != nil {
			return m, m.preload(*h)
		}
	}
	return m, nil
}

// queueAll appends every track in the current Discover list to Up Next — works
// for any list (playlist, album, artist, charts, search, For You). Section
// headers and album-chooser rows are skipped; duplicates/now-playing are deduped.
func (m model) queueAll() (tea.Model, tea.Cmd) {
	cands := m.currentResults()
	if len(cands) == 0 {
		m.status, m.isErr = "Nothing to queue here", true
		return m, nil
	}
	first := len(m.queue.Items()) == 0
	before := len(m.queue.Items())
	m.appendQueue(cands)
	added := len(m.queue.Items()) - before
	if added == 0 {
		m.status, m.isErr = "Already in the queue", false
		return m, nil
	}
	m.status, m.isErr = fmt.Sprintf("Queued %d · queue %d", added, len(m.queue.Items())), false
	if first {
		if h := m.queueHead(); h != nil {
			return m, m.preload(*h)
		}
	}
	return m, nil
}

func (m model) addToPlaylistFor(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if m.lib == nil || !ok {
		return m, nil
	}
	m.promptTrack = c
	return m, m.startPrompt("addtrack", "add to playlist (name; new or existing)…", "")
}

func (m model) downloadCand(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m, nil
	}
	if m.downloadDir == "" {
		m.status, m.isErr = "No download folder — run 'pixeltui setup' to set one", true
		return m, nil
	}
	if !download.Downloadable(c) {
		m.status, m.isErr = "Only YouTube tracks can be downloaded (this is already a file/stream)", true
		return m, nil
	}
	m.status, m.isErr = "Downloading “"+truncate(c.Track, 30)+"”…", false
	return m, tea.Batch(cmdDownload(c, m.downloadDir), m.spin.Tick)
}

// muteCand excludes a track's artist from this session's recommendations; if the
// track is the one currently playing, it also skips to the next.
func (m model) muteCand(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m, nil
	}
	if m.rec != nil && c.Artist != "" {
		if m.rec.ExcludeArtists == nil {
			m.rec.ExcludeArtists = map[string]bool{}
		}
		m.rec.ExcludeArtists[strings.ToLower(c.Artist)] = true
	}
	m.status, m.isErr = "Muting "+c.Artist+" this session", false
	if m.now != nil && trackKey(c) == m.st.nowKey {
		return m, m.advanceForce() // skip the current track
	}
	return m, nil
}

// stationCand starts an endless station seeded by c (fresh queue + autoplay).
func (m model) stationCand(c engine.Candidate, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m, nil
	}
	m.autoQueue = true
	m.queue.SetItems(nil)
	m.loading = true
	m.status, m.isErr = "Station from "+truncate(c.Track, 30), false
	m.gen++
	return m, tea.Batch(cmdPlay(c, m.now, m.st.preloaded[trackKey(c)], m.gen), m.spin.Tick)
}

// ── actions menu (per-track verb list, opened with '.') ─────────────────────────

func (m model) openActions() (tea.Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		c, ok = m.verbTarget(true) // fall back to now-playing
	}
	if !ok {
		return m, nil
	}
	like := "Like"
	if m.st.likedKeys[trackKey(c)] {
		like = "Unlike"
	}
	items := []actionEntry{
		{"Play now", "play"},
		{like, "like"},
		{"Add to queue", "queue"},
		{"Play next", "next"},
		{"Add to playlist…", "playlist"},
		{"Start station", "station"},
	}
	if download.Downloadable(c) {
		items = append(items, actionEntry{"Download", "download"})
	}
	items = append(items, actionEntry{"Mute artist", "dislike"})
	m.actionsTrack = c
	m.actionsItems = items
	m.actionsCursor = 0
	m.showActions = true
	return m, nil
}

func (m model) selectAction() (tea.Model, tea.Cmd) {
	if m.actionsCursor < 0 || m.actionsCursor >= len(m.actionsItems) {
		return m, nil
	}
	e := m.actionsItems[m.actionsCursor]
	c := m.actionsTrack
	m.showActions = false
	switch e.kind {
	case "play":
		m.loading = true
		m.gen++
		return m, tea.Batch(cmdPlay(c, m.now, m.st.preloaded[trackKey(c)], m.gen), m.spin.Tick)
	case "like":
		return m.likeCand(c, true)
	case "queue":
		return m.addToQueueCand(c, true)
	case "next":
		m.insertQueueHead(c)
		m.status, m.isErr = "Playing next", false
		return m, m.preload(c)
	case "playlist":
		return m.addToPlaylistFor(c, true)
	case "station":
		return m.stationCand(c, true)
	case "download":
		return m.downloadCand(c, true)
	case "dislike":
		return m.muteCand(c, true)
	}
	return m, nil
}

// ── browse menu (unified source picker) ────────────────────────────────────────

func (m model) openBrowse() (tea.Model, tea.Cmd) {
	var items []browseEntry
	if m.lib != nil {
		if len(m.buildForYou()) > 0 {
			items = append(items, browseEntry{label: "✧  For You", kind: "foryou"})
		}
		items = append(items, browseEntry{label: "♥  Liked Songs", kind: "liked"})
		if names, err := m.lib.ListPlaylists(); err == nil {
			for _, n := range names {
				if n == library.LikedName {
					continue // shown above as "Liked Songs"
				}
				items = append(items, browseEntry{label: "≡  " + n, kind: "playlist", id: n})
			}
		}
	}
	// Current charts (Last.fm global / country).
	if m.chartsGlobal {
		items = append(items, browseEntry{label: "🌐  Global Top", kind: "chart_global"})
	}
	if m.chartsCountry != "" {
		items = append(items, browseEntry{label: "📍  " + m.chartsCountry + " Top", kind: "chart_geo", id: m.chartsCountry})
	}
	if m.lib != nil {
		items = append(items, browseEntry{label: "📈  Listening Stats", kind: "stats"})
	}
	if len(m.localDirs) > 0 {
		items = append(items, browseEntry{label: "♪  Local files", kind: "local"})
	}
	if m.sub != nil {
		items = append(items, browseEntry{label: "☁  Subsonic — Starred", kind: "substarred"})
	}
	if m.lib != nil && len(m.queue.Items()) > 0 {
		items = append(items, browseEntry{label: "＋  Save current queue as playlist…", kind: "savequeue"})
	}
	if len(items) == 0 {
		m.status = "Nothing to browse yet — run 'pixeltui setup' to add a library or server"
		m.isErr = true
		return m, nil
	}
	m.showBrowse = true
	m.browseAll = items
	m.browseItems = items
	m.browseCursor = 0
	m.browseFilter = ""
	m.browseSearch = false
	if m.sub != nil {
		return m, cmdSubsonicPlaylists(m.sub) // append the server's playlists async
	}
	return m, nil
}

// applyBrowseFilter narrows browseItems to fuzzy matches of browseFilter over
// the full set (browseAll), keeping the cursor in range.
func (m *model) applyBrowseFilter() {
	q := strings.TrimSpace(m.browseFilter)
	if q == "" {
		m.browseItems = m.browseAll
	} else {
		labels := make([]string, len(m.browseAll))
		for i, e := range m.browseAll {
			labels[i] = e.label
		}
		out := make([]browseEntry, 0, len(m.browseAll))
		for _, mt := range fuzzy.Find(q, labels) {
			out = append(out, m.browseAll[mt.Index])
		}
		m.browseItems = out
	}
	if m.browseCursor >= len(m.browseItems) {
		m.browseCursor = maxi(0, len(m.browseItems)-1)
	}
}

// browseSel returns the highlighted browse entry (nil if none).
func (m model) browseSel() *browseEntry {
	if m.browseCursor >= 0 && m.browseCursor < len(m.browseItems) {
		return &m.browseItems[m.browseCursor]
	}
	return nil
}

// deleteBrowsePlaylist deletes the highlighted user playlist (stays in browse).
func (m model) deleteBrowsePlaylist() (tea.Model, tea.Cmd) {
	e := m.browseSel()
	if e == nil || e.kind != "playlist" {
		return m, nil
	}
	if err := m.lib.DeletePlaylist(e.id); err != nil {
		m.status, m.isErr = "delete failed: "+firstLine(err.Error()), true
		return m, nil
	}
	name := e.id
	for i, be := range m.browseAll {
		if be.kind == "playlist" && be.id == name {
			m.browseAll = append(m.browseAll[:i], m.browseAll[i+1:]...)
			break
		}
	}
	m.applyBrowseFilter()
	if m.browseCursor >= len(m.browseItems) && m.browseCursor > 0 {
		m.browseCursor--
	}
	m.status, m.isErr = "Deleted playlist “"+name+"”", false
	return m, nil
}

func (m model) selectBrowse() (tea.Model, tea.Cmd) {
	if m.browseCursor < 0 || m.browseCursor >= len(m.browseItems) {
		return m, nil
	}
	e := m.browseItems[m.browseCursor]
	m.showBrowse = false
	if e.kind == "savequeue" {
		return m, m.startPrompt("savequeue", "save queue as playlist…", "")
	}
	m.st.focusQueue = false
	switch e.kind {
	case "foryou":
		m.fyLocal = m.buildForYou()
		m.header = "FOR YOU"
		m.searchSource = ""
		m.staticList = false
		m.status = ""
		m.rebuildForYou() // sections from local + cached recs/chart
		cmds := []tea.Cmd{m.preloadResultsTop(3)}
		if !m.forYouRecsTried && m.rec != nil && len(m.fyLocal) > 0 {
			cmds = append(cmds, cmdDiscoverRecs(m.rec, m.fyLocal[0].Artist, m.fyLocal[0].Track))
		}
		if len(m.fyChart) == 0 && (m.chartsGlobal || m.chartsCountry != "") {
			cmds = append(cmds, cmdForYouChart(m.charts, m.chartsCountry, m.chartsGlobal))
		}
		return m, tea.Batch(cmds...)
	case "stats":
		m.showBrowse = false
		m.stats = m.computeStats()
		m.showStats = true
		return m, nil
	case "chart_global":
		m.results.SetItems(nil)
		m.header = "GLOBAL TOP"
		m.searchSource, m.staticList = "", false
		m.loading, m.status, m.isErr = true, "", false
		return m, tea.Batch(cmdGlobalChart(m.charts), m.spin.Tick)
	case "chart_geo":
		m.results.SetItems(nil)
		m.header = strings.ToUpper(e.id) + " TOP"
		m.searchSource, m.staticList = "", false
		m.loading, m.status, m.isErr = true, "", false
		return m, tea.Batch(cmdGeoChart(m.charts, e.id), m.spin.Tick)
	case "liked":
		liked := m.lib.Liked()
		m.results.SetItems(toItems(liked))
		m.results.Select(0)
		m.header = fmt.Sprintf("LIKED · %d", len(liked))
		m.searchSource = ""
		m.staticList = true // fixed list → "/" filters, doesn't fetch
		m.status = ""
		return m, m.preloadResultsTop(3)
	case "playlist":
		tracks, err := m.lib.LoadPlaylist(e.id)
		if err != nil {
			m.status = "couldn't load playlist"
			m.isErr = true
			return m, nil
		}
		m.results.SetItems(toItems(tracks))
		m.results.Select(0)
		m.header = strings.ToUpper(e.id) + fmt.Sprintf(" · %d", len(tracks))
		m.searchSource = ""
		m.staticList = true // fixed list → "/" filters, doesn't fetch
		m.status = ""
		return m, m.preloadResultsTop(3)
	case "local":
		m.searchSource = "local"
		m.staticList = false // local is searchable
		// Instant: show the cached index immediately, then refresh in the
		// background (mtime-incremental, so it's quick even for big libraries).
		if cached, ok := local.Cached(m.dataDir); ok {
			m.results.SetItems(toItems(cached))
			m.results.Select(0)
			m.header = fmt.Sprintf("LOCAL FILES · %d", len(cached))
			m.status = "refreshing…"
			m.isErr = false
			return m, tea.Batch(m.preloadResultsTop(3), cmdLocalRefresh(m.dataDir, m.localDirs))
		}
		// First-ever open: full scan with a spinner.
		m.loading = true
		m.status = "Scanning local library…"
		m.header = "LOCAL FILES"
		return m, tea.Batch(cmdLocalScan(m.dataDir, m.localDirs), m.spin.Tick)
	case "substarred":
		m.loading = true
		m.status = "Loading Subsonic…"
		m.header = "SUBSONIC · STARRED"
		m.searchSource = "subsonic"
		return m, tea.Batch(cmdSubsonicStarred(m.sub), m.spin.Tick)
	case "subplaylist":
		m.loading = true
		m.status = "Loading playlist…"
		m.header = "SUBSONIC · " + strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(e.label, "☁  ")))
		m.searchSource = "subsonic"
		return m, tea.Batch(cmdSubsonicPlaylistTracks(m.sub, e.id), m.spin.Tick)
	}
	return m, nil
}

// ── playback orchestration ────────────────────────────────────────────────────

func (m model) playSelected() (tea.Model, tea.Cmd) {
	var c engine.Candidate
	if m.st.focusQueue {
		it, ok := m.queue.SelectedItem().(trackItem)
		if !ok {
			return m, nil
		}
		c = it.c
		m.removeQueueAt(m.queue.Index())
	} else {
		switch it := m.results.SelectedItem().(type) {
		case albumItem:
			// Drill into the album's tracks; remember the chooser for esc-back.
			m.backItems = m.results.Items()
			m.backHeader = m.header
			m.loading, m.status, m.isErr = true, "", false
			return m, tea.Batch(cmdAlbumTracks(it.a), m.spin.Tick)
		case trackItem:
			c = it.c
		default:
			return m, nil
		}
	}
	m.loading = true
	m.status = ""
	m.isErr = false
	m.gen++ // new deliberate play → invalidate the old track's pending polls
	return m, tea.Batch(cmdPlay(c, m.now, m.st.preloaded[trackKey(c)], m.gen), m.spin.Tick)
}

// seek moves playback by delta seconds (mpv only) with optimistic UI + a Charm
// progress-bar spring toward the new position.
func (m model) seek(delta float64) (tea.Model, tea.Cmd) {
	if m.now == nil {
		return m, nil
	}
	if !m.hasMPV {
		m.status = "Install mpv for pause/seek →  make stream-setup"
		return m, nil
	}
	m.now.Seek(delta)
	m.position = maxf(0, m.position+delta)
	if m.duration > 0 && m.position > m.duration {
		m.position = m.duration
	}
	m.posAt = time.Now() // re-anchor interpolation after the jump
	m.seeking = true
	return m, m.prog.SetPercent(m.ratio()) // springs to the new spot
}

// ratio is the current playback fraction 0..1.
func (m model) ratio() float64 {
	if m.duration <= 0 {
		return 0
	}
	r := m.position / m.duration
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// cycleSleep cycles the sleep timer: off → 15 → 30 → 60 min → off.
func (m *model) cycleSleep() {
	var next time.Duration
	if m.sleepAt.IsZero() {
		next = 15 * time.Minute
	} else {
		switch rem := time.Until(m.sleepAt); {
		case rem > 45*time.Minute:
			next = 0
		case rem > 25*time.Minute:
			next = 60 * time.Minute
		case rem > 10*time.Minute:
			next = 30 * time.Minute
		default:
			next = 0
		}
	}
	if next == 0 {
		m.sleepAt = time.Time{}
		m.status = "Sleep timer off"
	} else {
		m.sleepAt = time.Now().Add(next)
		m.status = fmt.Sprintf("Sleep timer: %d min", int(next.Minutes()))
	}
	m.isErr = false
}

// toggleLyrics opens/closes the lyrics overlay, fetching for the current track.
func (m model) toggleLyrics() (tea.Model, tea.Cmd) {
	if m.showLyrics {
		m.showLyrics = false
		return m, nil
	}
	// LRCLIB matches on artist/track, so any playing track qualifies (no need
	// for a YouTube id — Subsonic/local tracks get lyrics too).
	if m.now == nil || (m.nowC.Track == "" && m.nowC.Artist == "") {
		m.status = "Play a track to see its lyrics"
		m.isErr = false
		return m, nil
	}
	m.showLyrics = true
	m.lyricsTrack = m.nowC.Track + " — " + m.nowC.Artist
	key := trackKey(m.nowC)

	// Instant if prefetched/seen before.
	if res, ok := m.lyricsCache[key]; ok {
		m.lyricsBusy = false
		m.lyricsSynced = res.synced
		if len(res.synced) == 0 {
			if strings.TrimSpace(res.text) == "" {
				m.lyricsVP.SetContent("  No lyrics found for this track.")
			} else {
				m.lyricsVP.SetContent(res.text)
			}
			m.lyricsVP.GotoTop()
		}
		return m, lyricsTick()
	}

	m.lyricsBusy = true
	m.lyricsSynced = nil
	m.lyricsVP.SetContent("")
	m.lyricsVP.GotoTop()
	return m, tea.Batch(cmdLyrics(m.nowC, key), m.spin.Tick, lyricsTick())
}

// lyricsTick re-renders the synced lyrics overlay smoothly (between 500ms polls).
func lyricsTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return lyricsTickMsg{} })
}

// effectivePos estimates the true playback position between 500ms polls so the
// synced lyric highlight tracks the audio instead of jumping twice a second.
func (m model) effectivePos() float64 {
	p := m.position
	if m.now != nil && !m.paused && !m.posAt.IsZero() {
		p += time.Since(m.posAt).Seconds()
	}
	if m.duration > 0 && p > m.duration {
		p = m.duration
	}
	if p < 0 {
		p = 0
	}
	return p
}

// replay restarts a track from the top (repeat-one).
func (m *model) replay(c engine.Candidate) tea.Cmd {
	m.loading = true
	m.gen++
	return tea.Batch(cmdPlay(c, m.now, m.st.preloaded[trackKey(c)], m.gen), m.spin.Tick)
}

func (m *model) advance() tea.Cmd {
	if !m.autoQueue {
		return nil
	}
	return m.advanceForce()
}

func (m *model) advanceForce() tea.Cmd {
	if h := m.queueHead(); h != nil {
		c := *h
		m.removeQueueAt(0)
		m.loading = true
		m.gen++
		return tea.Batch(cmdPlay(c, m.now, m.st.preloaded[trackKey(c)], m.gen), m.spin.Tick)
	}
	if !m.aqPending && m.rec != nil {
		m.aqPending = true
		m.status = "Finding more…"
		return m.refill(m.nowC)
	}
	return nil
}

// refill pulls YouTube Music radio (via the now-playing video id) first, then
// falls back to the local recommender.
func (m model) refill(seed engine.Candidate) tea.Cmd {
	if seed.VideoID != "" {
		return cmdRadio(seed.VideoID)
	}
	return cmdRecommend(m.rec, seed.Artist, seed.Track)
}

func (m *model) preload(c engine.Candidate) tea.Cmd {
	k := trackKey(c)
	if m.st.preloaded[k] != "" || m.inflight[k] {
		return nil
	}
	m.inflight[k] = true
	return cmdPreload(c)
}

// selected returns the candidate highlighted in the focused pane.
func (m model) selected() (engine.Candidate, bool) {
	var it list.Item
	if m.st.focusQueue {
		it = m.queue.SelectedItem()
	} else {
		it = m.results.SelectedItem()
	}
	t, ok := it.(trackItem)
	return t.c, ok
}

// armPreload schedules a debounced preload of the current selection so rapid
// scrolling doesn't spawn a yt-dlp per row — only the row you rest on warms up.
func (m model) armPreload() tea.Cmd {
	c, ok := m.selected()
	if !ok {
		return nil
	}
	k := trackKey(c)
	if m.st.preloaded[k] != "" || m.inflight[k] {
		return nil
	}
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return preloadArmMsg{key: k}
	})
}

// preloadResultsTop warms the first n search/discover results.
func (m *model) preloadResultsTop(n int) tea.Cmd {
	return m.preloadItems(m.results.Items(), n)
}

// preloadQueue warms the first n queued tracks (gapless auto-advance).
func (m *model) preloadQueue(n int) tea.Cmd {
	return m.preloadItems(m.queue.Items(), n)
}

func (m *model) preloadItems(items []list.Item, n int) tea.Cmd {
	var cmds []tea.Cmd
	for i := 0; i < n && i < len(items); i++ {
		if it, ok := items[i].(trackItem); ok {
			if c := m.preload(it.c); c != nil {
				cmds = append(cmds, c)
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// ── queue helpers ─────────────────────────────────────────────────────────────

func (m *model) queueHead() *engine.Candidate {
	items := m.queue.Items()
	if len(items) == 0 {
		return nil
	}
	c := items[0].(trackItem).c
	return &c
}

func (m *model) appendQueue(cs []engine.Candidate) {
	seen := map[string]bool{}
	if m.st.nowKey != "" {
		seen[m.st.nowKey] = true
	}
	items := m.queue.Items()
	for _, it := range items {
		seen[trackKey(it.(trackItem).c)] = true
	}
	for _, c := range cs {
		k := trackKey(c)
		if seen[k] {
			continue
		}
		seen[k] = true
		items = append(items, trackItem{c})
	}
	m.queue.SetItems(items)
}

func (m *model) removeQueueAt(i int) {
	items := m.queue.Items()
	if i < 0 || i >= len(items) {
		return
	}
	items = append(items[:i], items[i+1:]...)
	m.queue.SetItems(items)
}

// insertQueueHead puts c at the front of the queue (Play Next), deduped.
func (m *model) insertQueueHead(c engine.Candidate) {
	k := trackKey(c)
	items := m.queue.Items()
	out := []list.Item{trackItem{c}}
	for _, it := range items {
		if trackKey(it.(trackItem).c) != k {
			out = append(out, it)
		}
	}
	m.queue.SetItems(out)
}

// moveQueue shifts the item at i by delta (-1 up / +1 down) and follows it.
func (m *model) moveQueue(i, delta int) {
	items := m.queue.Items()
	j := i + delta
	if i < 0 || i >= len(items) || j < 0 || j >= len(items) {
		return
	}
	items[i], items[j] = items[j], items[i]
	m.queue.SetItems(items)
	m.queue.Select(j)
}

// shuffleQueue randomises queue order.
func (m *model) shuffleQueue() {
	items := m.queue.Items()
	rand.Shuffle(len(items), func(a, b int) { items[a], items[b] = items[b], items[a] })
	m.queue.SetItems(items)
	m.queue.Select(0)
}

func (m *model) enrichQueue(c engine.Candidate) {
	items := m.queue.Items()
	key := trackKey(c)
	for i, it := range items {
		if trackKey(it.(trackItem).c) == key {
			items[i] = trackItem{c}
			m.queue.SetItems(items)
			return
		}
	}
}

// ── layout ────────────────────────────────────────────────────────────────────

const (
	artCols = 12
	artRows = 6
	nowBarH = 8 // total rows incl. border
	footerH = 1
)

func (m *model) layout() {
	if m.width == 0 {
		return
	}
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	listH := contentH - 3 // border(2) + title(1)
	if listH < 1 {
		listH = 1
	}

	if m.width >= 86 {
		leftW := (m.width * 58) / 100
		rightW := m.width - leftW
		m.results.SetSize(leftW-4, listH)
		m.queue.SetSize(rightW-4, listH)
	} else {
		m.results.SetSize(m.width-4, listH)
		m.queue.SetSize(m.width-4, listH)
	}
	m.help.Width = m.width

	// Lyrics overlay fills the body area (border 2 + title 1).
	m.lyricsVP.Width = m.width - 4
	m.lyricsVP.Height = contentH - 3
	if m.lyricsVP.Height < 1 {
		m.lyricsVP.Height = 1
	}

	// Progress bar width for the animated transport bar.
	pw := m.width - 26
	if m.art != nil {
		pw -= artCols + 2
	}
	m.prog.Width = clampInt(pw, 10, 60)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m model) artWidth() int {
	if m.width >= 100 {
		return artCols
	}
	return 0
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return "\n  Loading…\n"
	}
	body := m.viewBody()
	switch {
	case m.showHelp:
		body = m.viewHelpPage()
	case m.showStats:
		body = m.viewStats()
	case m.showSettings:
		body = m.viewSettings()
	case m.showBrowse:
		body = m.viewBrowse()
	case m.showActions:
		body = m.viewActions()
	case m.showLyrics:
		body = m.viewLyrics()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewNowBar(),
		body,
		m.viewFooter(),
	)
}

// ── listening stats ─────────────────────────────────────────────────────────

type statResult struct {
	plays, tracks, artists, totalSec int
	topArtists, topTracks            []statCount
}

type statCount struct {
	label string
	n     int
}

// computeStats aggregates the play history into headline counts and top lists.
func (m model) computeStats() statResult {
	var r statResult
	if m.lib == nil {
		return r
	}
	hist, _ := m.lib.History(5000)
	r.plays = len(hist)
	artistN, artistName := map[string]int{}, map[string]string{}
	trackN, trackLabel := map[string]int{}, map[string]string{}
	for _, c := range hist {
		r.totalSec += c.DurationSec
		if a := strings.TrimSpace(c.Artist); a != "" {
			ak := strings.ToLower(a)
			artistN[ak]++
			artistName[ak] = a
		}
		if k := trackKey(c); k != "|" {
			if _, ok := trackLabel[k]; !ok {
				trackLabel[k] = c.Track + " · " + c.Artist
			}
			trackN[k]++
		}
	}
	r.artists, r.tracks = len(artistN), len(trackN)
	r.topArtists = topCounts(artistN, artistName, 8)
	r.topTracks = topCounts(trackN, trackLabel, 8)
	return r
}

func topCounts(counts map[string]int, labels map[string]string, n int) []statCount {
	out := make([]statCount, 0, len(counts))
	for k, c := range counts {
		out = append(out, statCount{label: labels[k], n: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].label < out[j].label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// viewStats renders the listening-stats page (opened from Browse).
func (m model) viewStats() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	s := m.stats

	if s.plays == 0 {
		body := lipgloss.JoinVertical(lipgloss.Left,
			stTitle.Render("LISTENING STATS"), "",
			stDim.Render("  No listening history yet — play something and check back."))
		return paneStyle(true).Width(m.width - 2).Height(contentH - 2).Render(body)
	}

	headline := stText.Render(fmt.Sprintf("  %d plays   ·   %d tracks   ·   %d artists",
		s.plays, s.tracks, s.artists))
	if s.totalSec > 0 {
		mins := s.totalSec / 60
		dur := fmt.Sprintf("%dm", mins)
		if hrs := mins / 60; hrs > 0 {
			dur = fmt.Sprintf("%dh %dm", hrs, mins%60)
		}
		headline += stDim.Render("   ·   " + dur + " listened")
	}

	colW := (m.width - 10) / 2
	if colW < 16 {
		colW = 16
	}
	renderCol := func(title string, items []statCount) string {
		lines := []string{stTitle.Render(title)}
		for i, it := range items {
			lines = append(lines, "  "+stDim.Render(fmt.Sprintf("%2d ", i+1))+
				stText.Render(truncate(it.label, colW-9))+stDim.Render(fmt.Sprintf(" ×%d", it.n)))
		}
		return lipgloss.NewStyle().Width(colW).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		renderCol("TOP ARTISTS", s.topArtists), "   ", renderCol("TOP TRACKS", s.topTracks))

	body := lipgloss.JoinVertical(lipgloss.Left,
		stTitle.Render("LISTENING STATS"), "", headline, "", cols, "",
		stDim.Render("  b / esc to close"))
	return paneStyle(true).Width(m.width - 2).Height(contentH - 2).Render(body)
}

// ── settings overlay ─────────────────────────────────────────────────────────

const settingsRows = 5

// updateSettings handles input while the settings overlay is open. Changes apply
// live; closing (, or esc) persists them to config.json.
func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Settings), key.Matches(msg, m.keys.Esc):
		m.showSettings = false
		m.saveSettings()
		m.status, m.isErr = "Settings saved", false
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "ctrl+n":
		if m.settingsCursor < settingsRows-1 {
			m.settingsCursor++
		}
	case "right", "l", "enter", " ":
		m.changeSetting(m.settingsCursor, +1)
	case "left", "h":
		m.changeSetting(m.settingsCursor, -1)
	}
	return m, nil
}

// changeSetting adjusts the row under the cursor by dir (+1/-1), applying live.
func (m *model) changeSetting(row, dir int) {
	switch row {
	case 0: // theme — recolors the whole UI immediately
		names := ThemeNames()
		i := (idxOf(names, m.themeName) + dir + len(names)) % len(names)
		m.themeName = names[i]
		applyTheme(m.themeName)
	case 1: // discovery level
		m.explore = clampInt(m.explore+dir, 0, 10)
		if m.rec != nil {
			m.rec.Weights = engine.ExploreWeights(m.explore)
		}
	case 2: // autoplay
		m.autoQueue = !m.autoQueue
	case 3: // global chart
		m.chartsGlobal = !m.chartsGlobal
	case 4: // country chart
		i := idxOf(chartCountryList, m.chartsCountry)
		if i < 0 {
			i = 0
		}
		i = (i + dir + len(chartCountryList)) % len(chartCountryList)
		m.chartsCountry = chartCountryList[i]
	}
}

// saveSettings merges the live settings into config.json (preserving the rest).
func (m model) saveSettings() {
	if m.dataDir == "" {
		return
	}
	cfg, err := config.Load(m.dataDir)
	if err != nil {
		return
	}
	cfg.Theme = m.themeName
	cfg.Explore = m.explore
	cfg.Autoplay = m.autoQueue
	cfg.Charts.Global = m.chartsGlobal
	cfg.Charts.Country = m.chartsCountry
	_ = cfg.Save(m.dataDir)
}

func idxOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m model) viewSettings() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	row := func(i int, label, val string) string {
		l := fmt.Sprintf("%-16s", label)
		if i == m.settingsCursor {
			return stSelBar.Render("▏") + " " + stSelText.Render(l+"  "+val+"  ◂ ▸")
		}
		return "  " + stText.Render(l) + "  " + stArtist.Render(val)
	}
	country := m.chartsCountry
	if country == "" {
		country = "(off)"
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		stTitle.Render("⚙  SETTINGS"), "",
		row(0, "Theme", m.themeName),
		row(1, "Discovery level", fmt.Sprintf("%d / 10", m.explore)),
		row(2, "Autoplay", onOff(m.autoQueue)),
		row(3, "Global chart", onOff(m.chartsGlobal)),
		row(4, "Country chart", country),
		"",
		stDim.Render("↑↓ move · ◂ ▸ (or ←/→) change · , or esc to save & close"),
	)
	return stPaneFocus.Width(m.width-2).Height(contentH-2).Padding(0, 1).Render(body)
}

// viewHelpPage renders the full keyboard-shortcuts page (toggle with ?).
func (m model) viewHelpPage() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}

	type row struct{ key, desc string }
	keyStyle := lipgloss.NewStyle().Foreground(cAccent2).Bold(true).Align(lipgloss.Right).PaddingRight(2)
	descStyle := lipgloss.NewStyle().Foreground(cText)
	section := func(title string, rows []row) string {
		t := table.New().
			Border(lipgloss.HiddenBorder()).
			BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
			BorderColumn(false).BorderRow(false).
			StyleFunc(func(_, col int) lipgloss.Style {
				if col == 0 {
					return keyStyle
				}
				return descStyle
			})
		for _, r := range rows {
			t.Row(r.key, r.desc)
		}
		return lipgloss.JoinVertical(lipgloss.Left, stTitle.Render(title), t.String())
	}

	navigate := section("NAVIGATE", []row{
		{"↑ ↓", "move selection"},
		{"PgUp PgDn", "jump a page"},
		{"g G", "top / bottom"},
		{"Tab", "switch pane"},
		{"↵", "play selected"},
		{"esc", "back"},
		{"q", "quit"},
	})
	playback := section("PLAYBACK", []row{
		{"space", "pause / resume"},
		{"← →", "seek 10s  (h l)"},
		{"n", "next track"},
		{"+ −", "volume up / down"},
		{"y", "lyrics (synced)"},
		{"o O", "station: selected / playing"},
	})
	track := section("TRACK   (lower=selected · ⇧=playing)", []row{
		{"f F", "like / unlike ♥"},
		{"a A", "add selected / play next"},
		{"e", "queue every track in this list"},
		{"p P", "add to playlist"},
		{"d D", "download"},
		{"x X", "mute artist (X also skips)"},
		{".", "actions menu (all + more)"},
	})
	search := section("SEARCH   (/ online · ' filter only)", []row{
		{"type", "filter the list live"},
		{"↓", "move into results"},
		{"↵ on box", "search online"},
		{"↵ on row", "play the result"},
		{"!a !al", "artist / album (bangs)"},
	})
	queue := section("QUEUE   (Up Next · Tab)", []row{
		{"j k", "reorder selected"},
		{"del", "remove"},
		{"s", "shuffle"},
		{"r", "repeat off / all / one"},
		{"c", "clear"},
		{"z", "autoplay · t sleep timer"},
	})
	browse := section("BROWSE & MORE   (b)", []row{
		{"b", "open browse menu"},
		{",", "settings (theme, etc.)"},
		{"↵", "open entry"},
		{"/", "filter entries"},
		{"del p", "delete / rename playlist"},
	})

	gap := "    "
	notes := lipgloss.JoinVertical(lipgloss.Left,
		stDim.Render("Browse (b):  Liked · Playlists · Charts (Global/Country) · Listening Stats · Local · Subsonic"),
		stDim.Render("Search (/):  type to filter · ↓ into results · ↵ on the box searches online (YouTube/Subsonic)"),
		stDim.Render("Bangs:       !a <artist> → top songs    !al <album> → pick an album, then its tracks"),
	)
	footer := stDim.Render("? or esc to close · q quit")

	var body string
	switch {
	case m.width >= 112:
		c1 := lipgloss.JoinVertical(lipgloss.Left, navigate, "", track)
		c2 := lipgloss.JoinVertical(lipgloss.Left, playback, "", search)
		c3 := lipgloss.JoinVertical(lipgloss.Left, queue, "", browse)
		body = lipgloss.JoinHorizontal(lipgloss.Top, c1, gap, c2, gap, c3)
	case m.width >= 74:
		c1 := lipgloss.JoinVertical(lipgloss.Left, navigate, "", track, "", browse)
		c2 := lipgloss.JoinVertical(lipgloss.Left, playback, "", search, "", queue)
		body = lipgloss.JoinHorizontal(lipgloss.Top, c1, gap, c2)
	default:
		body = lipgloss.JoinVertical(lipgloss.Left,
			navigate, "", playback, "", track, "", search, "", queue, "", browse)
	}

	inner := lipgloss.JoinVertical(lipgloss.Left,
		stTitle.Render("⌨  KEYBOARD SHORTCUTS"), "", body, "", notes, "", footer)
	return stPaneFocus.Width(m.width-2).Height(contentH-2).Padding(0, 1).Render(inner)
}

func (m model) viewBrowse() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	var b strings.Builder
	if m.browseSearch {
		b.WriteString(stTitle.Render("BROWSE") + "   " + stText.Render("⌕ "+m.browseFilter) + stSelBar.Render("▏") + "\n\n")
	} else {
		b.WriteString(stTitle.Render("BROWSE") + "   " + stDim.Render("↵ open · / filter · del delete · p rename · esc close") + "\n\n")
	}
	if len(m.browseItems) == 0 {
		b.WriteString(stDim.Render("   no matches"))
	}
	for i, e := range m.browseItems {
		if i == m.browseCursor {
			b.WriteString(stSelBar.Render("▏") + stSelText.Render(" "+e.label) + "\n")
		} else {
			b.WriteString("   " + stText.Render(e.label) + "\n")
		}
	}
	if m.sub != nil {
		b.WriteString("\n" + stDim.Render("   (Subsonic playlists load as they arrive)"))
	}
	return stPaneFocus.Width(m.width-2).Height(contentH-2).Padding(0, 1).Render(b.String())
}

func (m model) viewActions() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	var b strings.Builder
	head := truncate(m.actionsTrack.Track+" — "+m.actionsTrack.Artist, m.width-16)
	b.WriteString(stTitle.Render("ACTIONS") + "  " + stArtist.Render(head) + "\n")
	b.WriteString(stDim.Render("   ↑/↓ · ↵ run · esc close") + "\n\n")
	for i, e := range m.actionsItems {
		if i == m.actionsCursor {
			b.WriteString(stSelBar.Render("▏") + stSelText.Render(" "+e.label) + "\n")
		} else {
			b.WriteString("   " + stText.Render(e.label) + "\n")
		}
	}
	return stPaneFocus.Width(m.width-2).Height(contentH-2).Padding(0, 1).Render(b.String())
}

func (m model) viewLyrics() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}
	label := "LYRICS"
	if len(m.lyricsSynced) > 0 {
		label = "LYRICS ♪ synced"
	}
	title := stTitle.Render(label) + "  " + stArtist.Render(truncate(m.lyricsTrack, m.width-20))
	if m.lyricsBusy {
		title = m.spin.View() + " " + stDim.Render("loading lyrics…")
	}
	body := m.lyricsVP.View()
	if len(m.lyricsSynced) > 0 {
		body = m.renderSyncedLyrics(m.lyricsVP.Height)
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return stPaneFocus.Width(m.width - 2).Height(contentH - 2).Render(inner)
}

// renderSyncedLyrics draws a karaoke window: the active line (last one whose
// timestamp has passed) is highlighted and kept centered, scrolling with
// playback. height is the number of lines to show.
func (m model) renderSyncedLyrics(height int) string {
	lines := m.lyricsSynced
	if height < 1 {
		height = 1
	}
	// Active line = last timestamp <= current (interpolated) position.
	pos := m.effectivePos()
	active := 0
	for i, l := range lines {
		if l.T <= pos+0.15 { // tiny lead so the line flips slightly early
			active = i
		} else {
			break
		}
	}
	// Center the active line in the window, clamped to the ends.
	start := active - height/2
	if start < 0 {
		start = 0
	}
	if start > len(lines)-height {
		start = len(lines) - height
	}
	if start < 0 {
		start = 0
	}
	w := m.width - 6
	var b strings.Builder
	for i := start; i < start+height && i < len(lines); i++ {
		txt := truncate(lines[i].Text, w)
		if txt == "" {
			txt = " "
		}
		switch {
		case i == active:
			b.WriteString(stNowTitle.Render("▌ "+txt) + "\n")
		case i == active-1 || i == active+1:
			b.WriteString(stText.Render("  "+txt) + "\n")
		default:
			b.WriteString(stDim.Render("  "+txt) + "\n")
		}
	}
	return b.String()
}

func (m model) viewNowBar() string {
	inner := m.width - 2 // box border adds 2 → total width == m.width
	if inner < 10 {
		inner = 10
	}

	var info string
	if m.now == nil {
		title := stNowTitle.Render("♫ pixeltui")
		sub := stDim.Render("/ search · ↵ play · space pause · b browse · . actions · ? keys")
		hint := ""
		if !m.hasMPV {
			hint = stYellow.Render("⚑ install mpv for pause/seek →  make stream-setup")
		}
		info = lipgloss.JoinVertical(lipgloss.Left, title, "", sub, hint)
	} else {
		// ── transport controls (Charm-rendered) ──────────────────────────────
		// ⏮  ⏯/⏸  ⏭ — the active state glows; the seek bar is an animated
		// bubbles/progress driven by a harmonica spring.
		playGlyph := "⏸"
		if m.paused {
			playGlyph = "▶"
		}
		prev := stDim.Render("⏮")
		next := stDim.Render("⏭")
		mid := stGreenB.Render(playGlyph)
		if m.seeking {
			mid = stYellow.Render(playGlyph)
		}
		transport := prev + "  " + mid + "  " + next

		// Budget the title to the actual info width (minus art + transport) so a
		// long track/artist truncates cleanly on one line instead of wrapping.
		infoW := inner
		if m.art != nil {
			infoW -= artCols + 2
		}
		const sep = "   "
		avail := infoW - lipgloss.Width(transport) - len(sep)
		if avail < 12 {
			avail = 12
		}
		artW := avail / 3
		if artW > 24 {
			artW = 24
		}
		trackW := avail - artW - 4 // "  — "
		if trackW < 6 {
			trackW = 6
		}
		title := stNowTitle.Render(truncate(m.nowC.Track, trackW)) +
			"  " + stArtist.Render("— "+truncate(m.nowC.Artist, artW))
		head := transport + sep + title

		// Animated seek bar (m.prog.View renders the spring-eased percent).
		times := fmt.Sprintf("%s / %s",
			fmtDur(time.Duration(m.position*float64(time.Second))),
			fmtDur(time.Duration(m.duration*float64(time.Second))))
		volStr := ""
		if m.volume >= 0 {
			volStr = stDim.Render(fmt.Sprintf("  vol %d%%", m.volume))
		}
		bar := m.prog.View() + "  " + stDim.Render(times) + volStr

		ctlHint := ""
		if !m.hasMPV {
			ctlHint = stYellow.Render("⚑ mpv needed for pause/seek")
		}
		// Album subtitle (replaces the blank spacer when we know it).
		albumLine := ""
		if m.nowC.Album != "" {
			albumLine = stDim.Render("  " + truncate(m.nowC.Album, infoW-2))
		}
		info = lipgloss.JoinVertical(lipgloss.Left, head, albumLine, bar, ctlHint)
	}

	content := info
	if m.now != nil && m.art != nil {
		artBlock := lipgloss.NewStyle().Width(artCols).Render(strings.Join(m.art, "\n"))
		content = lipgloss.JoinHorizontal(lipgloss.Top, artBlock, "  ", info)
	}

	return lipgloss.NewStyle().
		Width(inner).
		Height(nowBarH-2).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Render(content)
}

func (m model) viewBody() string {
	contentH := m.height - nowBarH - footerH
	if contentH < 4 {
		contentH = 4
	}

	resultsTitle := "DISCOVER"
	if m.header != "" {
		resultsTitle = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(m.header, "♫")))
	}

	var leftHeader string
	switch {
	case m.searching:
		leftHeader = m.search.View()
	case m.loading:
		leftHeader = m.spin.View() + " " + stDim.Render("loading…")
	default:
		leftHeader = stTitle.Render(truncate(resultsTitle, 40))
	}
	leftInner := lipgloss.JoinVertical(lipgloss.Left, leftHeader, m.results.View())

	queueTitle := stTitle.Render(fmt.Sprintf("UP NEXT · %d", len(m.queue.Items())))
	if m.autoQueue {
		queueTitle += "  " + stGreen.Render("⟳")
	}
	switch m.repeat {
	case repeatAll:
		queueTitle += "  " + stYellow.Render("↻ all")
	case repeatOne:
		queueTitle += "  " + stYellow.Render("↻ one")
	}
	if !m.sleepAt.IsZero() {
		queueTitle += "  " + stDim.Render(fmt.Sprintf("💤 %dm", int(time.Until(m.sleepAt).Minutes())+1))
	}
	queueBody := m.queue.View()
	if len(m.queue.Items()) == 0 {
		queueBody = stDim.Render("\n  Empty — 'a' add selected · '.' actions\n  'z' autoplay · 'b' browse")
	}
	rightInner := lipgloss.JoinVertical(lipgloss.Left, queueTitle, queueBody)

	if m.width < 86 {
		return paneStyle(!m.st.focusQueue).
			Width(m.width - 2).Height(contentH - 2).Render(leftInner)
	}

	leftW := (m.width * 58) / 100
	rightW := m.width - leftW
	left := paneStyle(!m.st.focusQueue).Width(leftW - 2).Height(contentH - 2).Render(leftInner)
	right := paneStyle(m.st.focusQueue).Width(rightW - 2).Height(contentH - 2).Render(rightInner)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return stPaneFocus
	}
	return stPane
}

func (m model) viewFooter() string {
	if m.status != "" {
		st := stYellow
		if m.isErr {
			st = stRed
		}
		return "  " + st.Render(m.status)
	}
	// Focus-aware: queue keys when Up Next is focused, discover keys otherwise.
	return m.help.View(contextHelp{k: m.keys, queueFocus: m.st.focusQueue})
}

// ── async commands ────────────────────────────────────────────────────────────

func cmdSearch(query string) tea.Cmd {
	return func() tea.Msg {
		results, err := searchYTM(query, 30)
		return searchMsg{results: results, err: err}
	}
}

// parseBang reads a leading "bang" token that scopes a search by type:
//
//	!a / !artist  → artists     !al / !album → albums     (none) → tracks
//
// It returns the kind and the remaining query.
func parseBang(q string) (kind, rest string) {
	q = strings.TrimSpace(q)
	if !strings.HasPrefix(q, "!") {
		return "track", q
	}
	tok := q
	if i := strings.IndexAny(q, " \t"); i >= 0 {
		tok, rest = q[:i], strings.TrimSpace(q[i+1:])
	}
	switch strings.ToLower(tok) {
	case "!a", "!artist":
		return "artist", rest
	case "!al", "!album":
		return "album", rest
	}
	return "track", q // unknown bang → search the whole string as tracks
}

// cmdArtistTracks resolves the top artist match and loads their top songs.
func cmdArtistTracks(query string) tea.Cmd {
	return func() tea.Msg {
		arts, err := ytm.SearchArtists(query, 1)
		if err != nil {
			return searchMsg{err: err}
		}
		if len(arts) == 0 {
			return searchMsg{err: fmt.Errorf("no artist found for %q", query)}
		}
		songs, err := ytm.ArtistTopSongs(arts[0].BrowseID, 40)
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{results: songs, header: "ARTIST · " + arts[0].Name}
	}
}

// cmdAlbumSearch returns album entities for the chooser.
func cmdAlbumSearch(query string) tea.Cmd {
	return func() tea.Msg {
		albums, err := ytm.SearchAlbums(query, 25)
		if err != nil {
			return searchMsg{err: err}
		}
		return albumsMsg{albums: albums, query: query}
	}
}

// cmdAlbumTracks drills into one album's tracklist.
func cmdAlbumTracks(a ytm.Album) tea.Cmd {
	return func() tea.Msg {
		tracks, err := ytm.AlbumTracks(a, 60)
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{results: tracks, header: "ALBUM · " + a.Title}
	}
}

// cmdSubsonicStarred loads the user's starred songs from the Subsonic server.
func cmdSubsonicStarred(sub *subsonic.Client) tea.Cmd {
	return func() tea.Msg {
		results, err := sub.Starred()
		return searchMsg{results: results, err: err}
	}
}

// cmdDownload saves a track to disk via yt-dlp (tagged + Artist/Album layout).
func cmdDownload(c engine.Candidate, dir string) tea.Cmd {
	return func() tea.Msg {
		url := "https://music.youtube.com/watch?v=" + c.VideoID
		_, err := download.Track(ytdlpPath(), url, dir)
		return downloadDoneMsg{track: c.Track, err: err}
	}
}

// cmdLocalScan (re)indexes the local folders, reusing cached metadata for
// unchanged files. Returned as a searchMsg (used on first-ever open).
func cmdLocalScan(dataDir string, dirs []string) tea.Cmd {
	return func() tea.Msg {
		results, err := local.Scan(dataDir, dirs)
		return searchMsg{results: results, err: err}
	}
}

// cmdLocalRefresh rescans in the background and reports the result as a
// localRefreshMsg, so an instant cached view can be quietly updated.
func cmdLocalRefresh(dataDir string, dirs []string) tea.Cmd {
	return func() tea.Msg {
		results, _ := local.Scan(dataDir, dirs)
		return localRefreshMsg{results: results}
	}
}

// cmdSubsonicSearch searches the Subsonic server.
func cmdSubsonicSearch(sub *subsonic.Client, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := sub.Search(query, 30)
		return searchMsg{results: results, err: err}
	}
}

// cmdLocalSearch filters the local library by query (substring). Uses the cached
// index when available (instant); falls back to a scan on first use.
func cmdLocalSearch(dataDir string, dirs []string, query string) tea.Cmd {
	return func() tea.Msg {
		all, ok := local.Cached(dataDir)
		if !ok {
			var err error
			if all, err = local.Scan(dataDir, dirs); err != nil {
				return searchMsg{err: err}
			}
		}
		q := strings.TrimSpace(query)
		if q == "" {
			return searchMsg{results: all}
		}
		// Local is our own offline index, so match it fuzzily (typo-tolerant,
		// subsequence) and rank by score — unlike YouTube/Subsonic, whose own
		// services do the matching server-side.
		hay := make([]string, len(all))
		for i, c := range all {
			hay[i] = c.Track + " " + c.Artist
		}
		out := make([]engine.Candidate, 0, len(all))
		for _, mt := range fuzzy.Find(q, hay) {
			out = append(out, all[mt.Index])
		}
		return searchMsg{results: out}
	}
}

// cmdSubsonicPlaylists fetches the server's playlists for the browse menu.
func cmdSubsonicPlaylists(sub *subsonic.Client) tea.Cmd {
	return func() tea.Msg {
		pls, err := sub.Playlists()
		if err != nil {
			return browsePlaylistsMsg(nil)
		}
		out := make([]browseEntry, 0, len(pls))
		for _, p := range pls {
			out = append(out, browseEntry{label: "☁  " + p.Name, kind: "subplaylist", id: p.ID})
		}
		return browsePlaylistsMsg(out)
	}
}

// cmdSubsonicPlaylistTracks loads the tracks of one Subsonic playlist.
func cmdSubsonicPlaylistTracks(sub *subsonic.Client, id string) tea.Cmd {
	return func() tea.Msg {
		results, err := sub.PlaylistTracks(id)
		return searchMsg{results: results, err: err}
	}
}

func cmdArt(url string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		lines, err := renderArt(url, cols, rows)
		if err != nil {
			return artMsg(nil)
		}
		return artMsg(lines)
	}
}

// ── entry ─────────────────────────────────────────────────────────────────────

func Run(cfg Config) {
	streamCache = cfg.URLCache // enable disk caching of resolved stream URLs
	if cfg.Theme != "" {
		applyTheme(cfg.Theme)
	}

	if !isTerminal(os.Stdout) {
		for i, r := range cfg.Results {
			fmt.Printf("  %2d.  %s — %s\n", i+1, r.Track, r.Artist)
		}
		return
	}

	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
	}
	if fm, ok := final.(model); ok {
		fm.now.stop()
		// Persist the queue so "Up Next" survives restarts.
		if fm.lib != nil {
			items := fm.queue.Items()
			q := make([]engine.Candidate, 0, len(items))
			for _, it := range items {
				q = append(q, it.(trackItem).c)
			}
			fm.lib.SaveSession(library.Session{Queue: q, NowPlaying: fm.nowC, PositionSec: fm.position})
		}
	}
	cleanupCovers() // remove generated pixelated cover PNGs
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// sanitize strips characters that break fixed-width terminal layout: control
// chars and Unicode "format" runes (zero-width spaces/joiners, BOM, bidi marks).
// External data (YouTube chart titles especially) is full of these, and since
// they render at zero columns but count as runes they throw column math off.
func sanitize(s string) string {
	clean := true
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f: // control
		case unicode.Is(unicode.Cf, r): // zero-width / bidi / BOM
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// truncate sanitizes then trims s to a display width of max columns (adding "…"),
// measuring by terminal cells (not rune count) so wide/zero-width chars align.
func truncate(s string, max int) string {
	s = sanitize(s)
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	target := max - 1
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > target {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// cell renders s into exactly w display columns (truncating or right-padding),
// so table columns line up regardless of the characters inside.
func cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w)
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtSec(s int) string { return fmtDur(time.Duration(s) * time.Second) }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}
