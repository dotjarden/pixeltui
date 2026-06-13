package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/innertube"
	"github.com/dotjarden/pixeltui/tui/lyrics"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// ── pixelated cover-art cache (for the OS Now Playing widget) ──────────────────
// We generate a chunky terminal-style PNG once per art URL and reuse it. Preload
// warms this so by play time the cover is ready (no added play latency).

const (
	coverGrid = 16  // downscale resolution → blockiness
	coverOut  = 512 // upscaled PNG size
)

var (
	coverMu    sync.Mutex
	coverByURL = map[string]string{} // artURL → png path ("" = tried, none)
)

// coverFor returns a cached pixelated cover PNG for artURL, generating it on
// first use. Returns "" if unavailable. Safe for concurrent calls.
func coverFor(artURL string) string {
	if artURL == "" {
		return ""
	}
	coverMu.Lock()
	if p, ok := coverByURL[artURL]; ok {
		coverMu.Unlock()
		return p
	}
	coverMu.Unlock()

	p, err := pixelatedArtFile(artURL, coverGrid, coverOut)
	if err != nil {
		p = ""
	}
	coverMu.Lock()
	coverByURL[artURL] = p
	coverMu.Unlock()
	return p
}

// cleanupCovers removes all generated cover PNGs (call on exit).
func cleanupCovers() {
	coverMu.Lock()
	defer coverMu.Unlock()
	for _, p := range coverByURL {
		if p != "" {
			os.Remove(p) //nolint:errcheck
		}
	}
}

// ── playback ──────────────────────────────────────────────────────────────────

// playback holds one active audio stream.
type playback struct {
	cmd       *exec.Cmd
	dl        *exec.Cmd // yt-dlp feeder (pipe mode only)
	socket    string    // mpv IPC socket path (empty → no IPC control)
	ended     <-chan struct{}
	media     <-chan mediaCmd // OS/hardware transport commands (mpv only)
	mediaStop func()          // tears down the media reader
}

func (p *playback) hasEnded() bool {
	if p == nil || p.ended == nil {
		return true
	}
	select {
	case <-p.ended:
		return true
	default:
		return false
	}
}

func (p *playback) canControl() bool {
	return p != nil && p.socket != "" && !p.hasEnded()
}

func (p *playback) stop() {
	if p == nil {
		return
	}
	if p.mediaStop != nil {
		p.mediaStop()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill() //nolint:errcheck
	}
	if p.ended != nil {
		<-p.ended
	}
	if p.dl != nil && p.dl.Process != nil {
		p.dl.Process.Kill() //nolint:errcheck
		p.dl.Wait()         //nolint:errcheck
	}
	if p.socket != "" {
		removeIPC(p.socket)
	}
}

// ── mpv IPC ───────────────────────────────────────────────────────────────────
//
// mpv's --input-ipc-server is a Unix-domain socket on macOS/Linux and a named
// pipe on Windows. dialIPC / mpvSocketPath / ipcReady are provided per-platform
// (ipc_other.go, ipc_windows.go); everything below is platform-agnostic.

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

