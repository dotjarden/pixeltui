package pocket

import (
	"image"
	"image/color"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"

	qrcode "github.com/skip2/go-qrcode"
)

// renderList draws a menu/list screen: centered title, the visible rows with the
// cursor highlighted, a now-playing footer, and corner button hints.
func renderList(s *screen, npLine string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	fill(img, img.Bounds(), color.RGBA{18, 18, 22, 255})

	drawTextCenter(img, Size/2, 5, truncate(s.title, 16), color.RGBA{240, 240, 245, 255}, 2)
	fill(img, image.Rect(10, 26, Size-10, 27), color.RGBA{50, 50, 62, 255})

	const top0, rowH = 34, 24
	for i := 0; i < listVisible; i++ {
		idx := s.top + i
		if idx >= len(s.items) {
			break
		}
		y := top0 + i*rowH
		txt := color.RGBA{205, 205, 216, 255}
		if idx == s.cursor {
			fill(img, image.Rect(4, y-3, Size-4, y+rowH-5), color.RGBA{38, 52, 46, 255})
			fill(img, image.Rect(4, y-3, 7, y+rowH-5), modeColor(""))
			txt = color.RGBA{240, 255, 248, 255}
		}
		drawText(img, 12, y, truncate(s.items[idx].label, 16), txt, 2)
	}

	// Lit bottom bar — the bottom edge always reads as 'on', showing the
	// now-playing line (or a resting hint) above the button labels.
	fill(img, image.Rect(0, Size-30, Size, Size), color.RGBA{24, 26, 32, 255})
	fill(img, image.Rect(0, Size-30, Size, Size-29), modeColor(""))
	if npLine != "" {
		drawText(img, 8, Size-25, truncate(npLine, 28), color.RGBA{150, 200, 170, 255}, 1)
	} else {
		drawText(img, 8, Size-25, "nothing playing", color.RGBA{110, 110, 122, 255}, 1)
	}
	drawCorners(img, "up", "down", "back", "sel")
	return img
}

// renderVolume draws the volume screen: a big bar + percentage.
func renderVolume(vol int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	fill(img, img.Bounds(), color.RGBA{18, 18, 22, 255})
	drawTextCenter(img, Size/2, 6, "Volume", color.RGBA{240, 240, 245, 255}, 2)

	barY, barH := Size/2-22, 44
	fill(img, image.Rect(24, barY, Size-24, barY+barH), color.RGBA{45, 45, 56, 255})
	w := (Size - 48) * vol / 100
	fill(img, image.Rect(24, barY, 24+w, barY+barH), modeColor(""))
	drawTextCenter(img, Size/2, barY+barH+14, strconv.Itoa(vol)+"%", color.RGBA{240, 240, 245, 255}, 2)

	drawCorners(img, "+", "-", "back", "back")
	return img
}

// renderNowPlaying composes the now-playing body with a volume readout (hold
// A/B on this screen adjusts it) and the transport button hints.
func renderNowPlaying(np View, vol int) *image.RGBA {
	img := Render(np)
	drawTextCenter(img, Size/2, 4, "vol "+strconv.Itoa(vol)+"%", color.RGBA{135, 135, 150, 255}, 1)
	drawCorners(img, "play", "next", "prev", "menu")
	return img
}

// drawCorners labels each physical button in the matching screen corner:
// A top-left, B bottom-left, X top-right, Y bottom-right.
func drawCorners(img *image.RGBA, a, b, x, y string) {
	col := color.RGBA{120, 120, 138, 255}
	drawText(img, 3, 3, a, col, 1)
	drawTextRight(img, Size-22, 3, x, col, 1) // top-right hint sits left of the connection icon
	drawText(img, 3, Size-10, b, col, 1)
	drawTextRight(img, Size-3, Size-10, y, col, 1)
}

// renderQR draws a join QR on a white background (for scan contrast) with a
// caption — the pocket's "scan to join the party" screen.
func renderQR(content, caption string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	fill(img, img.Bounds(), color.RGBA{255, 255, 255, 255})
	if q, err := qrcode.New(content, qrcode.Medium); err == nil {
		bm := q.Bitmap()
		if n := len(bm); n > 0 {
			scale := 196 / n
			if scale < 1 {
				scale = 1
			}
			dim := scale * n
			ox, oy := (Size-dim)/2, 6
			black := color.RGBA{0, 0, 0, 255}
			for y := 0; y < n; y++ {
				for x := 0; x < n; x++ {
					if bm[y][x] {
						fill(img, image.Rect(ox+x*scale, oy+y*scale, ox+(x+1)*scale, oy+(y+1)*scale), black)
					}
				}
			}
		}
	}
	drawTextCenter(img, Size/2, Size-30, truncate(caption, 30), color.RGBA{20, 20, 24, 255}, 1)
	drawTextCenter(img, Size/2, Size-16, "X = back", color.RGBA{90, 90, 100, 255}, 1)
	return img
}

func textWidth(s string, scale int) int {
	d := &font.Drawer{Face: basicfont.Face7x13}
	return d.MeasureString(s).Ceil() * scale
}

func drawTextCenter(dst *image.RGBA, cx, y int, s string, col color.Color, scale int) {
	drawText(dst, cx-textWidth(s, scale)/2, y, s, col, scale)
}

func drawTextRight(dst *image.RGBA, rx, y int, s string, col color.Color, scale int) {
	drawText(dst, rx-textWidth(s, scale), y, s, col, scale)
}
