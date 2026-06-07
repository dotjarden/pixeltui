# Robust cross-platform `doctor` + improved install flow

**Date:** 2026-06-07
**Status:** Approved (design)
**Scope:** `tui/main.go` (doctor/fix/setup, resolvers, usage), `install.sh`, `install.ps1`, `README.md`, `tui/tui/player.go` (yt-dlp resolver + install hint)

## Problem

On a clean machine (verified in a fresh `ubuntu:latest` container) the install flow runs but `pixeltui doctor --fix` fails to install the playback dependencies:

1. **mpv (Linux)** — `fixMPV` unconditionally shells out through `sudo` (`main.go:1073`). A minimal container has no `sudo` and runs as root, so `exec.Command("sudo", …)` fails instantly with a Go "executable not found" error (no subprocess output) and mpv is never installed.
2. **yt-dlp** — `fixYtdlp` only knows how to build a pip venv, which requires `python3` (`main.go:1007-1017`). With no Python present it gives up; there is no fallback to the self-contained yt-dlp binary.

Separately, the install flow and docs should be tightened: auto-run the dependency phase, give a clear opt-out, and document the `curl`/`irm` one-liners.

## Goals

- `doctor` **checks** every dependency assuming nothing is pre-installed, and verifies each tool actually *runs* (not just exists on PATH).
- `doctor --fix` **installs** what's missing using self-contained downloads first, with no assumptions about `sudo`, Python, or a package manager — falling back to a package manager only where no static binary exists (mpv on Linux).
- The installer auto-runs the dependency phase with a clean opt-out, and ends "launch-ready."
- All user-facing docs reflect the flow.

## Non-goals

- Auto-installing ffmpeg/ffplay/ffprobe on every OS (check + hint only; best-effort PM install on Linux).
- A pip-venv fast path for yt-dlp (YAGNI — standalone binary only; existing venvs are still *discovered* for back-compat).
- Auto-running interactive `setup` (it needs a TTY; stays a suggested post-install step).

## Decisions (from brainstorming)

1. **Setup placement:** post-install, suggested — never auto-run.
2. **Fix philosophy:** self-contained downloads into `~/.pixeltui` first; package-manager fallback only for Linux mpv.
3. **Opt-out:** `--nodoctor` script flag **and** `PIXELTUI_NO_DOCTOR=1` env var; `PIXELTUI_NO_DEPS=1` kept as a back-compat alias.
4. **Fix scope:** yt-dlp + mpv only. ffmpeg/ffplay/ffprobe = check + hint (+ best-effort Linux PM).
5. **Install dependency phase:** a single `doctor --fix` pass (checks → installs missing → re-checks → prints final table). No separate `doctor` call.

## Design

### Install flow (`install.sh`, `install.ps1`)

```
download binary → verify checksum → install to PATH
  → doctor --fix          (skipped if --nodoctor / PIXELTUI_NO_DOCTOR / PIXELTUI_NO_DEPS)
  → ✓ launch-ready
     Next (optional):  pixeltui setup    (Last.fm / Subsonic / folders)
                       pixeltui          (launch)
```

- `install.sh`: accept `--nodoctor` as a script arg (`sh -s -- --nodoctor`) and honor `PIXELTUI_NO_DOCTOR` / `PIXELTUI_NO_DEPS` env vars. Replace the current `doctor --fix`-only block with the gated single pass and the launch-ready message.
- `install.ps1`: accept `-NoDoctor` switch and `$env:PIXELTUI_NO_DOCTOR` / `$env:PIXELTUI_NO_DEPS`. Same message.

### `doctor` — check half (`cmdDoctor`)

For each dependency: resolve it (PATH + `~/.pixeltui/bin` + existing venv/bundle), **run `--version`**, and report present-and-runnable vs missing vs found-but-won't-run. The table layout is unchanged; rows now reflect runnability. The same check function is reused after `--fix` so the final table is honest.

### `doctor --fix` — fix half

Fixers are **guarded** (skip when the check already passes) and **verified** (re-run `--version` after install, return success only if it runs).

