//go:build linux

package pocket

import (
	"fmt"
	"image"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

// Pirate Audio: Headphone Amp (PIM482) wiring — BCM numbering. These are the
// board defaults; if your revision differs, adjust here.
const (
	piSPIPort  = "SPI0.1" // display on SPI0, chip-select CE1
	piDCPin    = "GPIO9"  // data/command select (display is write-only, so MISO is free for this)
	piBLPin    = "GPIO13" // backlight
	piSPISpeed = 60 * physic.MegaHertz
)

// piButtonPins maps the four tactile buttons (active-low) to controls.
var piButtonPins = map[string]Button{
	"GPIO5":  BtnA, // top-left
	"GPIO6":  BtnB, // bottom-left
	"GPIO16": BtnX, // top-right
	"GPIO24": BtnY, // bottom-right
}

// ── ST7789 display over SPI ─────────────────────────────────────────────────
//
// NOTE: This is written to the documented ST7789 240×240 init + the Pirate Audio
// pinout but has NOT been validated on hardware. If the panel is blank, mirrored,
// or wrong-colored, the usual culprits are: SPI speed (try lower), the MADCTL
// rotation byte (0x36), or whether INVON (0x21) is needed — tweak init().

type piDisplay struct {
	port spi.PortCloser
	conn spi.Conn
	dc   gpio.PinIO
	bl   gpio.PinIO
}

// NewPiDisplay opens the ST7789 panel on the Pirate Audio board.
func NewPiDisplay() (Display, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("periph init: %w", err)
	}
	port, err := spireg.Open(piSPIPort)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", piSPIPort, err)
	}
	conn, err := port.Connect(piSPISpeed, spi.Mode0, 8)
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("spi connect: %w", err)
	}
	dc := gpioreg.ByName(piDCPin)
	if dc == nil {
		port.Close()
		return nil, fmt.Errorf("no DC pin %s", piDCPin)
	}
	d := &piDisplay{port: port, conn: conn, dc: dc, bl: gpioreg.ByName(piBLPin)}
	if err := d.init(); err != nil {
		port.Close()
		return nil, err
	}
	return d, nil
}

func (d *piDisplay) command(c byte, data ...byte) error {
	if err := d.dc.Out(gpio.Low); err != nil {
		return err
	}
	if err := d.conn.Tx([]byte{c}, nil); err != nil {
		return err
	}
	if len(data) > 0 {
		if err := d.dc.Out(gpio.High); err != nil {
			return err
		}
		if err := d.conn.Tx(data, nil); err != nil {
			return err
		}
	}
	return nil
}

func (d *piDisplay) init() error {
	type step struct {
		cmd   byte
		data  []byte
		delay time.Duration
	}
	for _, s := range []step{
		{0x01, nil, 150 * time.Millisecond},         // SWRESET
		{0x11, nil, 150 * time.Millisecond},         // SLPOUT
		{0x3A, []byte{0x55}, 10 * time.Millisecond}, // COLMOD: 16-bit/pixel (RGB565)
		{0x36, []byte{0x00}, 0},                     // MADCTL: row/col order (rotation)
		{0x21, nil, 10 * time.Millisecond},          // INVON: 240×240 panels render inverted
		{0x13, nil, 10 * time.Millisecond},          // NORON
		{0x29, nil, 10 * time.Millisecond},          // DISPON
	} {
		if err := d.command(s.cmd, s.data...); err != nil {
			return err
		}
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
	}
	if d.bl != nil {
		d.bl.Out(gpio.High) //nolint:errcheck // backlight on
	}
	return nil
}

func (d *piDisplay) Push(img *image.RGBA) error {
	// Full-screen address window (0..239 on both axes).
	if err := d.command(0x2A, 0x00, 0x00, 0x00, 0xEF); err != nil { // CASET
		return err
	}
	if err := d.command(0x2B, 0x00, 0x00, 0x00, 0xEF); err != nil { // RASET
		return err
	}
	// RAMWR, then the pixels as data.
	if err := d.dc.Out(gpio.Low); err != nil {
		return err
	}
	if err := d.conn.Tx([]byte{0x2C}, nil); err != nil {
		return err
	}
	if err := d.dc.Out(gpio.High); err != nil {
		return err
	}
	px := ToRGB565(img)
	const chunk = 4096 // stay under the kernel SPI bufsiz
	for i := 0; i < len(px); i += chunk {
		end := i + chunk
		if end > len(px) {
			end = len(px)
		}
		if err := d.conn.Tx(px[i:end], nil); err != nil {
			return err
		}
	}
	return nil
}

func (d *piDisplay) Close() error {
	if d.bl != nil {
		d.bl.Out(gpio.Low) //nolint:errcheck
	}
	return d.port.Close()
}

// ── GPIO buttons ──────────────────────────────────────────────────────────────

type piButtons struct {
	ch   chan Press
	stop chan struct{}
}

// NewPiButtons wires the four tactile buttons as active-low inputs with pull-ups.
// Each button gets a watcher goroutine that samples its level to time taps vs. holds.
func NewPiButtons() (Buttons, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("periph init: %w", err)
	}
	b := &piButtons{ch: make(chan Press, 8), stop: make(chan struct{})}
	wired := 0
	for name, btn := range piButtonPins {
		p := gpioreg.ByName(name)
		if p == nil {
			continue
		}
		if err := p.In(gpio.PullUp, gpio.NoEdge); err != nil {
			return nil, fmt.Errorf("config %s: %w", name, err)
		}
		go b.watch(p, btn)
		wired++
	}
	if wired == 0 {
		return nil, fmt.Errorf("no GPIO buttons available")
	}
	return b, nil
}

const (
	// longPress is how long a button must be held to register as a long-press.
	longPress = 350 * time.Millisecond
	// btnPoll is the button level-sampling interval.
	btnPoll = 12 * time.Millisecond
)

// watch turns one button's level into tap / hold presses by polling, not by
// waiting on edges. Edge timing was unreliable here: contact bounce just after a
// press makes WaitForEdge consume a stale edge and read the pin high, so a held
// button reported as a tap (hold-Y never opened the actions menu). Measuring how
// long the pin stays Low is bounce-proof.
func (b *piButtons) watch(p gpio.PinIO, btn Button) {
	stopping := func() bool {
		select {
		case <-b.stop:
			return true
		default:
			return false
		}
	}
	for {
		// Idle until pressed (active-low → Low means down).
		for p.Read() != gpio.Low {
			if stopping() {
				return
			}
			time.Sleep(btnPoll)
		}
		time.Sleep(20 * time.Millisecond) // press debounce
		if p.Read() != gpio.Low {
			continue // glitch that never settled
		}
		// Measure the hold: sample until a debounced release — two consecutive
		// High reads, so a single noisy sample mid-hold isn't misread as an early
		// release (which turned holds into taps) — or until we cross longPress.
		held := 20 * time.Millisecond
		long := false
		highs := 0
		for {
			if p.Read() != gpio.Low {
				highs++
				if highs >= 2 {
					break // ~two polls of High → real release
				}
			} else {
				highs = 0
			}
			if held >= longPress {
				long = true
				break
			}
			time.Sleep(btnPoll)
			held += btnPoll
		}
		b.send(Press{Btn: btn, Long: long})
		// Drain the rest of a hold so one press fires once, then debounce release.
		for p.Read() == gpio.Low {
			if stopping() {
				return
			}
			time.Sleep(btnPoll)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (b *piButtons) send(p Press) {
	select {
	case b.ch <- p:
	default:
	}
}

func (b *piButtons) Events() <-chan Press { return b.ch }

func (b *piButtons) Close() error {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
	return nil
}
