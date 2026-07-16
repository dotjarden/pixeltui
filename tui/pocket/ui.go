// Package pocket is pixeltui's hardware-player front-end: it renders a now-playing
// screen and maps physical buttons onto playback, driving the headless
// session.Controller. The actual panel and GPIO live behind the Display/Buttons
// interfaces — a Raspberry Pi + Pirate Audio (ST7789 + buttons) backend for the
// device, and a laptop dev backend (PNG out + keyboard) so it runs anywhere.
package pocket

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// Size is the square panel edge (the Pirate Audio ST7789 is 240×240).
const Size = 240

// View is the state the now-playing screen draws.
type View struct {
	Mode   string // "standalone" | "serve" | "party"
	Track  engine.Candidate
	Pos    float64
	Dur    float64
	Paused bool
	Status string      // shown when there's no track (e.g. "queue empty", a party code)
	Cover  image.Image // optional album art (decoded); nil → no art
	Frame  int         // animation frame (drives marquee scrolling of long names)
}

// Render draws the now-playing screen at Size×Size.
func Render(v View) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	fill(img, img.Bounds(), color.RGBA{18, 18, 22, 255})

	if v.Cover != nil {
		drawScaled(img, img.Bounds(), v.Cover)
	}

	// Bottom info strip (semi-transparent over the art).
	const barH = 66
	fill(img, image.Rect(0, Size-barH, Size, Size), color.RGBA{0, 0, 0, 205})

	if v.Track.Track != "" {
		title := v.Track.Track
		if v.Paused {
			title = "|| " + title
		}
		// Marquee long names so they read fully on the narrow panel.
		drawMarquee(img, 8, Size-barH+8, Size-16, title, color.RGBA{240, 240, 245, 255}, 2, v.Frame)
		drawMarquee(img, 8, Size-barH+34, Size-16, v.Track.Artist, color.RGBA{175, 175, 188, 255}, 1, v.Frame)
	} else {
		drawText(img, 8, Size/2-8, truncate(orDefault(v.Status, "no track"), 18), color.RGBA{150, 150, 162, 255}, 2)
	}

	// Progress bar.
	py := Size - 12
	fill(img, image.Rect(8, py, Size-8, py+4), color.RGBA{60, 60, 72, 255})
	if v.Dur > 0 {
		frac := clamp01(v.Pos / v.Dur)
		w := int(float64(Size-16) * frac)
		fill(img, image.Rect(8, py, 8+w, py+4), modeColor(v.Mode))
	}

	return img
}

// ToRGB565 converts an RGBA frame to big-endian RGB565 bytes — the pixel format
// the ST7789 expects for a RAMWR blit.
func ToRGB565(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*2)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			v := uint16(c.R&0xF8)<<8 | uint16(c.G&0xFC)<<3 | uint16(c.B>>3)
			out = append(out, byte(v>>8), byte(v))
		}
	}
	return out
}

// ── draw helpers (stdlib only) ─────────────────────────────────────────────────

func fill(dst *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(dst, r, image.NewUniform(c), image.Point{}, draw.Over)
}

// drawScaled nearest-neighbour-blits src to fill r.
func drawScaled(dst *image.RGBA, r image.Rectangle, src image.Image) {
	sb := src.Bounds()
	if sb.Dx() == 0 || sb.Dy() == 0 {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sy := sb.Min.Y + (y-r.Min.Y)*sb.Dy()/r.Dy()
		for x := r.Min.X; x < r.Max.X; x++ {
			sx := sb.Min.X + (x-r.Min.X)*sb.Dx()/r.Dx()
			dst.Set(x, y, src.At(sx, sy))
		}
	}
}

// drawText renders s with the 7×13 bitmap font, nearest-upscaled by scale.
func drawText(dst *image.RGBA, x, y int, s string, col color.Color, scale int) {
	drawTextClip(dst, x, y, s, col, scale, dst.Bounds().Min.X, dst.Bounds().Max.X)
}

// drawTextClip is drawText but only paints columns in [clipMinX, clipMaxX) — used
// by the marquee so scrolling text stays inside its strip.
func drawTextClip(dst *image.RGBA, x, y int, s string, col color.Color, scale, clipMinX, clipMaxX int) {
	if s == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	face := basicfont.Face7x13
	d := &font.Drawer{Face: face}
	w := d.MeasureString(s).Ceil()
	h := face.Metrics().Height.Ceil()
	if w <= 0 || h <= 0 {
		return
	}
	tmp := image.NewRGBA(image.Rect(0, 0, w, h))
	td := &font.Drawer{
		Dst:  tmp,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(0, face.Metrics().Ascent.Ceil()),
	}
	td.DrawString(s)
	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			c := tmp.RGBAAt(tx, ty)
			if c.A == 0 {
				continue
			}
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					px := x + tx*scale + sx
					if px < clipMinX || px >= clipMaxX {
						continue
					}
					dst.Set(px, y+ty*scale+sy, c)
				}
			}
		}
	}
}