| Dep | Strategy |
|---|---|
| **yt-dlp** | Download the self-contained standalone binary into `~/.pixeltui/bin/`, `chmod +x` (POSIX), verify. Asset by os/arch (see below). No Python/pip/venv. |
| **mpv** | mac → existing bundle download; win → existing shinchiro standalone; **linux → package manager without forced `sudo`** (see command builder). |
| **ffmpeg/ffplay/ffprobe** | Check + one-line install hint; best-effort PM install on Linux only. |

**yt-dlp asset map** (`ytdlpAsset(goos, goarch) string`) — must use the *bundled-Python* builds, not the generic `yt-dlp` zipapp which needs system Python:

| GOOS/GOARCH | Asset |
|---|---|
| windows/amd64 | `yt-dlp.exe` |
| darwin/amd64, darwin/arm64 | `yt-dlp_macos` (universal) |
| linux/amd64 | `yt-dlp_linux` |
| linux/arm64 | `yt-dlp_linux_aarch64` |

Downloaded from `https://github.com/yt-dlp/yt-dlp/releases/latest/download/<asset>` → `~/.pixeltui/bin/yt-dlp` (`.exe` on Windows).

**Linux mpv command builder** (`mpvInstallCmd(isRoot, hasSudo bool, pm []string) []string`):
- root → `pm` as-is (no sudo).
- non-root + sudo present → `["sudo"] + pm`.
- non-root + no sudo → `pm` as-is (best effort; will fail with a clear message).
- For apt, run `apt-get update` before `apt-get install -y mpv`.
Package managers probed in order: apt-get, dnf, pacman, zypper. If none present → clear manual-install message.

### Plumbing

- New helper `downloadBinary(url, destPath) error`: stream to a temp file in the dest dir, `chmod +x` on POSIX, atomic rename. Reused by the yt-dlp fixer.
- Add `~/.pixeltui/bin` (yt-dlp / yt-dlp.exe) to **both** resolvers: `preferredYtdlp` (`main.go`) and `ytdlpPath` (`player.go`), after the venv check and before PATH.
- Fix the Linux yt-dlp **install hint** in `player.go:ytdlpInstall()` to reference the self-contained binary.

### Docs (update *all*)

- **README.md** — quickstart block: `curl -fsSL …/install.sh | sh` (macOS/Linux), `irm …/install.ps1 | iex` (Windows), the `--nodoctor` / env-var opt-out, and what `doctor` / `doctor --fix` do.
- **install.sh / install.ps1** — header comment blocks: document `--nodoctor`, `PIXELTUI_NO_DOCTOR`, and the back-compat `PIXELTUI_NO_DEPS`.
- **`printUsage()`** (`main.go`) — ensure `doctor [--fix]`, `setup`, `update` lines match actual behavior.

## Components & interfaces

- `ytdlpAsset(goos, goarch string) string` — pure; unit-tested.
- `mpvInstallCmd(isRoot, hasSudo bool, pm []string) []string` — pure; unit-tested.
- `downloadBinary(url, dest string) error` — I/O; covered by manual e2e.
- `checkTool(...)` / fixers — reuse the same resolver so check and post-fix re-check agree.

## Error handling

- Every fixer returns a bool/err and is verified by re-running `--version`; the final `doctor` table is the source of truth.
- Network failures in `downloadBinary` surface a clear message and leave no partial file (temp + atomic rename).
- Linux mpv with no package manager / no permission → explicit manual-install guidance, non-fatal.

## Testing

- **Unit tests** (`main_test.go` or similar):
  - `ytdlpAsset`: every supported os/arch → exact expected asset; unsupported → empty/sentinel.
  - `mpvInstallCmd`: root, non-root+sudo, non-root+no-sudo, each PM → exact argv.
- **Manual end-to-end:**
  - Fresh `ubuntu:latest` container: `curl … | sh` → expect yt-dlp + mpv installed, doctor all-green; and `--nodoctor` skips the phase.
  - Windows + macOS: `doctor --fix` from clean state.

## Rollout

Ship as **v0.3.3** via the existing manual release path (`make release` → `dist/` → `gh release create`), so `pixeltui update` / a fresh `curl | sh` pick it up. (Optional, separate: a GitHub Actions release workflow to remove the manual step — not part of this spec.)
