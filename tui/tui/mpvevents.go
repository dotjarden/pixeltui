package tui

import (
	"bufio"
	"encoding/json"
)

// mediaCmd is an OS / hardware transport command observed from mpv.
type mediaCmd int

const (
	mediaNext mediaCmd = iota + 1
	mediaPrev
	mediaPlayPause
)

// startMediaReader binds mpv's transport keys to named script-messages and
// streams them back over a long-lived IPC connection, so OS "Now Playing" /
// hardware media controls can drive pixeltui's own queue (mpv's built-in
// next/prev act on its 1-item playlist and otherwise do nothing).
//
// It returns a channel of commands and a stop func. The channel is closed when
// the connection drops or stop() is called.
func startMediaReader(socket string) (<-chan mediaCmd, func()) {
	ch := make(chan mediaCmd, 8)
	conn, err := dialIPC(socket)
	if err != nil {
		close(ch)
		return ch, func() {}
	}

	// Route every transport key mpv understands to a script-message we observe.
	enc := json.NewEncoder(conn)
	for _, b := range [][2]string{
		{"NEXT", "px-next"}, {"PREV", "px-prev"},
		{"FORWARD", "px-next"}, {"REWIND", "px-prev"},
		{"PLAY", "px-pp"}, {"PAUSE", "px-pp"},
		{"PLAYPAUSE", "px-pp"}, {"STOP", "px-pp"},
	} {
		enc.Encode(map[string]any{ //nolint:errcheck
			"command": []any{"keybind", b[0], "script-message " + b[1]},
		})
	}

	done := make(chan struct{})
	go func() {
		defer close(ch)
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			select {
			case <-done:
				return
			default:
			}
			var ev struct {
				Event string   `json:"event"`
				Args  []string `json:"args"`
			}
			if json.Unmarshal(sc.Bytes(), &ev) != nil ||
				ev.Event != "client-message" || len(ev.Args) == 0 {
				continue
			}
			switch ev.Args[0] {
			case "px-next":
				ch <- mediaNext
			case "px-prev":
				ch <- mediaPrev
			case "px-pp":
				ch <- mediaPlayPause
			}
		}
	}()

	return ch, func() {
		close(done)
		conn.Close() //nolint:errcheck
	}
}