// CurrentEntryID reports the playlist entry id mpv is currently on (0 if
// unknown) — how the poll loop notices a gapless auto-advance.
func (p *playback) CurrentEntryID() int {
	if !p.canControl() {
		return 0
	}
	for _, e := range ipcPlaylist(p.socket) {
		if e.Current {
			return e.ID
		}
	}
	return 0
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

func (p *playback) Pause() {
	if p.canControl() {
		ipcCmd(p.socket, "cycle", "pause")
	}
}
func (p *playback) Seek(s float64) {
	if p.canControl() {
		ipcCmd(p.socket, "seek", s, "relative")
	}
}

// Restart seeks the current track back to the beginning (OS "previous" → restart).
func (p *playback) Restart() {
	if p.canControl() {
		ipcCmd(p.socket, "seek", 0, "absolute")
	}
}
func (p *playback) Volume() int {
	if !p.canControl() {
		return -1
	}
	return int(ipcFloat(p.socket, "volume"))
}
func (p *playback) SetVolume(v int) {
	if p.canControl() {
		ipcCmd(p.socket, "set_property", "volume", float64(v))
	}
}
func (p *playback) IsPaused() bool { return p.canControl() && ipcBool(p.socket, "pause") }
func (p *playback) Position() float64 {
	if !p.canControl() {
		return 0
	}
	return ipcFloat(p.socket, "time-pos")
}
func (p *playback) Duration() float64 {
	if !p.canControl() {
		return 0
	}
	return ipcFloat(p.socket, "duration")
}

// ── starting mpv ──────────────────────────────────────────────────────────────

func watchEnded(cmd *exec.Cmd) <-chan struct{} {
	ch := make(chan struct{})
	go func() { cmd.Wait(); close(ch) }() //nolint:errcheck
	return ch
}

// awaitSocket waits up to 4s for mpv's IPC endpoint to become connectable.
func awaitSocket(path string) bool {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if ipcReady(path) {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

func mpvBaseArgs(socket, title, coverPath string) []string {
	args := []string{
		// --vo=null (not --no-video): decodes the cover-art image so it reaches
		// the OS Now Playing widget, but opens NO window (verified windowless).
		"--vo=null",
		"--ytdl-format=bestaudio/best",
		// Pin the fast extractor client for mpv's internal yt-dlp (fallback path
		// only). MUST use -append: plain --ytdl-raw-options comma-splits the
		// value, so "android_vr,web" would break mpv startup entirely.
		"--ytdl-raw-options-append=extractor-args=youtube:player_client=android_vr,web",
		"--no-terminal",
		"--really-quiet",
		"--input-ipc-server=" + socket,
	}
	if title != "" {
		args = append(args, "--force-media-title="+title)
	}
	// Pixelated terminal-style cover art for the OS Now Playing widget (lol).
	if coverPath != "" {
		args = append(args, "--cover-art-files="+coverPath)
	}
	// OS "Now Playing" integration: macOS Control Center / MPNowPlayingInfoCenter,
	// Windows SMTC, Linux MPRIS — all via mpv's --media-controls.
	args = append(args, "--media-controls=yes")
	if runtime.GOOS == "darwin" {
		// Media keys + keep mpv out of the Dock while it owns Now Playing.
		args = append(args,
			"--input-media-keys=yes",
			"--macos-app-activation-policy=accessory",
		)
	}
	return args
}

// launchMPV starts mpv on source (direct CDN URL or a youtube watch URL) and
// waits for the IPC socket so pause/seek/volume work immediately. coverPath, if
// set, is the pixelated cover shown in the OS Now Playing widget.
func launchMPV(mpvPath, source, track, artist, coverPath string) (*playback, error) {
	sock := mpvSocketPath()
	args := append(mpvBaseArgs(sock, track+" — "+artist, coverPath), source)

	cmd := exec.Command(mpvPath, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		removeIPC(sock)
		return nil, err
	}
	if !awaitSocket(sock) {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
		removeIPC(sock)
		return nil, fmt.Errorf("mpv: IPC socket not ready")
	}
	pb := &playback{cmd: cmd, socket: sock, ended: watchEnded(cmd)}
	// Bridge OS / hardware media controls (next/prev/play-pause) to the app queue.
	pb.media, pb.mediaStop = startMediaReader(sock)
	padSkipPlaylist(sock)
	return pb, nil
}

// padSkipPlaylist gives mpv tiny silent neighbour entries so on-screen transport
// controls that drive mpv's playlist directly (Windows SMTC, and others that
// bypass the input/keybind layer) have a Next/Prev to move to — otherwise mpv
// disables those buttons on a 1-item playlist.
//
// Playlist becomes [silence, current, silence] with the current track at index
// 1. Next → the trailing silence plays out (≈50ms) → mpv exits → the existing
// auto-advance (pollMsg.ended → advance) plays the queue's next track. Prev →
// the leading silence plays out → mpv returns to the current track from the
// start (= restart). Both reuse paths that already work; the macOS keybind
// bridge overrides the default NEXT/PREV input actions, so it never double-fires.
//
// Harmless if unsupported: if the silent source or insert-at isn't available the
// entries simply don't enable the buttons (no worse than before).
func padSkipPlaylist(socket string) {
	const sentinel = "av://lavfi:anullsrc=d=0.05"        // ~50ms of silence, finite
	ipcCmd(socket, "loadfile", sentinel, "append")       // next slot  → index 1→2
	ipcCmd(socket, "loadfile", sentinel, "insert-at", 0) // prev slot  → current →1
}

// ytExtractorArgs pins YouTube player clients for extraction speed.
//
// The default behaviour probes several clients serially (~24s here). The
// "android_vr" client returns a clean, audio-only, pre-signed URL with no
// "n" signature to compute and no PO-token requirement — typically ~2× faster
// and ffplay/mpv-compatible (opus/webm). "web" is kept as a resilient fallback
// in case android_vr is ever blocked.
var ytExtractorArgs = []string{"--extractor-args", "youtube:player_client=android_vr,web"}

// withYT prepends the shared fast-extraction flags to a yt-dlp arg list.
func withYT(args ...string) []string {
	return append(append([]string{}, ytExtractorArgs...), args...)
}

// findMPV resolves mpv: $PIXELTUI_MPV → data-dir install → PATH. The data-dir
// path lets `doctor --fix` / `make stream-setup` install mpv into ~/.pixeltui and
// have the app find it without touching the system PATH (macOS bundle, or the
// Windows standalone build under ~/.pixeltui/mpv).
func findMPV() string {
	if p := os.Getenv("PIXELTUI_MPV"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, cand := range []string{
			filepath.Join(home, ".pixeltui", "mpv.app", "Contents", "MacOS", "mpv"), // macOS bundle
			filepath.Join(home, ".pixeltui", "mpv", "mpv.exe"),                      // Windows build
		} {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" { // winget portable shim (Windows)
		cand := filepath.Join(la, "Microsoft", "WinGet", "Links", "mpv.exe")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath("mpv"); err == nil {
		return p
	}
	return ""
}

// mpvAvailable reports whether mpv is installed (gates playback controls).
func mpvAvailable() bool { return findMPV() != "" }

// ytdlpPath returns the preferred yt-dlp, in priority order:
//  1. $PIXELTUI_YTDLP (explicit override)
//  2. ~/.pixeltui/ytdlp-venv/bin/yt-dlp  (pip install via `make fast-ytdlp`)
//  3. yt-dlp on PATH
//
// The pip yt-dlp starts in ~0.6s vs ~8s for the macOS PyInstaller standalone —
// the standalone re-unpacks 35MB to a temp dir on every call, which dominated
// play→audio latency. Preferring the pip build cuts cold starts ~7×.
func ytdlpPath() string {
	if p := os.Getenv("PIXELTUI_YTDLP"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		venv := filepath.Join(home, ".pixeltui", "ytdlp-venv")
		for _, cand := range []string{
			filepath.Join(venv, "bin", "yt-dlp"),         // macOS/Linux venv
			filepath.Join(venv, "Scripts", "yt-dlp.exe"), // Windows venv
		} {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
		}
		bin := filepath.Join(home, ".pixeltui", "bin", "yt-dlp")
		if runtime.GOOS == "windows" {
			bin = filepath.Join(home, ".pixeltui", "bin", "yt-dlp.exe")
		}
		if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
			return bin
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}

// urlCache caches resolved CDN URLs to disk (implemented by store.Cache).
type urlCache interface {
	GetStreamURL(videoID string) (string, bool)
	PutStreamURL(videoID, url string, expire int64)
}

// streamCache is set by Run from Config; nil disables caching.
var streamCache urlCache

// resolveStreamURL turns a video id into a direct CDN audio URL. The native
// InnerTube resolver runs first (~0.2s, single HTTP call, no subprocess);
// yt-dlp is only the fallback for rare playability quirks, so playback works
// without yt-dlp installed at all. Results are cached to disk by video id
// until the CDN URL's `expire` time, so replays/restarts are instant.
func resolveStreamURL(ytdlp, videoID string) (string, error) {
	if videoID == "" {
		return "", fmt.Errorf("no video id")
	}
	if streamCache != nil {
		if u, ok := streamCache.GetStreamURL(videoID); ok {
			return u, nil
		}
	}

	if res, err := innertube.Resolve(context.Background(), videoID); err == nil && res.URL != "" {
		if streamCache != nil {
			streamCache.PutStreamURL(videoID, res.URL, res.Expire)
		}
		return res.URL, nil
	}

	if ytdlp == "" {
		return "", fmt.Errorf("stream resolution failed (no yt-dlp fallback installed)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, ytdlp,
		withYT("--get-url", "-f", "bestaudio/best", "--no-playlist", "--quiet", ytm.WatchURL(videoID))...,
	).Output()
	if err != nil {
		return "", err
	}
	u := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0]
	if u == "" {
		return "", fmt.Errorf("no stream URL")
	}
	if streamCache != nil {
		streamCache.PutStreamURL(videoID, u, expireOf(u))
	}
	return u, nil
}

// expireOf reads the googlevideo `expire=` unix timestamp; falls back to +5h.
func expireOf(cdnURL string) int64 {
	i := strings.Index(cdnURL, "expire=")
	if i >= 0 {
		s := cdnURL[i+7:]
		if j := strings.IndexByte(s, '&'); j >= 0 {
			s = s[:j]
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return time.Now().Add(5 * time.Hour).Unix()
}

// ensureVideoID enriches a candidate with a YouTube Music video id (+ duration
// and art) if it doesn't already have one. Recommender candidates arrive bare;
// ytmusic search results already carry these.
func ensureVideoID(c engine.Candidate) engine.Candidate {
	if c.VideoID != "" {
		return c
	}
	if r, err := ytm.Resolve(c.Artist, c.Track); err == nil {
		c.VideoID = r.VideoID
		c.DurationSec = r.DurationSec
		c.ArtURL = r.ArtURL
	}
	return c
}

// startPlay begins streaming a candidate. We ALWAYS resolve the direct CDN URL
// ourselves with yt-dlp (fast android_vr client) and hand it to the player —
// mpv's internal ytdl hook is slow/fragile (it hangs on music.youtube URLs), so
// we never rely on it. Resolution order:
//  1. resolve CDN URL (preloaded if available, else yt-dlp --get-url)
//  2. play it: mpv (IPC controls) → ffplay (opus-capable)
//  3. fallback: yt-dlp | ffplay pipe, then afplay proxy (m4a) — for odd cases
//
// Returns the (possibly enriched) candidate so the UI gets duration/art.
func startPlay(c engine.Candidate, preloadedURL string) (*playback, engine.Candidate, error) {
	mpvPath := findMPV()

	// Direct-URL sources (e.g. Subsonic): the track already has a playable URL,
	// so skip ytmusic resolution and yt-dlp entirely.
	if c.StreamURL != "" {
		cover := ""
		if mpvPath != "" {
			cover = coverFor(c.ArtURL)
		}
		if pb, err := playDirectURL(mpvPath, c.StreamURL, cover, c); err == nil {
			return pb, c, nil
		}
		// fall through to generic handling if the direct play somehow failed
	}

	c = ensureVideoID(c)
	ytdlp := ytdlpPath()

	// Pixelated cover for the OS Now Playing widget (mpv only; cached/preloaded).
	cover := ""
	if mpvPath != "" {
		cover = coverFor(c.ArtURL)
	}

	var watchURL string
	if c.VideoID != "" {
		watchURL = ytm.WatchURL(c.VideoID)
	}

	// ── 1. Resolve a direct CDN URL ourselves (never mpv's internal hook) ──────
	cdnURL := preloadedURL
	if cdnURL == "" && c.VideoID != "" {
		if u, err := resolveStreamURL(ytdlp, c.VideoID); err == nil {
			cdnURL = u
		}
	}

	// ── 2. Play the direct URL: mpv (controls) preferred, else ffplay ─────────
	if cdnURL != "" {
		if pb, err := playDirectURL(mpvPath, cdnURL, cover, c); err == nil {
			return pb, c, nil
		}
	}

	// ── 3. Fallbacks (resolution failed, or no direct-URL player available) ───
	target := watchURL
	if target == "" {
		target = "ytsearch1:" + c.Track + " " + c.Artist
	}

	// 3a. mpv on the search/watch target (last resort: uses its ytdl hook).
	if mpvPath != "" && cdnURL == "" {
		if pb, err := launchMPV(mpvPath, target, c.Track, c.Artist, cover); err == nil {
			return pb, c, nil
		}
	}

	if ytdlp == "" {
		return nil, c, fmt.Errorf("stream resolution failed and yt-dlp (fallback) not found\n%s", ytdlpInstall())
	}

	// 3b. yt-dlp | ffplay pipe.
	if ffplay, _ := exec.LookPath("ffplay"); ffplay != "" && playerValid(ffplay, "-version") {
		dl := exec.Command(ytdlp, withYT("-f", "bestaudio/best", "-o", "-", "--quiet", "--no-playlist", target)...)
		pl := exec.Command(ffplay, "-nodisp", "-autoexit", "-loglevel", "quiet", "-i", "pipe:0")
		if pb, err := pipePlay(dl, pl); err == nil {
			return pb, c, nil
		}
	}

	// 3c. afplay proxy (re-resolves an m4a stream itself).
	if afplay, _ := exec.LookPath("afplay"); afplay != "" {
		pb, err := afplayProxy(ytdlp, afplay, target)
		return pb, c, err
	}

	return nil, c, fmt.Errorf("no player found\n%s", playerInstall())
}

// playDirectURL plays an already-resolved CDN URL with no yt-dlp at play time.
// Prefers mpv (IPC control); else ffplay (handles opus/webm). Returns an error
// if neither is available so the caller can fall back to the resolve path.
func playDirectURL(mpvPath, url, coverPath string, c engine.Candidate) (*playback, error) {
	if mpvPath != "" {
		return launchMPV(mpvPath, url, c.Track, c.Artist, coverPath)
	}
	ffplay, _ := exec.LookPath("ffplay")
	if ffplay == "" || !playerValid(ffplay, "-version") {
		return nil, fmt.Errorf("no direct-url player")
	}
	pl := exec.Command(ffplay, "-nodisp", "-autoexit", "-loglevel", "quiet", "-i", url)
	pl.Stderr = io.Discard
	if err := pl.Start(); err != nil {
		return nil, err
	}
	ended := make(chan struct{})
	go func() { pl.Wait(); close(ended) }() //nolint:errcheck
	select {
	case <-ended:
		return nil, fmt.Errorf("ffplay crashed on startup")
	case <-time.After(350 * time.Millisecond):
	}
	return &playback{cmd: pl, ended: ended}, nil
}

func pipePlay(dl, pl *exec.Cmd) (*playback, error) {
	pipe, err := dl.StdoutPipe()
	if err != nil {
		return nil, err
	}
	pl.Stdin = pipe
	dl.Stderr = io.Discard
	pl.Stderr = io.Discard

	if err := dl.Start(); err != nil {
		return nil, err
	}
	if err := pl.Start(); err != nil {
		dl.Process.Kill() //nolint:errcheck
		dl.Wait()         //nolint:errcheck
		return nil, err
	}

	ended := make(chan struct{})
	go func() { pl.Wait(); close(ended) }() //nolint:errcheck

	select {
	case <-ended:
		dl.Process.Kill() //nolint:errcheck
		dl.Wait()         //nolint:errcheck
		return nil, fmt.Errorf("player crashed on startup")
	case <-time.After(350 * time.Millisecond):
	}
	return &playback{cmd: pl, dl: dl, ended: ended}, nil
}

func playerValid(path, versionFlag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, versionFlag)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func afplayProxy(ytdlp, afplayPath, target string) (*playback, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// afplay (CoreAudio) can't decode opus/webm, so prefer m4a here; the "web"
	// client in ytExtractorArgs supplies an itag-140 m4a stream.
	raw, err := exec.CommandContext(ctx, ytdlp,
		withYT("--get-url", "-f", "bestaudio[ext=m4a]/bestaudio", "--no-playlist", "--quiet", target)...,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp url: %w", err)
	}
	cdnURL := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0]
	if cdnURL == "" {
		return nil, fmt.Errorf("no stream URL")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 0}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", cdnURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body) //nolint:errcheck
	})}
	go srv.Serve(ln) //nolint:errcheck

	port := ln.Addr().(*net.TCPAddr).Port
	cmd := exec.Command(afplayPath, fmt.Sprintf("http://127.0.0.1:%d/audio", port))
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		srv.Close()
		return nil, err
	}
	pb := &playback{cmd: cmd, ended: watchEnded(cmd)}
	go func() { <-pb.ended; srv.Close() }()
	return pb, nil
}

// ── async tea commands ────────────────────────────────────────────────────────

// cmdPlay starts playback. preloadedURL (if set) is a direct CDN URL for an
// instant start. gen tags the resulting playOKMsg so the model can ignore a
// play that the user has already superseded.
func cmdPlay(c engine.Candidate, old *playback, preloadedURL string, gen int) tea.Cmd {
	return func() tea.Msg {
		old.stop()
		pb, enriched, err := startPlay(c, preloadedURL)
		if err != nil {
			return playErrMsg{err}
		}
		return playOKMsg{pb: pb, c: enriched, gen: gen}
	}
}

// waitMedia blocks on the playback's media channel, turning an OS / hardware
// transport command into a mediaMsg. Returns nil when there's no media channel
// (e.g. a non-mpv fallback player). Re-issue it after each command to keep
// listening; it reports closed=true when mpv exits.
func waitMedia(pb *playback, gen int) tea.Cmd {
	if pb == nil || pb.media == nil {
		return nil
	}
	ch := pb.media
	return func() tea.Msg {
		c, ok := <-ch
		return mediaMsg{cmd: c, gen: gen, closed: !ok}
	}
}

// cmdPreload resolves the video id (if needed) and the CDN URL ahead of time so
// the next play starts near-instantly.
func cmdPreload(c engine.Candidate) tea.Cmd {
	return func() tea.Msg {
		// Direct-URL tracks (Subsonic) are already playable — just warm the cover.
		if c.StreamURL != "" {
			coverFor(c.ArtURL)
			return preloadMsg{key: trackKey(c), c: c, url: c.StreamURL}
		}
		c = ensureVideoID(c)
		key := trackKey(c)
		if c.VideoID == "" {
			return preloadMsg{key: key, c: c, err: fmt.Errorf("preload unavailable")}
		}
		url, err := resolveStreamURL(ytdlpPath(), c.VideoID)
		coverFor(c.ArtURL) // warm the pixelated cover so play time stays instant
		return preloadMsg{key: key, c: c, url: url, err: err}
	}
}

// cmdPoll samples mpv state every 500 ms (self-scheduling). gen identifies which
// track this poll belongs to so the model can drop polls from a replaced track.
func cmdPoll(pb *playback, gen int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)
		if pb == nil || pb.hasEnded() {
			return pollMsg{ended: true, gen: gen}
		}
		return pollMsg{
			pos:     pb.Position(),
			dur:     pb.Duration(),
			paused:  pb.IsPaused(),
			vol:     pb.Volume(),
			plCurID: pb.CurrentEntryID(),
			gen:     gen,
		}
	}
}

// cmdGaplessSet reconciles mpv's playlist with the queue head: drops a stale
// appended entry (removeID) and/or appends next (already resolved to url) so
// the natural end-of-track boundary plays on inside the running mpv.
func cmdGaplessSet(pb *playback, removeID int, next *engine.Candidate, url string, gen int) tea.Cmd {
	sock := pb.socket
	return func() tea.Msg {
		if removeID != 0 {
			if idx := playlistIndexOfID(sock, removeID); idx >= 0 {
				ipcCmd(sock, "playlist-remove", idx)
			}
		}
		if next == nil {
			return gaplessMsg{gen: gen}
		}
		c := *next
		cover := coverFor(c.ArtURL)
		id, err := gaplessAppend(sock, url, c.Track+" — "+c.Artist, cover)
		return gaplessMsg{id: id, key: trackKey(c), c: c, gen: gen, err: err}
	}
}

// cmdLyrics fetches lyrics for a track: LRCLIB first (synced + works without a
// YouTube id, so Subsonic/local tracks get lyrics too), then YouTube Music.
func cmdLyrics(c engine.Candidate, key string) tea.Cmd {
	return func() tea.Msg {
		if res, err := lyrics.Fetch(c.Artist, c.Track, "", c.DurationSec); err == nil && !res.Empty() {
			return lyricsMsg{key: key, synced: res.Synced, text: res.Plain}
		}
		text, err := ytm.Lyrics(c.VideoID) // fallback (plain)
		return lyricsMsg{key: key, text: text, err: err}
	}
}

// cmdRadio fetches YouTube Music's radio feed for a video id (auto-queue source
// that complements the local recommender).
func cmdRadio(videoID string) tea.Cmd {
	return func() tea.Msg {
		if videoID == "" {
			return autoQueueMsg{}
		}
		results, err := ytm.Radio(videoID, 15)
		if err != nil || len(results) == 0 {
			return autoQueueMsg{}
		}
		return autoQueueMsg{results: results}
	}
}

// cmdMultiStation blends recommendations from several seed tracks (a playlist
// station). Results arrive as a normal auto-queue fill.
func cmdMultiStation(rec *engine.Recommender, seeds []engine.Seed) tea.Cmd {
	return func() tea.Msg {
		if rec == nil || len(seeds) == 0 {
			return autoQueueMsg{station: true}
		}
		results, err := rec.RecommendMulti(seeds, 20)
		if err != nil {
			return autoQueueMsg{station: true}
		}
		return autoQueueMsg{results: results, station: true}
	}
}

// cmdRecommend fetches local-engine recommendations for auto-queue.
func cmdRecommend(rec recommender, artist, track string) tea.Cmd {
	return func() tea.Msg {
		if rec == nil {
			return autoQueueMsg{}
		}
		results, err := rec.Recommend(artist, track, 12)
		if err != nil || len(results) == 0 {
			return autoQueueMsg{}
		}
		return autoQueueMsg{results: results}
	}
}

type recommender interface {
	Recommend(artist, track string, n int) ([]engine.Candidate, error)
}

// cmdDiscoverRecs fetches engine recommendations to enrich the "For You" landing.
// Best-effort and async: a nil recommender, missing Last.fm key, or any error
// just yields no recs (the landing keeps its local content). Returns them as a
// discoverRecsMsg rather than queuing, so the caller can blend them into Discover.
func cmdDiscoverRecs(rec recommender, artist, track string) tea.Cmd {
	return func() tea.Msg {
		if rec == nil || (artist == "" && track == "") {
			return discoverRecsMsg{}
		}
		results, err := rec.Recommend(artist, track, 12)
		if err != nil {
			return discoverRecsMsg{}
		}
		return discoverRecsMsg{recs: results}
	}
}

// searchYTM runs a YouTube Music search (songs, not videos) and returns
// candidates pre-filled with video id, duration and album art.
func searchYTM(query string, limit int) ([]engine.Candidate, error) {
	return ytm.Search(query, limit)
}

// ── install hints ─────────────────────────────────────────────────────────────

func ytdlpInstall() string {
	switch runtime.GOOS {
	case "darwin":
		return "  curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos -o /usr/local/bin/yt-dlp && chmod +x /usr/local/bin/yt-dlp"
	case "linux":
		return "  pixeltui doctor --fix   (installs a self-contained yt-dlp into ~/.pixeltui/bin)"
	case "windows":
		return "  winget install yt-dlp"
	default:
		return "  https://github.com/yt-dlp/yt-dlp/releases"
	}
}

func playerInstall() string {
	switch runtime.GOOS {
	case "darwin":
		return "  Install mpv:  make stream-setup  (or: brew install mpv)"
	case "linux":
		return "  sudo apt install mpv   (or ffmpeg for ffplay fallback)"
	case "windows":
		return "  winget install mpv"
	default:
		return "  https://mpv.io/installation/"
	}
}
