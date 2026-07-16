# pixeltui desktop

Cross-platform desktop client for pixeltui (macOS + Windows). Built with **Tauri 2** + **SvelteKit 5**.

The desktop app is a self-contained host: it bundles the `pixeltui` Go binary as a sidecar, spawns it on `127.0.0.1:8790`, pairs once, and serves the SvelteKit UI in a webview.

## Develop

```sh
cd clients/desktop
pnpm install
pnpm tauri dev
```

The first run will show an onboarding wizard that writes `~/.pixeltui/config.json` and runs `pixeltui doctor --fix` to install yt-dlp + mpv.

## Build / release

```sh
# Build sidecars for the current platform first
CGO_ENABLED=0 go build -ldflags="-s -w" -o src-tauri/binaries/pixeltui-$(rustc -vV | sed -n 's|host: ||p') ./tui

# Bundle the desktop app
pnpm tauri build
```

CI builds all platforms on tag `desktop-v*`. See `.github/workflows/release-desktop.yml`.

## Notable architecture

- Sidecar address: `127.0.0.1:8790` (coexists with a TUI server on `:8787`).
- Auth: `Authorization: Bearer <token>` for fetch; `?token=` for media/EventSource.
- Downloads save bytes client-side from `/api/stream` to `~/.pixeltui/downloads/`, then load offline via the Tauri asset protocol (`fetch(convertFileSrc) → Blob → objectURL` to keep Web Audio CORS-clean).
- Settings edit `~/.pixeltui/config.json` directly and restart the sidecar.
- Party mode is remote-only (join an existing pocket/host room by code).
- Auto-updater uses `tauri-plugin-updater`; endpoint + public key are in `src-tauri/tauri.conf.json`.

## Gaps vs iOS

- No Identify / ShazamKit (skipped for v1).
- macOS has no in-app audio output picker (`setSinkId` is unavailable in WKWebView).
- Last.fm browser auth must be done via `pixeltui scrobble-auth` in a terminal once, then paste the session key into Settings.
