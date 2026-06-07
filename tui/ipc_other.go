//go:build !windows

package tui

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// On macOS/Linux mpv's IPC endpoint is a Unix-domain socket. We place it under
// the OS temp dir (honours $TMPDIR) rather than a hardcoded /tmp.

func mpvSocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("pixeltui-%d.sock", time.Now().UnixNano()))
}

func dialIPC(addr string) (io.ReadWriteCloser, error) {
	return net.DialTimeout("unix", addr, 200*time.Millisecond)
}

func ipcReady(addr string) bool {
	_, err := os.Stat(addr)
	return err == nil
}

func removeIPC(addr string) {
	os.Remove(addr) //nolint:errcheck
}
