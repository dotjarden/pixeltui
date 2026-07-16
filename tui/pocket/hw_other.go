//go:build !linux

package pocket

import "errors"

// The Pirate Audio SPI/GPIO backend is Linux-only (it runs on the Pi). On other
// platforms (e.g. a macOS dev box) use the dev backends via `pixeltui pocket --dev`.
var errNoHardware = errors.New("pocket: hardware backend is Linux-only — run with --dev on this platform")

// NewPiDisplay is unavailable off-Linux.
func NewPiDisplay() (Display, error) { return nil, errNoHardware }

// NewPiButtons is unavailable off-Linux.
func NewPiButtons() (Buttons, error) { return nil, errNoHardware }
