// Package download saves tracks to a local music library using yt-dlp, with
// embedded tags + cover art and an Artist/Album/Title folder layout — i.e. the
// conventional structure a Subsonic/Navidrome server expects to scan.
package download

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// outputTemplate organises files as <dir>/Artist/Album/Title.ext with sensible
// fallbacks, so a Subsonic/Navidrome scan picks them up cleanly.
const outputTemplate = "%(artist,uploader,creator|Unknown Artist)s/%(album,playlist_title|Singles)s/%(track,title)s.%(ext)s"

// args builds the yt-dlp argument list (separated for testability). thumb embeds
// cover art (needs the `mutagen` module; we retry without it if unavailable).
func args(dir, watchURL string, thumb bool) []string {
	a := []string{
		"--extractor-args", "youtube:player_client=android_vr,web",
		"-x",                   // extract audio (keeps source codec — no quality loss)
		"--audio-quality", "0", // best
		"--embed-metadata", // write artist/title/album/date tags into the file
	}
	if thumb {
		a = append(a, "--embed-thumbnail") // cover art (best-effort)
	}
	return append(a,
		"--no-playlist", "--no-warnings", "--quiet",
		"--paths", dir, // base output directory
		"-o", outputTemplate,
		"--print", "after_move:filepath", // emit the final path on success
		watchURL,
	)
}

// Track downloads one candidate's audio into dir. ytdlp is the yt-dlp binary
// path (caller supplies the fast one). watchURL is its source URL. Returns the
// saved file path. Only YouTube-resolvable tracks are downloadable.
func Track(ytdlp, watchURL, dir string) (string, error) {
	if ytdlp == "" {
		return "", fmt.Errorf("yt-dlp not found")
	}
	if watchURL == "" {
		return "", fmt.Errorf("track has no downloadable source")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Try with cover art; if that fails (e.g. mutagen missing), retry without so
	// the track still downloads with full tags.
	if path, err := run(ytdlp, args(dir, watchURL, true)); err == nil {
		return path, nil
	}
	return run(ytdlp, args(dir, watchURL, false))
}

func run(ytdlp string, a []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, ytdlp, a...).Output()
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if i := strings.IndexByte(path, '\n'); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return "", fmt.Errorf("download produced no file")
	}
	return path, nil
}

// Downloadable reports whether a candidate can be downloaded (has a YouTube id).
// Subsonic/local tracks are already files on disk/server, so they're skipped.
func Downloadable(c engine.Candidate) bool {
	return c.VideoID != ""
}
