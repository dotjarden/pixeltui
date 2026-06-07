package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// notifyNowPlaying sends a best-effort OS notification for the current track.
// Errors are silently discarded — the notification is cosmetic, never critical.
//
//   darwin  → osascript: appears in Notification Centre and on the Lock Screen
//   linux   → notify-send: shown by the desktop notification daemon (if present)
//   windows → no-op (PowerShell toast is too slow/modal for per-track use)
func notifyNowPlaying(artist, track string) {
	body := track + " — " + artist
	switch runtime.GOOS {
	case "darwin":
		// display notification shows in Notification Centre and,
		// on macOS 13+, on the Lock Screen widget strip.
		script := fmt.Sprintf(
			`display notification %q with title "pixeltui ▶" sound name ""`,
			body,
		)
		exec.Command("osascript", "-e", script).Run() //nolint:errcheck

	case "linux":
		// notify-send is available on GNOME, KDE, XFCE, etc.
		// -a sets the application name; -t sets expire timeout (ms).
		exec.Command("notify-send",
			"-a", "pixeltui",
			"-t", "4000",
			"pixeltui ▶  "+track,
			artist,
		).Run() //nolint:errcheck
	}
}
