package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// tracklist is a lightweight, smooth-scrolling list for tracks + section headers.
// It replaces bubbles/list (which flips a whole page at a time) with line-by-line
// scrolling, and drops the unused pagination/filtering/status machinery — so it's
// faster, dependency-light, and renders as plain strings on every platform.
//
// It keeps the small slice of the bubbles/list API the model relies on
// (Items/SetItems/Select/Index/SelectedItem/SetSize/View/Update) so the rest of
// the model is unchanged, and still stores []list.Item (trackItem/sectionItem).
type tracklist struct {
	items   []list.Item
	cursor  int // selected index
	offset  int // index of the first visible row (smooth scroll)
	w, h    int
	st      *renderState
	isQueue bool
}

func newTrackList(items []list.Item, st *renderState, isQueue bool) tracklist {
	return tracklist{items: items, st: st, isQueue: isQueue}
}

func (l tracklist) Items() []list.Item { return l.items }
func (l tracklist) Index() int         { return l.cursor }

func (l tracklist) SelectedItem() list.Item {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		return nil
	}
	return l.items[l.cursor]
}

func (l *tracklist) SetSize(w, h int) {
	l.w, l.h = w, h
	l.clamp()
}

func (l *tracklist) SetItems(items []list.Item) {
	l.items = items
	l.clamp()
}

func (l *tracklist) Select(i int) {
	l.cursor = i
	l.clamp()
}

// clamp keeps the cursor in range and the selected row scrolled into view.
func (l *tracklist) clamp() {
	if len(l.items) == 0 {
		l.cursor, l.offset = 0, 0
		return
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.items) {
		l.cursor = len(l.items) - 1
	}
	if l.h > 0 {
		if l.cursor < l.offset {
			l.offset = l.cursor
		} else if l.cursor >= l.offset+l.h {
			l.offset = l.cursor - l.h + 1
		}
	}
	if max := len(l.items) - l.h; l.offset > max {
		l.offset = max
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

// Update handles navigation keys with one-line-at-a-time scrolling.
func (l tracklist) Update(msg tea.Msg) (tracklist, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return l, nil
	}
	step := l.h - 1
	if step < 1 {
		step = 1
	}
	switch km.String() {
	case "up", "k", "ctrl+p":
		l.cursor--
	case "down", "j", "ctrl+n":
		l.cursor++
	case "pgup":
		l.cursor -= step
	case "pgdown":
		l.cursor += step
	case "home", "g":
		l.cursor = 0
	case "end", "G":
		l.cursor = len(l.items) - 1
	default:
		return l, nil
	}
	l.clamp()
	return l, nil
}

func (l tracklist) View() string {
	if l.h <= 0 {
		return ""
	}
	var b strings.Builder
	for row := 0; row < l.h; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		if i := l.offset + row; i >= 0 && i < len(l.items) {
			b.WriteString(l.renderRow(i))
		}
	}
	return b.String()
}

// renderRow renders one item to a single fixed-width line.
func (l tracklist) renderRow(index int) string {
	item := l.items[index]
	if s, ok := item.(sectionItem); ok {
		return stTitle.Render(strings.ToUpper(s.label))
	}

	var c engine.Candidate
	isAlbum := false
	durOverride := ""
	switch it := item.(type) {
	case trackItem:
		c = it.c
	case albumItem:
		isAlbum = true
		c = engine.Candidate{Track: it.a.Title, Artist: it.a.Artist}
		durOverride = it.a.Year
	default:
		return ""
	}

	width := l.w
	if width < 10 {
		width = 10
	}

	focused := l.isQueue == l.st.focusQueue
	// While the search box is active no Discover row is highlighted (the input
	// itself is the active element); the first ↓ moves into the list.
	selected := focused && index == l.cursor && !(l.st.hideSel && !l.isQueue)
	isNow := !isAlbum && l.st.nowKey != "" && trackKey(c) == l.st.nowKey

	marker := " "
	switch {
	case isAlbum:
		marker = " " // album rows: no track marker
	case isNow && l.st.paused:
		marker = "⏸"
	case isNow:
		marker = "♪"
	case l.st.likedKeys[trackKey(c)]:
		marker = "♥"
	case l.st.preloaded[trackKey(c)] != "":
		marker = "·"
	}

	// Number rows within their section: reset at each header, skip headers.
	// (Lists without sections just number 1..N continuously.)
	ord := 0
	for _, x := range l.items[:index] {
		switch x.(type) {
		case sectionItem:
			ord = 0
		case trackItem, albumItem:
			ord++
		}
	}
	num := fmt.Sprintf("%2d", ord+1)

	const durW = 5
	// Show an album column when the pane is wide enough (extra info, no crowding).
	showAlbum := width >= 72 && c.Album != ""
	artistW := 18
	if width < 50 {
		artistW = 10
	}
	albumW := 0
	if showAlbum {
		artistW = 16
		albumW = 16
		if width >= 110 {
			albumW = 24
		}
	}
	fixed := 1 + 1 + 1 + 2 + 1 + 2 + 2 + durW
	if showAlbum {
		fixed += albumW + 2
	}
	trackW := width - artistW - fixed
	if trackW < 6 {
		trackW = 6
		artistW = maxi(6, width-trackW-fixed+artistW)
	}

	track := cell(c.Track, trackW)
	artist := cell(c.Artist, artistW)
	durStr := durOverride // album rows show the year here
	if durStr == "" && c.DurationSec > 0 {
		durStr = fmtSec(c.DurationSec)
	}
	dur := fmt.Sprintf("%*s", durW, durStr)

	core := marker + " " + num + " " + track + "  " + artist
	if showAlbum {
		core += "  " + cell(c.Album, albumW)
	}
	core += "  " + dur

	switch {
	case selected:
		return stSelBar.Render("▏") + stSelText.Render(core)
	case isNow:
		return " " + stGreen.Render(core)
	default:
		row := " " + stText.Render(marker) + " " + stDim.Render(num) + " " +
			stText.Render(track) + "  " + stArtist.Render(artist)
		if showAlbum {
			row += "  " + stDim.Render(cell(c.Album, albumW))
		}
		return row + "  " + stDim.Render(dur)
	}
}
