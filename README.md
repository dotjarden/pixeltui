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
- **Artist & album pages** — drill into any artist (top songs · albums · singles,
  with Last.fm listener stats) or album (ordered tracklist, year), like the big apps.
- **Scrobbling** — submit plays to Last.fm and/or ListenBrainz, with an offline
  spool so listens made without a connection aren't lost.

---

## Contents

- [Requirements](#requirements)
- [Install](#install)
- [Quick start](#quick-start)
- [Controls](#controls)
- [Scrobbling](#scrobbling)
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

Skip the automatic dependency check/install with `--nodoctor`
(`curl … | sh -s -- --nodoctor`) or `PIXELTUI_NO_DOCTOR=1`. You can always run
`pixeltui doctor --fix` later.

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
go install github.com/dotjarden/pixeltui/tui@latest   # binary installs as `tui`; rename to `pixeltui` if you like
pixeltui doctor --fix      # install yt-dlp + mpv
```

### Updating

```sh
pixeltui update            # latest release
pixeltui update v0.2.4     # a specific tag — also how you roll back
```

Downloads the release build for your platform, verifies its checksum, and
replaces the running binary in place. With no argument it tracks the latest
release; pass a tag (with or without the `v`) to pin or roll back. All
releases: <https://github.com/dotjarden/pixeltui/releases>. (You can also just
re-run the install one-liner, or `go install …@latest`.)

### Uninstall

```sh
pixeltui uninstall            # remove the binary, data dir, and bundled yt-dlp/mpv
pixeltui uninstall --keep-data   # keep your library + config; remove everything else
pixeltui uninstall -y         # skip the confirmation prompt
```

A full clean by default: removes the `pixeltui` binary, the `~/.pixeltui` data
directory (cache, graph, library, config) and the self-contained tools it
installed there. On Windows it also strips the PATH entry the installer added.
**mpv installed via your system package manager is left in place** (remove it
yourself, e.g. `sudo apt-get remove mpv`).

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
| `← →` (or `h l`) | seek (step configurable in settings, default 10s) |
| `n` | next track · `;` jump to the playing track |
| `+` / `−` | volume up / down |

**Track** — lowercase = highlighted, **Shift** = now-playing

| Key | Action |
|-----|--------|
| `f` / `F` | like / unlike (♥) |
| `a` | add to queue   (`A` = play next) |
| `p` / `P` | add to playlist |
| `d` / `D` | download |
| `x` / `X` | mute artist for this session (`X` also skips) |
| `.` | **actions menu** — everything above, plus **go to artist / album** & start station |
| `⇧↵` | start an endless station from the selection |

**Artist & album pages**

| Key | Action |
|-----|--------|
| `/` then `!a <artist>` | artist page: top songs · albums · singles (+ Last.fm listener stats) |
| `/` then `!al <album>` | album search → full album page (ordered tracklist, year, durations) |
| `↵` on an album row | open the album |
| `esc` | back through pages (album → artist → results) |
| `e` | queue everything on the page |

**Queue** (Up Next pane — `Tab` switches focus)

| Key | Action |
|-----|--------|
| `↑ ↓` | navigate · `j` `k` reorder |
| `del` | remove · `s` shuffle · `r` repeat · `c` clear |
| `u` | undo the last clear / remove / shuffle |

**View & modes**

| Key | Action |
|-----|--------|
| `/` | search the current source |
| `b` | browse: Liked · playlists · charts · stats · Local · Subsonic (`o` on a playlist starts a blended station · `u` restores a deleted playlist) |
| `y` | lyrics — `[` `]` nudge the sync, `0` resets |
| `z` autoplay · `t` sleep timer · `,` settings |
| `Tab` | switch pane · `?` all keys · `q` quit · `esc` back |

Press `?` in the app for the full list at any time.

---

## Scrobbling

pixeltui can submit your plays to **Last.fm** and/or **ListenBrainz**:

1. `pixeltui setup` → *Scrobbling* — paste your Last.fm **API key + shared
   secret** (both on the [same page](https://www.last.fm/api/account/create))
   and/or a [ListenBrainz token](https://listenbrainz.org/profile/), and enable it.
2. For Last.fm, a one-time browser authorization runs at the end of setup
   (redo anytime with `pixeltui scrobble-auth`).

Plays submit at the standard 50% / 4-minute mark, with a now-playing update at
track start. Plays made offline are spooled and retried on the next launch.
Toggle scrobbling live in the in-app settings (`,`).

---

## Commands

```
pixeltui                          open the player (search-first)
pixeltui [track] [artist]         start seeded from a track
pixeltui setup                    interactive config wizard
pixeltui scrobble-auth            authorize Last.fm scrobbling (one-time)
pixeltui serve [--tunnel …]       companion-app server (see "Companion server")
pixeltui update [version]         self-update: latest, or a tag like v0.2.4
pixeltui doctor [--fix]           check setup; --fix auto-installs/repairs deps
pixeltui reset [target]           wipe data: cache | graph | library | config | all
pixeltui uninstall [--keep-data]  remove pixeltui, data, and bundled tools
pixeltui export <playlist> [file] write a playlist as XSPF (portable)
pixeltui build-graph              build the offline recommendation graph (once)
pixeltui cache warm --artist X    pre-fetch an artist for offline use
pixeltui cache stats | clear      show / wipe the cache
pixeltui help                     full usage and flags
```

Common flags for the recommend/seed mode: `-explore 0..10`, `-deep-cuts`,
`-no-artist "A,B"`, `-n N`, `-offline`, `-no-tui`, `-key <lastfm>`, `-dev`.

`doctor --fix` self-resolves the keystone dependencies: it installs a
self-contained **yt-dlp** binary into `~/.pixeltui/bin` (no Python needed) and
**mpv** — a bundle on macOS, the standalone build on Windows, your package
manager on Linux.

---

## Configuration

Run `pixeltui setup`, or edit `~/.pixeltui/config.json`:

```json
{
  "lastfm_key": "",
  "scrobble": {
    "enabled": false,
    "lastfm_secret": "",
    "lastfm_session": "",
    "lastfm_user": "",
    "listenbrainz_token": ""
  },
  "subsonic": { "url": "", "user": "", "pass": "" },
  "local_dirs": [],
  "download_dir": "",
  "explore": 5,
  "autoplay": true,
  "seek_step": 10,
  "charts": { "global": true, "country": "" },
  "server": { "addr": ":8787", "name": "", "public_url": "", "tunnel": "" }
}
```

The `server` block sets the defaults for `pixeltui serve` — bind address,
advertised name, and remote access (`tunnel`: `"tailscale"`, `"cloudflare"`,
`"ngrok"`, or a fixed `public_url` for a tunnel you run yourself). See
[Companion server](#companion-server-experimental).

**Current charts** (live YouTube Music top tracks — no API key needed) show in
**Browse** and on the **For You** landing: `charts.global` (worldwide Top, on by
default) and `charts.country` — a country name or 2-letter code, e.g.
`"United States"` or `"GB"`.

Every value can also be set by environment variable (env wins over the file):

| Variable | Meaning |
|----------|---------|
| `LASTFM_API_KEY` | Last.fm API key for recommendations |
| `LASTFM_API_SECRET` | Last.fm shared secret (scrobbling) |
| `LISTENBRAINZ_TOKEN` | ListenBrainz user token (scrobbling) |
| `PIXELTUI_SUBSONIC_URL` / `_USER` / `_PASS` | Subsonic/Navidrome server |
| `PIXELTUI_LOCAL_DIRS` | local music folders (PATH-style list) |
| `PIXELTUI_DOWNLOAD_DIR` | where downloads are saved |
| `PIXELTUI_SERVE_ADDR` / `_URL` / `_TUNNEL` | `pixeltui serve` bind address / public URL / tunnel |
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
bin/             self-contained tools pixeltui installed (yt-dlp)
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
pixeltui serve                       # LAN: prints a pairing QR + code
pixeltui serve --tunnel tailscale    # advertise your tailnet address
pixeltui serve --tunnel cloudflare   # public trycloudflare.com URL, no account
pixeltui serve --tunnel ngrok        # public ngrok URL (authtoken required)
pixeltui serve --url https://music.example.com   # BYO tunnel / reverse proxy
```

All of these can be saved as defaults in `pixeltui setup` (Companion server
step) — or in the `server` section of `~/.pixeltui/config.json` — so a plain
`pixeltui serve` does the right thing:

```jsonc
"server": {
  "addr": ":8787",            // bind address
  "name": "studio-mac",       // name shown on paired devices
  "tunnel": "tailscale",      // "", "tailscale", "cloudflare", or "ngrok"
  "public_url": ""            // fixed URL for a tunnel you manage yourself
}
```

- **Pairing:** scan the QR (or enter the URL + code) once; the device gets a
  saved token (hashed at rest in `~/.pixeltui/devices.json`, revocable).
  Pairing codes are single-use and rotate after repeated bad attempts.
- **Transport:** REST for actions, Server-Sent Events for live state.
- **Streaming:** everything is served range-aware — Subsonic and local files
  directly, YouTube via a natively resolved AAC/m4a CDN URL (instant seek, no
  transcoding; ffmpeg only as a last-resort fallback).
- **Shared library:** likes and playlists live in `~/.pixeltui/library` as
  plain M3U8 — the TUI and every paired client read *and write* the same
  files, so changes made anywhere show up everywhere.
- **Shared history:** clients report plays back (`/api/played`), landing in
  the same `history.jsonl` the TUI writes — so phone listening feeds Recently
  Played, listening stats, recommendations, and scrobbling
  (Last.fm / ListenBrainz) exactly like TUI plays.
- **Full feature surface:** search (all sources), artist/album pages, charts,
  radio stations, engine recommendations, synced lyrics, listening stats,
  likes/playlists, play history — everything the TUI offers, over
  `/api/*` endpoints guarded by per-device tokens.

### Tunnel options

| Tunnel | URL | Privacy | Needs |
|---|---|---|---|
| `tailscale` | `http://<host>.ts.net:8787` | private mesh (WireGuard) — **recommended** | Tailscale on both devices |
| `cloudflare` | random `*.trycloudflare.com` (HTTPS) | public URL, bearer token is the gate | `cloudflared` binary |
| `ngrok` | ngrok domain (HTTPS) | public URL, bearer token is the gate | `ngrok` + authtoken |
| `--url` | whatever you run | up to you | your own tunnel/proxy |

`--tunnel cloudflare` and `--tunnel ngrok` spawn the tunnel as a child
process, wait for the public URL, bake it into the pairing QR, and tear the
tunnel down when the server stops. `--tunnel tailscale` starts nothing — it
just detects your tailnet DNS name and advertises it.

### From anywhere (Tailscale)

The server binds plain HTTP, so don't port-forward it to the open internet —
put it on a private tunnel. [Tailscale](https://tailscale.com) is the
zero-config option (free for personal use, WireGuard-encrypted end to end):

1. Install Tailscale on the machine running `pixeltui serve` and sign in
   (`brew install --cask tailscale` on macOS, or your package manager).
2. Install the Tailscale app on the phone, sign in to the **same account**,
   and leave its VPN toggle on.
3. Find the machine's tailnet name: `tailscale status` (e.g.
   `mymac.tail1234.ts.net`).
4. Start the server — it detects the tailnet name itself:

   ```sh
   pixeltui serve --tunnel tailscale
   ```

5. Pair by scanning the QR. The phone now reaches your library from any
   network — home Wi-Fi, LTE, hotel — as long as Tailscale is connected.

The traffic is HTTP *inside* an encrypted WireGuard tunnel, and only devices
on your tailnet can reach the port at all. Other tunnels (Cloudflare Tunnel,
ngrok) also work — pass whatever public URL they give you via `--url` — but
they expose the endpoint publicly, leaving only the bearer token as the gate,
so a private mesh like Tailscale is the recommended default.

## Repository layout

```
tui/   the pixeltui Go app (terminal player + `serve`) — go.mod at root
```

## Build from source

Requires Go 1.25+.

```sh
make build        # → ./pixeltui  (builds ./tui)
make install      # → $PREFIX/pixeltui (default /usr/local/bin)
make release      # cross-compile for all platforms → dist/
make help         # all targets
```

The build version is embedded via `-ldflags -X main.version` (`pixeltui version`);
plain `make build` derives it from `git describe`.

### Releasing (maintainers)

```sh
scripts/release.sh v0.2.0     # tags + pushes; CI does the rest
```

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which cross-builds
every platform, writes `SHA256SUMS`, and publishes the GitHub release that
`pixeltui update` pulls from. Tags with a suffix (e.g. `v0.2.0-rc1`) publish as
pre-releases.

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
