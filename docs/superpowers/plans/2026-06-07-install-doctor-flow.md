# Robust cross-platform `doctor` + install flow — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `pixeltui doctor` check every dependency and `doctor --fix` install yt-dlp + mpv on a bare machine (no sudo, no Python, no package manager assumed), wire a clean opt-out into the installers, and update all docs.

**Architecture:** Add a small, unit-tested `tui/deps.go` holding the pure provisioning logic (`ytdlpAsset`, `mpvInstallCmd`) and a `downloadBinary` helper. Rewrite `fixYtdlp` (standalone binary download) and the Linux branch of `fixMPV` (no forced sudo) to use them. Teach both yt-dlp resolvers about `~/.pixeltui/bin`. Update install scripts + README + usage.

**Tech Stack:** Go 1.25 (`package main` in `tui/`), POSIX `sh`, PowerShell, Markdown. Build/test with the local SDK at `C:\Users\magic\go-sdk\go\bin\go.exe`.

**Conventions for every task below:**
- `GO` = `C:\Users\magic\go-sdk\go\bin\go.exe`
- Run all `go`/`git` commands from the repo root `C:\Users\magic\Documents\TUI\pixeltui`.
- Work is on branch `install-doctor-robustness` (already created).

---

### Task 1: `ytdlpAsset` pure function

**Files:**
- Create: `tui/deps.go`
- Create: `tui/deps_test.go`

- [ ] **Step 1: Write the failing test** — create `tui/deps_test.go`:

```go
package main

import "testing"

func TestYtdlpAsset(t *testing.T) {
	cases := []struct{ os, arch, want string }{
		{"windows", "amd64", "yt-dlp.exe"},
		{"darwin", "amd64", "yt-dlp_macos"},
		{"darwin", "arm64", "yt-dlp_macos"},
		{"linux", "amd64", "yt-dlp_linux"},
		{"linux", "arm64", "yt-dlp_linux_aarch64"},
		{"linux", "386", ""},
		{"plan9", "amd64", ""},
	}
	for _, c := range cases {
		if got := ytdlpAsset(c.os, c.arch); got != c.want {
			t.Errorf("ytdlpAsset(%q,%q) = %q, want %q", c.os, c.arch, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -run TestYtdlpAsset -v`
Expected: FAIL — `undefined: ytdlpAsset`.

- [ ] **Step 3: Write minimal implementation** — create `tui/deps.go`:

