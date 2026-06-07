# `pixeltui uninstall` — design

**Date:** 2026-06-07
**Status:** Approved (design)
**Scope:** `tui/main.go` (new command + dispatch + usage), `README.md`

## Problem

There is no first-class uninstaller. `make uninstall` only removes the binary (clone-only); `reset all` wipes data but deliberately keeps the binary and tools. A `curl|sh` user has no clean teardown, and `~/.pixeltui` now holds tens of MB of bundled tools (self-contained yt-dlp, mpv bundle/build); Windows also mutates User PATH with no reverse.

## Decision

`pixeltui uninstall [--keep-data] [-y]` — **full clean by default**, `--keep-data` spares the user's library + config.

## Behavior

1. Resolve targets: running binary (`os.Executable()` + `EvalSymlinks`), data dir `~/.pixeltui`, and (Windows) the User PATH entry for the binary's directory.
2. Print the removal plan (what goes, what stays); confirm `[y/N]` unless `-y`.
3. Remove:
   - **Data:** default `RemoveAll(~/.pixeltui)`. With `--keep-data`, remove every entry except `library/` and `config.json`.
   - **Binary:**
     - Unix: `os.Remove(exe)` (a running binary can be unlinked). On permission error, print `sudo rm <path>` to finish.
     - Windows: rename `exe` → `exe.old` (can't delete a running .exe), strip the install dir from User PATH (via PowerShell `[Environment]::SetEnvironmentVariable`), and spawn a detached `cmd /c` cleaner to delete the `.old` + install dir after exit.
   - **System mpv** (package-manager installed on Linux): left in place; print the `apt-get remove mpv` (etc.) hint.

## Components

- `uninstallDataTargets(entries []string, keepData bool) []string` — pure; returns the data-dir entry names to remove. `keepData` skips `library` and `config.json`. Unit-tested.
- `cmdUninstall(args []string)` — orchestration + platform binary removal + PATH cleanup.
- Dispatch: add `case "uninstall":` in `main()`. Usage: add a line to `printUsage`.

## Error handling

- Unwritable binary → clear `sudo rm` hint, non-fatal.
- Missing data dir → no-op.
- Never auto-removes a shared system package.

## Testing

- Unit: `uninstallDataTargets` — default returns all entries; `keepData` excludes `library` + `config.json`.
- Manual e2e (Ubuntu container): install the new binary, seed `~/.pixeltui`, run `pixeltui uninstall -y`, assert the binary and `~/.pixeltui` are gone.

## Rollout

Ship as **v0.3.4** via the manual release path.

## Docs

README: add `uninstall` to the Commands list and a short "Uninstall" subsection.
