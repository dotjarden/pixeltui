package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// streamYouTube transcodes a YouTube track to AAC on the fly and streams it to
// the client (progressive; phone buffers it). iOS/Android play AAC natively,
// unlike the source opus/webm. Requires yt-dlp + ffmpeg on the server.
//
// The pipeline is  yt-dlp -o - | ffmpeg -c:a aac -f adts -  bound to the request
// context, so a client disconnect kills both processes.
func (s *server) streamYouTube(w http.ResponseWriter, r *http.Request, videoID string) {
	ydl := ytdlpPath()
	ff, _ := exec.LookPath("ffmpeg")
	if ydl == "" || ff == "" {
		http.Error(w, "youtube streaming needs yt-dlp + ffmpeg on the server (run: pixeltui doctor --fix)", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	watch := "https://music.youtube.com/watch?v=" + videoID

	dl := exec.CommandContext(ctx, ydl,
		"-f", "bestaudio/best", "-o", "-", "--quiet", "--no-playlist",
		"--extractor-args", "youtube:player_client=android_vr,web", watch)
	tr := exec.CommandContext(ctx, ff,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-vn", "-c:a", "aac", "-b:a", "192k", "-f", "adts", "pipe:1")

	pipe, err := dl.StdoutPipe()
	if err != nil {
		http.Error(w, "stream init failed", http.StatusInternalServerError)
		return
	}
	tr.Stdin = pipe
	tr.Stdout = w

	w.Header().Set("Content-Type", "audio/aac")
	if err := dl.Start(); err != nil {
		http.Error(w, "yt-dlp failed", http.StatusBadGateway)
		return
	}
	if err := tr.Start(); err != nil {
		_ = dl.Process.Kill()
		_ = dl.Wait()
		http.Error(w, "ffmpeg failed", http.StatusBadGateway)
		return
	}
	// Stream until done or the client goes away (ctx cancel kills both procs).
	_ = tr.Wait()
	_ = dl.Wait()
}

// ytdlpPath mirrors the app's resolver: $PIXELTUI_YTDLP → venv → PATH.
func ytdlpPath() string {
	if p := os.Getenv("PIXELTUI_YTDLP"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		venv := filepath.Join(home, ".pixeltui", "ytdlp-venv")
		cands := []string{filepath.Join(venv, "bin", "yt-dlp")}
		if runtime.GOOS == "windows" {
			cands = []string{filepath.Join(venv, "Scripts", "yt-dlp.exe")}
		}
		for _, c := range cands {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}
