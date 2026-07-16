package pocket

import (
	"image"
	"image/png"
	"os"
)

// devDisplay writes each frame to a PNG so you can watch the pocket screen on a
// laptop (open it with an auto-reloading image viewer). No hardware required.
type devDisplay struct{ path string }

// NewDevDisplay returns a Display that writes frames to a PNG at path.
func NewDevDisplay(path string) Display { return &devDisplay{path: path} }

func (d *devDisplay) Push(img *image.RGBA) error {
	f, err := os.CreateTemp(filepathDir(d.path), ".pocket-*")
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	f.Close()
	return os.Rename(f.Name(), d.path) // atomic swap so a viewer never sees a half-written file
}

func (d *devDisplay) Close() error { return nil }

// devButtons maps stdin keypresses to the four buttons (context-sensitive — in
// lists A/B are up/down, X is back, Y is select). Lowercase = tap, UPPERCASE =
// long-press (hold): on Now Playing, hold A/B = volume up/down.
//
//	a/w → A   b/s → B   x → X   y/enter → Y     (uppercase = hold)
//
// (Run the terminal in raw mode for single-key input; line-buffered also works.)
type devButtons struct {
	ch   chan Press
	stop chan struct{}
}

// NewDevButtons returns a Buttons driven by stdin keypresses.
func NewDevButtons() Buttons {
	b := &devButtons{ch: make(chan Press, 4), stop: make(chan struct{})}
	go b.read()
	return b
}

func (b *devButtons) read() {
	buf := make([]byte, 1)
	for {
		select {
		case <-b.stop:
			return
		default:
		}
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		var btn Button
		long := false
		switch buf[0] {
		case 'a', 'w':
			btn = BtnA
		case 'A', 'W':
			btn, long = BtnA, true
		case 'b', 's':
			btn = BtnB
		case 'B', 'S':
			btn, long = BtnB, true
		case 'x':
			btn = BtnX
		case 'X':
			btn, long = BtnX, true
		case 'y', '\r', '\n':
			btn = BtnY
		case 'Y':
			btn, long = BtnY, true
		default:
			continue
		}
		b.send(Press{Btn: btn, Long: long})
	}
}

func (b *devButtons) send(p Press) {
	select {
	case b.ch <- p:
	default:
	}
}

func (b *devButtons) Events() <-chan Press { return b.ch }

func (b *devButtons) Close() error {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
	return nil
}

// filepathDir returns the directory of path (so the temp file lands on the same
// filesystem for an atomic rename). Avoids importing path/filepath for one call.
func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}
