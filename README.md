# pixeltui

A fast, minimal, cross-platform **terminal music player** — search, stream, and
organize music from the command line, with a hybrid recommendation engine and
first-class support for self-hosted libraries.

![pixeltui demo](docs/demo.gif)

- **Fast & tiny** — a single ~10 MB Go binary, minimal dependencies, no Electron, no daemon.
- **Works anywhere** — macOS, Linux, and Windows (amd64 / arm64).
- **Few third parties** — streams from YouTube Music out of the box; optionally
  point it at your own Subsonic/Navidrome server or a folder of local files.
- **Yours to keep** — likes, playlists, and history are stored as open, portable
  files (M3U8 / XSPF / ListenBrainz JSON) you can take anywhere.
- **Synced lyrics** — karaoke-style, auto-scrolling lyrics via LRCLIB (free, no
  account); works for YouTube, Subsonic, and local tracks alike.

---

## Contents

- [Requirements](#requirements)
- [Install](#install)
- [Quick start](#quick-start)
- [Controls](#controls)
- [Commands](#commands)
- [Configuration](#configuration)
- [Sources](#sources)
- [Downloads](#downloads)
- [Library & interop](#library--interop)
- [Data directory](#data-directory)
- [Build from source](#build-from-source)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Requirements

| Tool      | Required? | Purpose |
|-----------|-----------|---------|
| **yt-dlp**  | required  | Resolves YouTube Music audio streams. |
| **mpv**     | recommended | Pause / seek / volume + OS "Now Playing". Without it, audio still plays via ffplay/afplay but transport controls are off. |
| **ffmpeg**  | optional  | `ffprobe` reads tags for local files; `ffplay` is a playback fallback. |

pixeltui can install and repair these for you — see [`doctor`](#commands).

Streaming a Subsonic/Navidrome server or playing local files does **not** require
yt-dlp (those play directly).

---

## Install

### macOS / Linux — one line

```sh
curl -fsSL https://raw.githubusercontent.com/dotjarden/pixeltui/main/install.sh | sh
```

Auto-detects your CPU, downloads the right prebuilt binary, puts `pixeltui` on
your PATH, and installs the playback dependencies (yt-dlp, mpv). No Go needed.

### Windows — one line (PowerShell)

```powershell
irm https://raw.githubusercontent.com/dotjarden/pixeltui/main/install.ps1 | iex
```

### Manual download

Grab the binary for your platform from the
[latest release](https://github.com/dotjarden/pixeltui/releases/latest):

| Platform | Asset |
|----------|-------|
| macOS, Apple Silicon (M1–M4) | `pixeltui-darwin-arm64` |
| macOS, Intel | `pixeltui-darwin-amd64` |
| Linux x86-64 | `pixeltui-linux-amd64` |
| Linux ARM64 | `pixeltui-linux-arm64` |
| Windows | `pixeltui-windows-amd64.exe` |

These are command-line programs — run them from a terminal, not by double-clicking
(double-clicking a binary just opens it in a text editor). On macOS:

```sh
chmod +x pixeltui-darwin-arm64
xattr -d com.apple.quarantine pixeltui-darwin-arm64    # clear Gatekeeper
sudo mv pixeltui-darwin-arm64 /usr/local/bin/pixeltui
pixeltui doctor --fix
```

### With Go installed

```sh
go install github.com/dotjarden/pixeltui@latest
pixeltui doctor --fix      # install yt-dlp + mpv
```

### Updating

```sh
pixeltui update
```

Downloads the latest release build for your platform, verifies its checksum, and
replaces the running binary in place. (You can also just re-run the install
one-liner, or `go install …@latest`.)

---

## Quick start

```sh
pixeltui                       # open the interactive player (search-first)
pixeltui "Get Lucky" "Daft Punk"   # start seeded from a track
pixeltui setup                 # configure Last.fm key, Subsonic, local & download folders
pixeltui doctor                # check everything is wired up
```

Recommendations use a free [Last.fm API key](https://www.last.fm/api/account/create).
Search and playback work without one.

---

## Controls

One rule keeps the keymap predictable for track actions:

> **lowercase acts on the highlighted track · SHIFT acts on what's playing.**
> (e.g. `f` likes the selected track, `F` likes the one currently playing.)

**Playback** (always the now-playing track)

| Key | Action |
|-----|--------|
| `↵` | play selected track |
| `space` | pause / resume |
| `← →` (or `h l`) | seek −10s / +10s |
| `n` | next track |
| `+` / `−` | volume up / down |

**Track** — lowercase = highlighted, **Shift** = now-playing

| Key | Action |
|-----|--------|
| `f` / `F` | like / unlike (♥) |
| `a` | add to queue   (`A` = play next) |
| `p` / `P` | add to playlist |
| `d` / `D` | download |
| `x` / `X` | mute artist for this session (`X` also skips) |
| `.` | **actions menu** — everything above, plus play-next & start station |
| `⇧↵` | start an endless station from the selection |

**Queue** (Up Next pane — `Tab` switches focus)

| Key | Action |
|-----|--------|
| `↑ ↓` | navigate · `j` `k` reorder |
| `del` | remove · `s` shuffle · `r` repeat · `c` clear |

**View & modes**

| Key | Action |
|-----|--------|
| `/` | search the current source |
| `b` | browse: Liked · playlists · Local · Subsonic · save queue |
| `y` | lyrics · `z` autoplay · `t` sleep timer |
| `Tab` | switch pane · `?` all keys · `q` quit · `esc` back |

Press `?` in the app for the full list at any time.

---

## Commands

```
pixeltui                          open the player (search-first)
pixeltui [track] [artist]         start seeded from a track
pixeltui setup                    interactive config wizard
pixeltui update                   self-update to the latest release
pixeltui doctor [--fix]           check setup; --fix auto-installs/repairs deps
pixeltui reset [target]           wipe data: cache | graph | library | config | all
pixeltui export <playlist> [file] write a playlist as XSPF (portable)
pixeltui build-graph              build the offline recommendation graph (once)
pixeltui cache warm --artist X    pre-fetch an artist for offline use
pixeltui cache stats | clear      show / wipe the cache
pixeltui help                     full usage and flags
```

Common flags for the recommend/seed mode: `-explore 0..10`, `-deep-cuts`,
`-no-artist "A,B"`, `-n N`, `-offline`, `-no-tui`, `-key <lastfm>`, `-dev`.

`doctor --fix` self-resolves the keystone dependencies: it installs a fast pip
**yt-dlp** and, on macOS, a self-contained **mpv** bundle (package manager on Linux).

---

## Configuration

Run `pixeltui setup`, or edit `~/.pixeltui/config.json`:

```json
{
  "lastfm_key": "",
  "subsonic": { "url": "", "user": "", "pass": "" },
  "local_dirs": [],
  "download_dir": "",
  "explore": 5,
  "autoplay": true
}
```

Every value can also be set by environment variable (env wins over the file):

| Variable | Meaning |
|----------|---------|
| `LASTFM_API_KEY` | Last.fm API key for recommendations |
| `PIXELTUI_SUBSONIC_URL` / `_USER` / `_PASS` | Subsonic/Navidrome server |
| `PIXELTUI_LOCAL_DIRS` | local music folders (PATH-style list) |
| `PIXELTUI_DOWNLOAD_DIR` | where downloads are saved |
| `PIXELTUI_YTDLP` / `PIXELTUI_MPV` | override the yt-dlp / mpv binary path |

The config file is written `0600` since it can hold a password.

---

## Sources

pixeltui can pull music from three places; switch between them in the `b` browse menu.

- **YouTube Music** (default) — search, radio, and lyrics with no account.
- **Subsonic / Navidrome** — point it at your server in `setup`. Starred songs and
  playlists are browsable and stream directly (no yt-dlp).
- **Local files** — add folders in `setup`; tags are read via `ffprobe` with a
  filename fallback. Plays directly.

`/` searches whichever source you're currently browsing.

---

## Downloads

Set a download folder (in `setup`), then press `d` on any YouTube track. Files are
saved with embedded tags and cover art in a standard layout:

```
<download_dir>/Artist/Album/Title.ext
```

That's exactly what Subsonic/Navidrome expect to scan — drop the folder into your
server's music library and it just works.

---

## Library & interop

Everything pixeltui stores about your listening is kept in open formats under
`~/.pixeltui/library/`, so nothing is locked in:

- **Likes & playlists** → M3U8 (export any playlist to XSPF with `pixeltui export`)
- **Listening history** → ListenBrainz-style JSON
- **Up Next + session** → restored on the next launch

---

## Data directory

Everything lives under `~/.pixeltui/`:

```
config.json      configuration
cache.db         stream-URL + API cache (bbolt)
graph.bin        offline recommendation graph
library/         likes, playlists, history
ytdlp-venv/      fast pip yt-dlp (optional)
mpv.app/         self-contained mpv (macOS, optional)
```

Reset any of it with `pixeltui reset <target>`. Installed tools (mpv, yt-dlp) are
kept even on `reset all`.

---

## Companion server (experimental)

`pixeltui serve` exposes your library and sources over HTTP so a phone (or any
client) can browse, search, and stream from anywhere:

```sh
pixeltui serve                 # prints a pairing QR + code
pixeltui serve --url https://pixeltui.example.ts.net   # behind a tunnel
```

- **Pairing:** scan the QR (or enter the URL + code) once; the device gets a
  saved token. Tokens live in `~/.pixeltui/devices.json` (revocable).
- **Transport:** REST for actions, Server-Sent Events for live state.
- **Streaming:** Subsonic and local play directly (range-aware); YouTube
  transcoding is on the roadmap.
- **From anywhere:** bring your own tunnel — [Tailscale](https://tailscale.com)
  is the easy, private option; pass its address with `--url`.

A native companion app (Flutter) is in progress. The server is the stable,
documented contract it builds on.

## Build from source

Requires Go 1.25+.

```sh
make build        # → ./pixeltui
make install      # → $PREFIX/pixeltui (default /usr/local/bin)
make release      # cross-compile for all platforms → dist/
make help         # all targets
```

---

## Troubleshooting

Run `pixeltui doctor` — it reports the status of every dependency, your API key,
configured sources, the graph, and the cache, and tells you exactly how to fix
anything that's missing. Add `--fix` to auto-resolve what it can.

---

## License

pixeltui is released under the [MIT License](LICENSE).

The Go libraries compiled into the binary are all permissive (MIT / BSD-3-Clause).
The external tools pixeltui drives — **yt-dlp**, **mpv**, and **ffmpeg** — are run
as separate, user-installed programs; they are not bundled with or linked into
pixeltui, so their licenses (Unlicense, GPL/LGPL) apply only to those tools. Full
attribution and per-license details are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
