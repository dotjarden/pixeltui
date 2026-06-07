//go:build windows

package tui

import (
	"fmt"
	"io"
	"os"
	"time"
)

// On Windows mpv's IPC endpoint is a named pipe (\\.\pipe\NAME), not a Unix
// socket. A named pipe is opened like a file, and *os.File satisfies
// io.ReadWriteCloser, so the shared ipcRound logic works unchanged.

func mpvSocketPath() string {
	return fmt.Sprintf(`\\.\pipe\pixeltui-%d`, time.Now().UnixNano())
}

func dialIPC(addr string) (io.ReadWriteCloser, error) {
	// mpv creates the pipe server; we open the client end for read/write.
	return os.OpenFile(addr, os.O_RDWR, 0)
}

func ipcReady(addr string) bool {
	// A named pipe can't be stat'd like a file; probe by opening it.
	f, err := os.OpenFile(addr, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close() //nolint:errcheck
	return true
}

func removeIPC(string) {
	// Named pipes are released automatically when mpv exits; nothing to remove.
}
