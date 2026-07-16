package player

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// CoverFor returns a cached pixelated cover PNG for artURL, generating it on
// first use. Returns "" if unavailable. Safe for concurrent calls.
func CoverFor(artURL string) string {
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

// CleanupCovers removes all generated cover PNGs (call on exit).
func CleanupCovers() {
	coverMu.Lock()
	defer coverMu.Unlock()
	for _, p := range coverByURL {
		if p != "" {
			os.Remove(p) //nolint:errcheck
		}
	}
}

// fetchImage downloads and decodes an image URL (jpeg/png).
func fetchImage(url string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}

// pixelatedArtFile makes a deliberately chunky, terminal-style PNG of the album
// art: downscale to a tiny grid, then nearest-neighbor upscale so each "pixel"
// is a big square. Written to a temp PNG for mpv's --cover-art-files so the OS
// Now Playing widget shows the same pixelated art. Caller deletes it (via
// CleanupCovers).
func pixelatedArtFile(artURL string, grid, out int) (string, error) {
	src, err := fetchImage(artURL)
	if err != nil {
		return "", err
	}
	small := resizeNearest(src, grid, grid) // crush detail
	big := resizeNearest(small, out, out)   // blow it back up → chunky pixels

	path := filepath.Join(os.TempDir(), fmt.Sprintf("pixeltui-cover-%d.png", time.Now().UnixNano()))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, big); err != nil {
		os.Remove(path) //nolint:errcheck
		return "", err
	}
	return path, nil
}

// resizeNearest returns a new RGBA image of size (w, h) using nearest-neighbor
// sampling from src — no external packages. (The tui package's art.go keeps its
// own copy for terminal block-art rendering; this is the player-side primitive
// for the cover PNG so the player has no dependency on the TUI.)
func resizeNearest(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