// drawMarquee draws s at (x,y) within width w: static if it fits, otherwise it
// scrolls left and loops (advanced by frame), so long names read fully.
func drawMarquee(dst *image.RGBA, x, y, w int, s string, col color.Color, scale, frame int) {
	tw := textWidth(s, scale)
	if tw <= w {
		drawTextClip(dst, x, y, s, col, scale, x, x+w)
		return
	}
	const gap = 28
	period := tw + gap
	off := (frame * 3) % period // 3px/frame
	drawTextClip(dst, x-off, y, s, col, scale, x, x+w)
	drawTextClip(dst, x-off+period, y, s, col, scale, x, x+w) // wrapped copy for a seamless loop
}

// drawConnection draws a small signal-bars glyph in the top-right corner: green
// when online, amber + a red slash when offline — connection state, always visible.
func drawConnection(dst *image.RGBA, online bool) {
	fill(dst, image.Rect(Size-20, 1, Size-1, 14), color.RGBA{0, 0, 0, 120}) // contrast chip
	col := color.RGBA{93, 202, 165, 255}
	if !online {
		col = color.RGBA{210, 140, 40, 255}
	}
	for i := 0; i < 3; i++ { // three ascending bars
		h := 3 + i*3
		x := Size - 17 + i*4
		fill(dst, image.Rect(x, 11-h, x+3, 11), col)
	}
	if !online {
		red := color.RGBA{225, 70, 60, 255}
		for i := 0; i < 16; i++ {
			fill(dst, image.Rect(Size-19+i, 12-i*3/4, Size-17+i, 14-i*3/4), red)
		}
	}
}

// drawSpinner draws an 8-dot indeterminate spinner of radius r centered at
// (cx,cy): the head is brightest and the tail fades, rotating with frame.
func drawSpinner(dst *image.RGBA, cx, cy, r, frame int) {
	const n = 8
	dot := 2
	if r < 8 {
		dot = 1
	}
	head := (frame / 2) % n
	for i := 0; i < n; i++ {
		ang := 2 * math.Pi * float64(i) / float64(n)
		dx := int(math.Round(float64(r) * math.Cos(ang)))
		dy := int(math.Round(float64(r) * math.Sin(ang)))
		d := (head - i + n) % n
		b := uint8(60 + (195 * (n - 1 - d) / (n - 1)))
		fill(dst, image.Rect(cx+dx-dot, cy+dy-dot, cx+dx+dot, cy+dy+dot), color.RGBA{b, b, b, 255})
	}
}

// fillCircle paints a filled disc — the success/failure marker on the download card.
func fillCircle(dst *image.RGBA, cx, cy, r int, col color.Color) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				dst.Set(cx+dx, cy+dy, col)
			}
		}
	}
}

// drawDownloadToast draws a slim, non-blocking status strip along the top while a
// download runs (and briefly on completion). Unlike a modal it doesn't dim or
// cover the screen, so you can keep browsing — downloads happen in the background.
func drawDownloadToast(dst *image.RGBA, title, msg string, phase, frame int) {
	const h = 18
	fill(dst, image.Rect(0, 0, Size, h), color.RGBA{16, 16, 22, 236})
	fill(dst, image.Rect(0, h-1, Size, h), modeColor("")) // accent underline
	switch phase {
	case dlRunning:
		drawSpinner(dst, 11, h/2, 5, frame)
		drawText(dst, 22, 3, truncate("Downloading "+title, 30), color.RGBA{235, 235, 242, 255}, 1)
	case dlDone:
		fillCircle(dst, 11, h/2, 5, color.RGBA{93, 202, 165, 255})
		drawText(dst, 22, 3, truncate("Saved: "+title, 31), color.RGBA{150, 220, 180, 255}, 1)
	case dlFail:
		fillCircle(dst, 11, h/2, 5, color.RGBA{225, 90, 70, 255})
		drawText(dst, 22, 3, truncate("Failed: "+msg, 31), color.RGBA{235, 150, 140, 255}, 1)
	}
}

func modeColor(mode string) color.RGBA {
	switch mode {
	case "serve":
		return color.RGBA{55, 138, 221, 255} // blue
	case "party":
		return color.RGBA{160, 110, 230, 255} // purple
	default:
		return color.RGBA{93, 202, 165, 255} // green (standalone)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 3 { // basicfont has no "…" glyph — keep it ASCII to avoid tofu boxes
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}
