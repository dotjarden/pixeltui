package tui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

// renderArt downloads an image from artURL, resizes it to cols×(rows*2) pixels
// using nearest-neighbor interpolation (stdlib only, no external deps), then
// renders each 2-pixel-tall column pair as the Unicode UPPER HALF BLOCK "▀"
// with 24-bit ANSI fg color (top pixel) and bg color (bottom pixel).
//
// Returns `rows` strings; each has `cols` visual characters but is longer in
// bytes due to ANSI escape codes. On any error returns nil, err.
func renderArt(artURL string, cols, rows int) ([]string, error) {
	src, err := fetchImage(artURL)
	if err != nil {
		return nil, err
	}

	// Resize to cols × (rows*2) pixels using nearest-neighbor.
	// Each character cell represents 2 vertical pixels (▀ upper half block).
	img := resizeNearest(src, cols, rows*2)

	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		var sb strings.Builder
		for col := 0; col < cols; col++ {
			top := rgbaAt(img, col, row*2)
			bot := rgbaAt(img, col, row*2+1)
			sb.WriteString(ansiBlock(top, bot))
		}
		lines[row] = sb.String()
	}
	return lines, nil
}

// pixelatedArtFile makes a deliberately chunky, terminal-style PNG of the album
// art: downscale to a tiny grid (same blocky look we render in the TUI), then
// nearest-neighbor upscale so each "pixel" is a big square. Written to a temp
// PNG for mpv's --cover-art-files so the OS Now Playing widget shows the same
// pixelated art — just for the lol. Returns the file path (caller deletes it).
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

// ── image helpers ─────────────────────────────────────────────────────────────

// resizeNearest returns a new RGBA image of size (w, h) using
// nearest-neighbor sampling from src. No external packages required.
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

// rgbaAt reads a pixel from the image and returns it as color.RGBA.
func rgbaAt(img image.Image, x, y int) color.RGBA {
	c := img.At(x, y)
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
}

// ansiBlock returns a single "▀" character styled with 24-bit ANSI colors:
// fg = top pixel (upper half block uses foreground color),
// bg = bot pixel (lower half block is background).
func ansiBlock(top, bot color.RGBA) string {
	return fmt.Sprintf(
		"\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[0m",
		top.R, top.G, top.B,
		bot.R, bot.G, bot.B,
	)
}
