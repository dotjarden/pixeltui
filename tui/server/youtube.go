package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// YouTube streaming, fast path: resolve a pre-signed AAC/m4a CDN URL natively
// via InnerTube (~0.2s; see innertube.go), cache it on disk until the URL's
// own expiry, and proxy it range-aware — the same treatment Subsonic gets.
// iOS/Android play AAC natively, so no transcoding: the client gets
// Content-Length + byte ranges, which means instant seeking and proper
// buffering. yt-dlp is only the resolution fallback, and the ffmpeg transcode
// pipeline below only serves the rare video with no m4a rendition.

// resolveM4A returns a direct AAC/m4a CDN URL for a video id, consulting the
// shared stream-URL disk cache first. The cache key is suffixed so it never
// collides with the TUI's bestaudio (opus) entries for the same video.
func (s *server) resolveM4A(videoID string) (string, error) {
	key := videoID + "|m4a"
	if s.cfg.StreamCache != nil {
		if u, ok := s.cfg.StreamCache.GetStreamURL(key); ok {
			return u, nil
		}
	}
	// Single-flight: AVPlayer fires a burst of concurrent range requests at
	// track start. Without this every one of them misses the still-cold cache
	// and kicks off its own resolution (a stampede that multiplied the cold
	// start to ~5s). Collapsing them onto one resolution means the burst shares
	// a single ~0.2s call, then everyone reads the warm cache.
	v, err, _ := s.resolveGroup.Do(key, func() (any, error) {
		return s.resolveM4AUncached(videoID, key)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (s *server) resolveM4AUncached(videoID, key string) (string, error) {
	// Re-check the cache: a concurrent flight may have just populated it.
	if s.cfg.StreamCache != nil {
		if u, ok := s.cfg.StreamCache.GetStreamURL(key); ok {
			return u, nil
		}
	}

	// Fast path: native InnerTube ANDROID_VR resolution (~0.2s, no yt-dlp).
	if res, err := innertubeResolve(context.Background(), videoID); err == nil && res.url != "" {
		if s.cfg.StreamCache != nil {
			s.cfg.StreamCache.PutStreamURL(key, res.url, res.expire)
		}
		return res.url, nil
	}

	// Fallback: yt-dlp resolution (Python; ~2s). Used only when the native
	// path fails (rare playability quirks).
	ydl := ytdlpPath()
	if ydl == "" {
		return "", fmt.Errorf("yt-dlp not found")
	}
	u, err := ytGetURL(ydl, videoID, "android_vr")
	if err != nil {
		u, err = ytGetURL(ydl, videoID, "android_vr,web")
	}
	if err != nil {
		return "", err
	}
	if s.cfg.StreamCache != nil {
		s.cfg.StreamCache.PutStreamURL(key, u, expireOf(u))
	}
	return u, nil
}

func ytGetURL(ydl, videoID, clients string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ydl,
		"--extractor-args", "youtube:player_client="+clients,
		"--get-url", "-f", "bestaudio[ext=m4a]/bestaudio[acodec^=mp4a]",
		"--no-playlist", "--quiet",
		"https://music.youtube.com/watch?v="+videoID).Output()
	if err != nil {
		return "", fmt.Errorf("yt-dlp: %w", err)
	}
	u := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if u == "" {
		return "", fmt.Errorf("no m4a stream")
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

// streamYouTube serves audio for a video id: m4a proxy first, transcode last.
func (s *server) streamYouTube(w http.ResponseWriter, r *http.Request, videoID string) {
	if u, err := s.resolveM4A(videoID); err == nil {
		s.proxy(w, r, u)
		return
	}
	s.transcodeYouTube(w, r, videoID)
}

// transcodeYouTube is the legacy fallback: yt-dlp -o - | ffmpeg → ADTS. No
// ranges, no seeking — used only when no m4a rendition exists.
func (s *server) transcodeYouTube(w http.ResponseWriter, r *http.Request, videoID string) {
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
