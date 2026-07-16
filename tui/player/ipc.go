package player

import (
	"encoding/json"
	"fmt"
	"time"
)

// mpv IPC — its --input-ipc-server is a Unix-domain socket on macOS/Linux and a
// named pipe on Windows. dialIPC / mpvSocketPath / ipcReady are provided
// per-platform (ipc_other.go, ipc_windows.go); everything below is
// platform-agnostic.

func ipcRound(socket string, req interface{}) (json.RawMessage, error) {
	type result struct {
		data json.RawMessage
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := dialIPC(socket)
		if err != nil {
			done <- result{nil, err}
			return
		}
		defer conn.Close()
		// Best-effort deadline where the transport supports it (Unix sockets).
		if d, ok := conn.(interface{ SetDeadline(time.Time) error }); ok {
			d.SetDeadline(time.Now().Add(250 * time.Millisecond)) //nolint:errcheck
		}
		if err := json.NewEncoder(conn).Encode(req); err != nil {
			done <- result{nil, err}
			return
		}
		dec := json.NewDecoder(conn)
		for {
			var resp struct {
				Data  json.RawMessage `json:"data"`
				Error string          `json:"error"`
				Event string          `json:"event"`
			}
			if err := dec.Decode(&resp); err != nil {
				done <- result{nil, err}
				return
			}
			// mpv broadcasts events (start-file, end-file, …) to every IPC
			// client; skip them so a boundary crossing can't be mistaken for
			// the command reply.
			if resp.Event != "" {
				continue
			}
			if resp.Error != "" && resp.Error != "success" {
				done <- result{nil, fmt.Errorf("mpv: %s", resp.Error)}
				return
			}
			done <- result{resp.Data, nil}
			return
		}
	}()

	// Hard timeout so a wedged pipe can never block the UI (covers platforms
	// where the transport doesn't honour SetDeadline, e.g. Windows pipes).
	select {
	case r := <-done:
		return r.data, r.err
	case <-time.After(400 * time.Millisecond):
		return nil, fmt.Errorf("ipc timeout")
	}
}

func ipcCmd(socket string, args ...interface{}) {
	ipcRound(socket, map[string]interface{}{"command": args}) //nolint:errcheck
}

func ipcFloat(socket, prop string) float64 {
	data, err := ipcRound(socket, map[string]interface{}{
		"command": []interface{}{"get_property", prop}, "request_id": 1,
	})
	if err != nil || data == nil {
		return 0
	}
	var v float64
	json.Unmarshal(data, &v) //nolint:errcheck
	return v
}

func ipcBool(socket, prop string) bool {
	data, err := ipcRound(socket, map[string]interface{}{
		"command": []interface{}{"get_property", prop}, "request_id": 2,
	})
	if err != nil || data == nil {
		return false
	}
	var v bool
	json.Unmarshal(data, &v) //nolint:errcheck
	return v
}

// plEntry is one mpv playlist entry (subset of the `playlist` property).
type plEntry struct {
	Filename string `json:"filename"`
	Current  bool   `json:"current"`
	ID       int    `json:"id"`
}

// parsePlaylist decodes mpv's `playlist` property payload.
func parsePlaylist(data json.RawMessage) []plEntry {
	var entries []plEntry
	json.Unmarshal(data, &entries) //nolint:errcheck
	return entries
}

func ipcPlaylist(socket string) []plEntry {
	data, err := ipcRound(socket, map[string]interface{}{
		"command": []interface{}{"get_property", "playlist"}, "request_id": 3,
	})
	if err != nil || data == nil {
		return nil
	}
	return parsePlaylist(data)
}

func playlistCurrentIndex(socket string) int {
	for i, e := range ipcPlaylist(socket) {
		if e.Current {
			return i
		}
	}
	return -1
}

func playlistIndexOfID(socket string, id int) int {
	for i, e := range ipcPlaylist(socket) {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// gaplessAppend inserts url into the RUNNING mpv right after the playing
// entry so mpv's own gapless audio (--gapless-audio=weak default; our streams
// are uniform AAC) carries playback across the boundary with no respawn.
// Per-file options set the OS Now Playing title/cover for the appended entry
// where mpv supports them. Returns the new playlist entry id.
//
// Form fallbacks, newest first:
//  1. mpv ≥ 0.38: ["loadfile", url, "insert-at", index, {options}]
//  2. older mpv:  ["loadfile", url, "append", {options}] (no index argument;
//     lands at the end, after the pad-silence — still a ~50ms boundary)
//  3. paranoia:   bare append; the title is refreshed over IPC at the boundary
func gaplessAppend(socket, url, title, cover string) (int, error) {
	opts := map[string]string{}
	if title != "" {
		opts["force-media-title"] = title
	}
	if cover != "" {
		opts["cover-art-files"] = cover
	}
	if idx := playlistCurrentIndex(socket); idx >= 0 {
		if id, err := loadfileEntryID(socket, url, "loadfile", url, "insert-at", idx+1, opts); err == nil {
			return id, nil
		}
	}
	if id, err := loadfileEntryID(socket, url, "loadfile", url, "append", opts); err == nil {
		return id, nil
	}
	return loadfileEntryID(socket, url, "loadfile", url, "append")
}

// loadfileEntryID runs a loadfile command and returns the appended entry's
// playlist id — from the command reply when mpv provides it, else by finding
// the entry in the playlist.
func loadfileEntryID(socket, url string, args ...interface{}) (int, error) {
	data, err := ipcRound(socket, map[string]interface{}{"command": args, "request_id": 4})
	if err != nil {
		return 0, err
	}
	var res struct {
		ID int `json:"playlist_entry_id"`
	}
	if data != nil {
		json.Unmarshal(data, &res) //nolint:errcheck
	}
	if res.ID != 0 {
		return res.ID, nil
	}
	entries := ipcPlaylist(socket)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Filename == url && entries[i].ID != 0 {
			return entries[i].ID, nil
		}
	}
	return 0, fmt.Errorf("mpv: no playlist entry id")
}