```go
package main

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -run TestYtdlpAsset -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/deps.go tui/deps_test.go
git commit -m "feat(deps): ytdlpAsset platform→asset mapping

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `mpvInstallCmd` pure function

**Files:**
- Modify: `tui/deps.go`
- Modify: `tui/deps_test.go`

- [ ] **Step 1: Write the failing test** — append to `tui/deps_test.go`:

```go
func TestMpvInstallCmd(t *testing.T) {
	pm := []string{"apt-get", "install", "-y", "mpv"}
	cases := []struct {
		name            string
		isRoot, hasSudo bool
		want            []string
	}{
		{"root no sudo", true, false, []string{"apt-get", "install", "-y", "mpv"}},
		{"root with sudo", true, true, []string{"apt-get", "install", "-y", "mpv"}},
		{"user with sudo", false, true, []string{"sudo", "apt-get", "install", "-y", "mpv"}},
		{"user no sudo", false, false, []string{"apt-get", "install", "-y", "mpv"}},
	}
	for _, c := range cases {
		got := mpvInstallCmd(c.isRoot, c.hasSudo, pm)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: argv[%d] = %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -run TestMpvInstallCmd -v`
Expected: FAIL — `undefined: mpvInstallCmd`.

- [ ] **Step 3: Write minimal implementation** — append to `tui/deps.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -run TestMpvInstallCmd -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/deps.go tui/deps_test.go
git commit -m "feat(deps): mpvInstallCmd no-forced-sudo argv builder

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `binDir` + `downloadBinary` helper

**Files:**
- Modify: `tui/deps.go`
- Modify: `tui/deps_test.go`

- [ ] **Step 1: Write the failing test** — append to `tui/deps_test.go`:

```go
func TestDownloadBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("#!/bin/sh\necho hi\n"))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "sub", "yt-dlp")
	if err := downloadBinary(srv.URL, dest); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "#!/bin/sh\necho hi\n" {
		t.Errorf("content = %q", string(b))
	}
}

func TestDownloadBinaryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "yt-dlp")
	if err := downloadBinary(srv.URL, dest); err == nil {
		t.Error("expected error on HTTP 404, got nil")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("dest should not exist after failed download")
	}
}

func TestBinDir(t *testing.T) {
	if got := binDir("/home/u/.pixeltui"); got != filepath.Join("/home/u/.pixeltui", "bin") {
		t.Errorf("binDir = %q", got)
	}
}
```

Then update the import block at the top of `tui/deps_test.go` to:

```go
import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -run "TestDownloadBinary|TestBinDir" -v`
Expected: FAIL — `undefined: downloadBinary` / `undefined: binDir`.

- [ ] **Step 3: Write minimal implementation** — add an import block at the top of `tui/deps.go` (just under `package main`) and append the funcs:

```go
import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)
```

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -v`
Expected: PASS (all deps tests).

- [ ] **Step 5: Commit**

```bash
git add tui/deps.go tui/deps_test.go
git commit -m "feat(deps): binDir + atomic downloadBinary helper

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Rewrite `fixYtdlp` to install the standalone binary

**Files:**
- Modify: `tui/main.go:1005-1033` (the whole `fixYtdlp` function)

Note: `toolVersion(bin, "--version")` (main.go:1297) already returns `"?"` when a binary won't run — reuse it to verify.

- [ ] **Step 1: Replace the function body.** Replace the existing `fixYtdlp` (the comment line `// fixYtdlp creates/repairs…` through its closing `}`) with:

```go
// fixYtdlp installs the self-contained standalone yt-dlp binary into
// ~/.pixeltui/bin — no Python, pip, or venv. Existing pip venvs are still
// discovered by preferredYtdlp for back-compat; this is the universal path.
func fixYtdlp(dir string) bool {
	asset := ytdlpAsset(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		fmt.Println("    no prebuilt yt-dlp for this platform — install yt-dlp manually")
		return false
	}
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	dest := filepath.Join(binDir(dir), name)
	url := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + asset
	fmt.Println("    downloading yt-dlp (self-contained)…")
	if err := downloadBinary(url, dest); err != nil {
		fmt.Println("    download failed:", err)
		return false
	}
	if toolVersion(dest, "--version") == "?" {
		fmt.Println("    installed but won't run:", dest)
		return false
	}
	fmt.Println("    yt-dlp installed → ~/.pixeltui/bin")
	return true
}
```

- [ ] **Step 2: Verify it builds**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" build ./tui/`
Expected: no output (success). (The `os/exec` import stays used elsewhere; no import changes needed.)

- [ ] **Step 3: Commit**

```bash
git add tui/main.go
git commit -m "feat(doctor): install self-contained yt-dlp binary (no Python)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Rewrite the Linux branch of `fixMPV` (no forced sudo)

**Files:**
- Modify: `tui/main.go:1066-1079` (the `case "linux":` block inside `fixMPV`)

- [ ] **Step 1: Replace the Linux case.** Replace from `case "linux":` through its `return false` (the four lines of package-manager loop + the "no known package manager" message) with:

```go
	case "linux":
		pms := [][]string{
			{"apt-get", "install", "-y", "mpv"}, {"dnf", "install", "-y", "mpv"},
			{"pacman", "-S", "--noconfirm", "mpv"}, {"zypper", "install", "-y", "mpv"},
		}
		isRoot := os.Geteuid() == 0
		hasSudo := hasBin("sudo")
		for _, pm := range pms {
			if !hasBin(pm[0]) {
				continue
			}
			// apt's package list may be stale in minimal images; refresh first.
			if pm[0] == "apt-get" {
				upd := mpvInstallCmd(isRoot, hasSudo, []string{"apt-get", "update"})
				u := exec.Command(upd[0], upd[1:]...)
				u.Stdin, u.Stdout, u.Stderr = os.Stdin, os.Stdout, os.Stderr
				u.Run() //nolint:errcheck
			}
			argv := mpvInstallCmd(isRoot, hasSudo, pm)
			fmt.Printf("    installing mpv via %s…\n", pm[0])
			c := exec.Command(argv[0], argv[1:]...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if c.Run() == nil && hasBin("mpv") {
				return true
			}
		}
		fmt.Println("    couldn't auto-install mpv — install via your package manager (apt/dnf/pacman/zypper)")
		return false
```

- [ ] **Step 2: Verify it builds**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" build ./tui/`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add tui/main.go
git commit -m "fix(doctor): install mpv on Linux without forced sudo

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Teach both yt-dlp resolvers about `~/.pixeltui/bin`

**Files:**
- Modify: `tui/main.go:1280-1290` (`preferredYtdlp`)
- Modify: `tui/tui/player.go:400-410` (`ytdlpPath`)

- [ ] **Step 1: Update `preferredYtdlp`.** In `tui/main.go`, inside `preferredYtdlp`, immediately AFTER the `for _, c := range []string{ ... }` venv loop's closing `}` and BEFORE the `if p, err := exec.LookPath("yt-dlp")` line, insert:

```go
		bin := filepath.Join(home, ".pixeltui", "bin", "yt-dlp")
		if runtime.GOOS == "windows" {
			bin = filepath.Join(home, ".pixeltui", "bin", "yt-dlp.exe")
		}
		if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
			return bin
		}
```

(This block is inside the existing `if home, err := os.UserHomeDir(); err == nil {` scope, so `home` is in scope.)

- [ ] **Step 2: Update `ytdlpPath`.** In `tui/tui/player.go`, inside `ytdlpPath`, after the venv `for _, cand := range []string{ ... }` loop's closing `}` and before the `if p, err := exec.LookPath("yt-dlp")` line, insert the same block:

```go
		bin := filepath.Join(home, ".pixeltui", "bin", "yt-dlp")
		if runtime.GOOS == "windows" {
			bin = filepath.Join(home, ".pixeltui", "bin", "yt-dlp.exe")
		}
		if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
			return bin
		}
```

`player.go` already imports `runtime`, `os`, `path/filepath` (verify the import block contains them; it does as of this writing).

- [ ] **Step 3: Verify it builds**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" build ./tui/`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add tui/main.go tui/tui/player.go
git commit -m "feat(deps): resolve yt-dlp from ~/.pixeltui/bin

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Fix the Linux yt-dlp install hint

**Files:**
- Modify: `tui/tui/player.go:836-838` (the `case "linux":` arm of `ytdlpInstall`)

The current Linux hint tells users to `curl` the generic `yt-dlp` asset, which needs system Python. Point them at `doctor --fix` instead.

- [ ] **Step 1: Replace the Linux arm.** In `ytdlpInstall`, replace the `case "linux":` line and its `return "  curl -fsSL …yt-dlp …"` line with:

```go
	case "linux":
		return "  pixeltui doctor --fix   (installs a self-contained yt-dlp into ~/.pixeltui/bin)"
```

- [ ] **Step 2: Verify it builds**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" build ./tui/`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add tui/tui/player.go
git commit -m "docs(player): correct Linux yt-dlp install hint

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Make the doctor mpv check verify it runs

**Files:**
- Modify: `tui/main.go:911-915` (the mpv row in `cmdDoctor`)

- [ ] **Step 1: Replace the mpv row.** Replace:

```go
	if mpvBin() != "" {
		ok("mpv", "pause/seek/volume + OS Now Playing")
	} else {
		warn("mpv", "missing — audio plays via ffplay/afplay but no controls. Fix: pixeltui doctor --fix")
	}
```

with:

```go
	switch mb := mpvBin(); {
	case mb != "" && toolVersion(mb, "--version") != "?":
		ok("mpv", "pause/seek/volume + OS Now Playing")
	case mb != "":
		bad("mpv", "found but won't run — Fix: pixeltui doctor --fix  ("+mb+")")
	default:
		warn("mpv", "missing — audio plays via ffplay/afplay but no controls. Fix: pixeltui doctor --fix")
	}
```

- [ ] **Step 2: Verify it builds**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" build ./tui/`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add tui/main.go
git commit -m "feat(doctor): verify mpv actually runs, not just present

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Installer flow — `install.sh`

**Files:**
- Modify: `install.sh:17-19` (header env-override comment)
- Modify: `install.sh:125-130` (dependency block)
- Modify: `install.sh:132-137` (done message)

- [ ] **Step 1: Update the header comment.** Replace lines 17-19:

```sh
# Env overrides:
#   PREFIX=/path        install location
#   PIXELTUI_NO_DEPS=1  skip the yt-dlp/mpv auto-install step
```

with:

```sh
# Flags / env overrides:
#   PREFIX=/path           install location
#   --nodoctor             skip the auto doctor --fix (also: PIXELTUI_NO_DOCTOR=1)
#   PIXELTUI_NO_DEPS=1     back-compat alias for --nodoctor
```

- [ ] **Step 2: Replace the dependency block.** Replace lines 125-130 (`# ── playback dependencies …` through the closing `fi`) with:

```sh
# ── playback dependencies (yt-dlp + mpv) ─────────────────────────────────────────
skip_doctor=0
[ "$PIXELTUI_NO_DOCTOR" = "1" ] && skip_doctor=1
[ "$PIXELTUI_NO_DEPS" = "1" ] && skip_doctor=1
for a in "$@"; do [ "$a" = "--nodoctor" ] && skip_doctor=1; done

if [ "$skip_doctor" != "1" ]; then
  step "Checking & installing playback dependencies"
  info "Running 'pixeltui doctor --fix' (yt-dlp + mpv) ..."
  "$PREFIX/pixeltui" doctor --fix || warn "Some deps couldn't auto-install — run 'pixeltui doctor' to see what's left."
fi
```

- [ ] **Step 3: Update the done message.** Replace lines 132-137 (`# ── done …` through the final `printf … new terminal …`) with:

```sh
# ── done ─────────────────────────────────────────────────────────────────────────
printf "\n${GREEN}${BOLD}  ✓ launch-ready${RESET}\n\n"
printf "  Next (optional):\n"
printf "    ${BOLD}pixeltui setup${RESET}    add Last.fm key, Subsonic, folders\n"
printf "    ${BOLD}pixeltui${RESET}          launch the player\n\n"
printf "  ${DIM}(open a new terminal first if 'pixeltui' isn't found)${RESET}\n\n"
```

- [ ] **Step 4: Syntax-check the script**

Run: `bash -n install.sh`
Expected: no output (valid).

- [ ] **Step 5: Commit**

```bash
git add install.sh
git commit -m "feat(install.sh): gated single doctor --fix pass + --nodoctor

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Installer flow — `install.ps1`

**Files:**
- Modify: `install.ps1:1-20` (add `param` + document flag)
- Modify: `install.ps1:82-87` (dependency block)
- Modify: `install.ps1:89-95` (done message)

- [ ] **Step 1: Add a `param` block + document the flag.** The very first executable statement in a PowerShell script must be `param(...)`. Immediately AFTER the leading comment header block (the `#  $env:PIXELTUI_NO_DEPS = "1"   skip …` line) and BEFORE `$ErrorActionPreference = "Stop"`, insert:

```powershell
param([switch]$NoDoctor)
```

Also update the comment header's env-override section (the lines documenting `$env:PIXELTUI_NO_DEPS`) to:

```powershell
#   $env:PREFIX                  install location
#   -NoDoctor                    skip the auto doctor --fix
#   $env:PIXELTUI_NO_DOCTOR = "1"  same, via env (back-compat: PIXELTUI_NO_DEPS)
```

- [ ] **Step 2: Replace the dependency block.** Replace lines 82-87 (`# ── playback dependencies …` through its closing `}`) with:

```powershell
# ── playback dependencies ─────────────────────────────────────────────────────────
$skipDoctor = $NoDoctor -or ($env:PIXELTUI_NO_DOCTOR -eq "1") -or ($env:PIXELTUI_NO_DEPS -eq "1")
if (-not $skipDoctor) {
    Step "Checking & installing playback dependencies"
    Info "Running 'pixeltui doctor --fix' (yt-dlp + mpv) ..."
    try { & $exe doctor --fix } catch { Warn "Some deps couldn't auto-install — run 'pixeltui doctor'." }
}
```

- [ ] **Step 3: Update the done message.** Replace lines 89-95 (`# ── done …` through the `pixeltui          launch the player` line) with:

```powershell
# ── done ──────────────────────────────────────────────────────────────────────────
Write-Host "`n  ✓ launch-ready" -ForegroundColor Green
Write-Host ""
Write-Host "  Next (optional):"
Write-Host "    pixeltui setup    add Last.fm key, Subsonic, folders" -ForegroundColor White
Write-Host "    pixeltui          launch the player" -ForegroundColor White
```

- [ ] **Step 4: Syntax-check the script**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" version > $null; powershell -NoProfile -Command "$null = [System.Management.Automation.PSParser]::Tokenize((Get-Content -Raw install.ps1), [ref]$null); 'ps1 ok'"`
Expected: prints `ps1 ok` (no parse errors).

- [ ] **Step 5: Commit**

```bash
git add install.ps1
git commit -m "feat(install.ps1): gated single doctor --fix pass + -NoDoctor

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Documentation — README + usage

**Files:**
- Modify: `README.md:55-68` (install one-liners — add opt-out note)
- Modify: `README.md:194-195` (doctor --fix description)
- Modify: `README.md:271-278` (data-directory listing)
- Modify: `tui/main.go:1353` (`printUsage` doctor line)

- [ ] **Step 1: Add the opt-out note under the install one-liners.** In `README.md`, immediately AFTER the macOS/Linux blurb (the paragraph ending `…installs the playback dependencies (yt-dlp, mpv). No Go needed.`) and the Windows one-liner block, add a new paragraph:

```markdown
Skip the automatic dependency check/install with `--nodoctor`
(`curl … | sh -s -- --nodoctor`) or `PIXELTUI_NO_DOCTOR=1`. You can always run
`pixeltui doctor --fix` later.
```

- [ ] **Step 2: Correct the `doctor --fix` description.** Replace README lines 194-195:

```markdown
`doctor --fix` self-resolves the keystone dependencies: it installs a fast pip
**yt-dlp** and, on macOS, a self-contained **mpv** bundle (package manager on Linux).
```

with:

```markdown
`doctor --fix` self-resolves the keystone dependencies: it installs a
self-contained **yt-dlp** binary into `~/.pixeltui/bin` (no Python needed) and
**mpv** — a bundle on macOS, the standalone build on Windows, your package
manager on Linux.
```

- [ ] **Step 3: Add `bin/` to the data-directory listing.** In the `~/.pixeltui/` code block (README lines 271-278), add this line after the `library/` line:

```
bin/             self-contained tools pixeltui installed (yt-dlp)
```

- [ ] **Step 4: Keep `printUsage` accurate.** Confirm `tui/main.go:1353` reads `pixeltui doctor [--fix]           check setup; --fix auto-resolves what it can`. No change needed unless wording drifted; if so, leave as-is (it is already correct).

- [ ] **Step 5: Commit**

```bash
git add README.md tui/main.go
git commit -m "docs: document --nodoctor + self-contained yt-dlp install

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Full verification + release v0.3.3

**Files:** none (verification + rollout)

- [ ] **Step 1: Run the full test suite**

Run: `& "C:\Users\magic\go-sdk\go\bin\go.exe" test ./tui/ -v`
Expected: all PASS (TestYtdlpAsset, TestMpvInstallCmd, TestDownloadBinary, TestDownloadBinaryHTTPError, TestBinDir).

- [ ] **Step 2: Cross-compile all release targets** (proves every platform still builds)

Run:
```powershell
$GO="C:\Users\magic\go-sdk\go\bin\go.exe"; $env:CGO_ENABLED="0"
foreach($p in @("darwin/amd64","darwin/arm64","linux/amd64","linux/arm64","windows/amd64")){
  $o=$p.Split("/"); $env:GOOS=$o[0]; $env:GOARCH=$o[1]
  & $GO build -ldflags="-s -w" -trimpath -o ("dist/pixeltui-{0}-{1}{2}" -f $o[0],$o[1],$(if($o[0] -eq "windows"){".exe"}else{""})) ./tui
  Write-Host "$p ->" $LASTEXITCODE
}
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```
Expected: each line ends `-> 0`.

- [ ] **Step 3: Manual end-to-end in a clean container** (user runs; the real acceptance test)

```bash
docker run --rm -it ubuntu:latest bash -lc '
  apt-get update -qq && apt-get install -y -qq curl >/dev/null
  curl -fsSL https://raw.githubusercontent.com/dotjarden/pixeltui/main/install.sh | sh
  pixeltui doctor'
```
Expected after the release is live: yt-dlp ✓ (self-contained) and mpv ✓ in the doctor table. Also verify `… | sh -s -- --nodoctor` skips the dependency phase.

- [ ] **Step 4: Merge the branch to main**

```bash
git checkout main
git merge --no-ff install-doctor-robustness -m "Robust cross-platform doctor + install flow"
git push origin main
```

- [ ] **Step 5: Cut and publish v0.3.3** (mirrors the v0.3.2 process)

```powershell
# regenerate SHA256SUMS over dist/ with bare basenames, then:
gh release create v0.3.3 --repo dotjarden/pixeltui --target main `
  --title "pixeltui v0.3.3" --notes-file <notes> (Get-ChildItem dist -File).FullName
```
Then verify `gh api repos/dotjarden/pixeltui/releases/latest --jq .tag_name` returns `v0.3.3` and the asset URLs return HTTP 200.

---

## Notes for the implementer

- **`os.Geteuid()`** compiles on all platforms (returns `-1` on Windows); it's only reached in the Linux branch, so the `-1` value is harmless.
- **Back-compat:** existing users with a `ytdlp-venv` keep working — `preferredYtdlp`/`ytdlpPath` still check the venv first; the new `~/.pixeltui/bin` path is an additional fallback.
- **Don't** reintroduce the pip-venv install path in `fixYtdlp` — standalone binary only, by design.
- **Deferred from the spec:** the spec mentions a *best-effort* ffmpeg install via the Linux package manager. This plan keeps ffmpeg/ffplay/ffprobe as **check + hint only** (the existing doctor rows) and does not auto-install them, since ffplay is only a fallback once mpv is present (YAGNI). If wanted later, it's a one-line `mpvInstallCmd`-style call in a new `fixFfmpeg` — out of scope here.
