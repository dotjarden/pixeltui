package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// ytdlpAsset returns the yt-dlp release asset for the platform, or "" if
// unsupported. These are the SELF-CONTAINED builds (bundled Python); the bare
// "yt-dlp" zipapp needs a system Python and is deliberately avoided.
func ytdlpAsset(goos, goarch string) string {
	switch goos {
	case "windows":
		return "yt-dlp.exe"
	case "darwin":
		return "yt-dlp_macos" // universal binary
	case "linux":
		switch goarch {
		case "amd64":
			return "yt-dlp_linux"
		case "arm64":
			return "yt-dlp_linux_aarch64"
		}
	}
	return ""
}

// mpvInstallCmd builds the argv to install mpv via a Linux package manager.
// pm is the package manager's own install command. sudo is prepended only when
// we're not root AND sudo is present; when already root (containers) or sudo is
// missing, the package manager runs directly.
func mpvInstallCmd(isRoot, hasSudo bool, pm []string) []string {
	if !isRoot && hasSudo {
		return append([]string{"sudo"}, pm...)
	}
	return pm
}

// binDir is where pixeltui keeps self-contained tool binaries it installs.
func binDir(dataDir string) string {
	return filepath.Join(dataDir, "bin")
}

// downloadBinary streams url into dest and makes it executable. It writes to a
// temp file in dest's directory and renames atomically, so a failed download
// never leaves a half-written binary behind.
func downloadBinary(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".dl-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, err = io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return err
	}
	if runtime.GOOS != "windows" {
		os.Chmod(tmpPath, 0755) //nolint:errcheck
	}
	return os.Rename(tmpPath, dest)
}
