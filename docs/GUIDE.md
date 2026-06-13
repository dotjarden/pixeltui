# pixeltui — user guide

The extended manual. If you just want to install and play, start with the
[README](../README.md) — it covers installation, the quick keymap, and the
config reference. This guide goes deeper: how the pieces work, how to tune
them, and what to do when something misbehaves.

Companion-app developers: the server's HTTP API is documented in
[API.md](API.md), and the internals in [ARCHITECTURE.md](ARCHITECTURE.md).

## Contents

- [Getting set up](#getting-set-up)
- [Everyday playing](#everyday-playing)
- [Your library](#your-library)
- [Recommendations deep dive](#recommendations-deep-dive)
- [Scrobbling](#scrobbling)
- [Downloads](#downloads)
- [Self-hosted & local sources](#self-hosted--local-sources)
- [The companion app](#the-companion-app)
- [Offline behavior summary](#offline-behavior-summary)
- [Data & privacy](#data--privacy)
- [Troubleshooting & FAQ](#troubleshooting--faq)

---

## Getting set up

### What you actually need

Almost nothing. Streaming resolves **natively in-process** via InnerTube
(~0.2 s) — there is no Python helper on the playback path. The external tools
are quality-of-life:

| Tool | Required? | What it adds |
|---|---|---|
| **mpv** | recommended | pause / seek / volume, plus the OS "Now Playing" integration. Without it audio still plays through ffplay or afplay, but transport controls are off. |
| **yt-dlp** | optional | powers downloads (`d` key) and acts as the stream-resolution *fallback* for the rare video the native resolver can't handle. Not needed to play. |
| **ffmpeg** | optional | `ffprobe` reads tags from local files; `ffplay` is a playback fallback. |

Subsonic/Navidrome and local files stream directly and need none of these
beyond a player.

### The setup wizard

```sh
pixeltui setup
```

(First launch with no config offers the same wizard automatically.) It walks
through seven short pages — Tab/↑↓ move, Enter advances, Esc cancels with
nothing saved:

1. **Last.fm API key + theme** — the key powers recommendations and is free
   ([last.fm/api/account/create](https://www.last.fm/api/account/create));
   it's a 32-character hex string and the wizard validates the format. Search
   and playback work without one. Themes: `default` (purple/pink), `ocean`,
   `matrix`, `amber`, `rose`, `mono`.
2. **Scrobbling** — enable the master switch, paste the Last.fm **shared
   secret** (same page as the key) and/or a
   [ListenBrainz token](https://listenbrainz.org/profile/).
3. **Playback** — discovery level (0 safe · 5 balanced · 10 wild) and whether
   autoplay tops up the queue with similar tracks when it runs out.
4. **Subsonic / Navidrome** — URL, username, password for an optional
   self-hosted source.
5. **Local folders & download folder** — comma-separated paths for local
   music (each is checked to exist); a download folder enables the `d` key.
6. **Charts** — toggle the live worldwide Top chart and pick an optional
   country chart. No API key needed for either.
7. **Companion server** — defaults for `pixeltui serve`: remote-access mode
   (LAN only / Tailscale / Cloudflare / ngrok), bind address (default
   `:8787`), and an optional fixed public URL for a tunnel you run yourself.

After saving, if scrobbling is enabled with Last.fm credentials but no
session yet, the wizard runs the one-time browser authorization on the spot,
then live-tests your Last.fm key and Subsonic connection.

Everything the wizard writes lands in `~/.pixeltui/config.json` (file mode
`0600`, since it can hold a password). You can edit it by hand or override
any value with environment variables — see the README's
[Configuration](../README.md#configuration) table. Env always wins over the
file.

### doctor: the health check

```sh
pixeltui doctor          # report only
pixeltui doctor --fix    # auto-resolve what it can
```

`doctor` prints a status table covering:

- **yt-dlp** — found / runnable / missing (missing is only a *warning*:
  streaming works without it). `--fix` downloads a self-contained binary
  (no Python) into `~/.pixeltui/bin`.
- **mpv** — `--fix` installs it too: a bundled app on macOS
  (`~/.pixeltui/mpv.app`), the standalone build on Windows, your package
  manager on Linux. Resolution order: `$PIXELTUI_MPV` → bundled copy → PATH.
- **fallback players** — ffplay / afplay; it flags loudly if *nothing* can
  play audio.
- **ffprobe** — only warned about if you have local folders configured.
- **Last.fm key**, **scrobbling state** (including "configured but not
  authorized — run `pixeltui scrobble-auth`"), **Subsonic** (live ping),
  **local folders** (each checked to exist).
- **Data**: data dir path, the offline graph (artist count + age, or a nudge
  to run `build-graph`), and cache size.

Run it whenever something feels off — every failing row says exactly how to
fix it.

---

## Everyday playing

The keymap has one rule worth internalizing:

> **lowercase acts on the highlighted track · SHIFT acts on what's playing.**

So `f` likes the selected row, `F` likes the song currently coming out of
your speakers. `d`/`D` downloads, `p`/`P` adds to a playlist, `x`/`X` mutes
the artist for the session (`X` also skips). Press `?` anytime for the full
in-app list.

### Search

`/` opens search against the **current source** — the prompt shows which one,
and Tab cycles YouTube Music · Subsonic · Local. Two search prefixes open
dedicated pages:

| Prefix | Result |
|---|---|
| `!a <artist>` | artist page — top songs, albums, singles, Last.fm listener stats |
| `!al <album>` | album search → full album page (ordered tracklist, year, durations) |

On any page, `↵` on an album row opens it, `e` queues everything on the page,
and `esc` walks back through pages (album → artist → results).

`'` is different from `/`: it **filters the current list in place** with a
fuzzy match — handy in long playlists or browse views.

### Queue

Tab switches focus between the main pane and **Up Next**. With the queue
focused:

| Key | Action |
|---|---|
| `↑ ↓` | navigate |
| `j` / `k` | move the selected entry down / up |
| `del` | remove |
| `s` / `r` / `c` | shuffle · repeat · clear |
| `u` | undo the last clear / remove / shuffle |

`a` adds the highlighted track to the back of the queue; `A` plays it next.

Track-to-track transitions are **gapless** when mpv is the player: once the
next queued track's stream URL is preloaded, it's appended to the running
mpv's playlist over IPC, so the natural end of a song flows straight into the
next with no process respawn and no silence. Manual skips (`n`) and the
ffplay/afplay fallbacks use the regular start path. The iOS companion app is
gapless too, and adds an optional crossfade (Settings → Crossfade,
2–12 seconds).

### Browse

`b` opens browse: **Liked Songs · your playlists · charts · listening stats ·
Local · Subsonic**. Inside browse: `del` deletes a playlist, `p` renames,
`u` restores a deleted playlist, and `o` on a playlist starts a **blended
station** seeded from up to 4 random tracks in it.

### Stations & autoplay

- `o` starts an endless station from the highlighted track (`O` from the
  now-playing track) — also reachable from the `.` actions menu.
- `z` toggles **autoplay**: when the queue runs dry, pixeltui tops it up with
  recommendations seeded from what you've been playing, tuned by your
  discovery level.

### Lyrics

`y` shows karaoke-style synced lyrics (via LRCLIB — free, no account; works
for YouTube, Subsonic, and local tracks). If the timing is off, `[` and `]`
nudge the sync and `0` resets it.

### Sleep timer

`t` cycles the sleep timer: **off → 15 → 30 → 60 minutes → off**. When it
expires, playback stops.

### Settings overlay

`,` opens live settings — changes apply immediately and persist to
`config.json`:

| Row | Notes |
|---|---|
| Theme | recolors the UI instantly (`default` / `ocean` / `matrix` / `amber` / `rose` / `mono`) |
| Discovery level | 0–10, same knob as `--explore` |
| Autoplay | on / off |
| Global chart | on / off |
| Country chart | cycle through countries / off |
| Scrobbling | live toggle (credentials still come from `setup`) |
| Seek step | how far `←`/`→` jump (default 10 s) |

---

## Your library

Everything pixeltui knows about your listening lives under
`~/.pixeltui/library/` in **open, portable formats** — nothing is locked in.

| File | Format | Contents |
|---|---|---|
| `playlists/*.m3u8` | M3U8 | your playlists; one file per playlist |
| `playlists/Liked Songs.m3u8` | M3U8 | likes — "Liked Songs" is a reserved playlist name |
| `history.jsonl` | ListenBrainz-style JSON Lines | every play, appended as it happens |
| `session.json` | JSON | Up Next + playback state, restored on next launch |
| `scrobble-spool.jsonl` | JSON Lines | scrobbles made offline, awaiting submission |

- **Likes** — `f`/`F` in the app. If Last.fm scrobbling is set up, a like is
  also "loved" on Last.fm.
- **Playlists** — `p`/`P` to add tracks; manage (rename, delete, restore)
  from browse.
- **History** feeds Recently Played, your listening stats, the genre chart,
  and the recommendation engine's affinity signals.
- **Sessions** — quit mid-album and the queue and position come back next
  launch.

### Export & interop

```sh
pixeltui export "Road Trip" road-trip.xspf
```

Writes any playlist (including Liked Songs) as **XSPF**, the portable
playlist format most players import. The M3U8 files themselves are also
plain text you can sync, version, or open elsewhere — and the companion
server reads and writes the very same files, so the TUI and your phone share
one library.

`pixeltui reset library` wipes it all (with a confirmation prompt);
`reset cache | graph | config | all` handle the rest. Even `reset all`
keeps the installed tools.

---

## Recommendations deep dive

### How a recommendation happens

Every query walks a hybrid, four-layer source chain — the first layer that
answers wins:

1. **Static graph** (`~/.pixeltui/graph.bin`) — a prebuilt artist-similarity
   graph. Instant, fully offline.
2. **Fresh cache** (`~/.pixeltui/cache.db`, bbolt) — recent API results.
3. **Live Last.fm** — online lookups, automatically written back to the cache.
4. **Stale cache** — expired entries, used as a last resort when offline.

Candidates are then scored as a weighted blend of four signals (defaults, at
explore level 5):

| Signal | Weight | What it measures |
|---|---|---|
| Similarity | 0.40 | how closely the artist matches the seed (graph/Last.fm) |
| Artist novelty | 0.35 | 1.0 for a different artist, 0.0 for seed-family tracks — the anti-"more of the same" signal |
| Popularity | 0.20 | *anti*-popularity: niche artists score higher |
| Serendipity | 0.05 | deterministic jitter so results aren't fully predictable |

Two more touches shape the final list:

- **Liked-artist affinity** — artists you've liked or play often get a
  modest boost (weight 0.20, kept small so it nudges rather than dominates).
- **Per-artist cap** — at most **2 tracks per artist** in any result set.

### Tuning

- **Explore (0–10)** — set in `setup`, settings (`,`), or `--explore`. It
  doesn't just filter: it *re-weights the scoring*, interpolating between a
  "safe" profile (similarity 0.60, novelty 0.25, popularity 0.10) at 0 and a
  "wild" one (similarity 0.20, novelty 0.45, popularity 0.30) at 10. Level 5
  is exactly the defaults above.
- **`--deep-cuts`** — skips top hits and surfaces album tracks and deep
  catalogue.
- **`--no-artist "A,B"`** — exclude artists (case-insensitive) from results.
- **`--dev`** — print the per-signal scoring breakdown for every result, if
  you want to see why something was picked.

```sh
pixeltui "Get Lucky" "Daft Punk" --explore 8 --deep-cuts -n 15
pixeltui "Holocene" "Bon Iver" --no-tui        # plain numbered list, no UI
```

### Going offline: build-graph and cache warm

The graph makes recommendations work with no network at all:

```sh
pixeltui build-graph                    # one-time; needs a Last.fm key
pixeltui build-graph --max 10000 --rate 4.5 --workers 10
```

It crawls Last.fm breadth-first from popular seed artists, rate-limited
(default 4.5 requests/sec — the free tier allows 5) across parallel workers,
and writes `graph.bin`. It prints an estimated build time up front, and
Ctrl-C saves a partial graph. Defaults: 5 000 artists max, 10 workers.

For specific favorites, pre-fetch them into the cache instead:

```sh
pixeltui cache warm --artist "Radiohead"
pixeltui cache stats     # size & contents
pixeltui cache clear     # wipe it (refills on use)
```

Then `--offline` (or just losing your connection) runs entirely on layers
1, 2, and 4.

---

## Scrobbling

pixeltui submits plays to **Last.fm** and/or **ListenBrainz** — either or
both.

**Setup once:**

1. `pixeltui setup` → enable scrobbling, paste the Last.fm API key *and*
   shared secret (both on the
   [same page](https://www.last.fm/api/account/create)) and/or a
   [ListenBrainz token](https://listenbrainz.org/profile/).
2. Last.fm needs a one-time browser authorization; setup runs it at the end,
   or do it later with `pixeltui scrobble-auth` (which also flips the enable
   switch on for you).

**Behavior:**

- A **now-playing** update fires at track start; the play itself submits at
  the standard **50% or 4-minute** mark, whichever comes first.
- Plays made offline are **spooled** to
  `~/.pixeltui/library/scrobble-spool.jsonl` (capped at **500** entries,
  oldest dropped first) and retried on the next launch.
- Liking a track (`f`/`F`) also **loves** it on Last.fm.
- Plays reported by companion-app clients scrobble exactly like TUI plays.
- Toggle live in settings (`,`); `pixeltui doctor` shows the current
  scrobbling state, including "configured but not authorized".

---

## Downloads

Downloads are the one feature that *requires* yt-dlp
(`pixeltui doctor --fix` installs a self-contained binary — no Python).

1. Set a download folder in `setup`.
2. Press `d` on a highlighted YouTube track (`D` for the now-playing one),
   or use the `.` actions menu.

Files are saved with embedded tags and cover art in a server-friendly layout:

```
<download_dir>/Artist/Album/Title.ext
```

That's exactly the structure Subsonic/Navidrome scan, so the folder doubles
as a seed for a self-hosted library — point your server (or pixeltui's own
local-folders source) at it and it just works.

---

## Self-hosted & local sources

pixeltui plays from three sources; `b` (browse) and the search prompt's Tab
key switch between them.

### YouTube Music (default)

Search, radio, charts, and lyrics with no account. Streams resolve natively
in-process.

### Subsonic / Navidrome

Configure in `setup`, `config.json`, or env
(`PIXELTUI_SUBSONIC_URL` / `_USER` / `_PASS`). Starred songs and playlists
are browsable, and tracks stream **directly** from your server — no yt-dlp,
no YouTube involved. `setup` and `doctor` both live-test the connection.

### Local folders

Add folders in `setup` (or `PIXELTUI_LOCAL_DIRS`, a PATH-style list). Tags
are read with `ffprobe`, falling back to filename parsing when it's missing.
The scan results are kept in an index (`~/.pixeltui/local-index.json`)
keyed by file modification time, so rescans only touch files that changed.
Plays are direct from disk.

---

## The companion app

`pixeltui serve` turns your machine into a music server any paired client
can browse, search, and stream from. The native iOS app, **PixelPal** (SwiftUI,
in `clients/ios`), has full feature parity: offline downloads, two-way
like/playlist sync, daily mixes, stations, lyrics, and stats. The HTTP API
is in [API.md](API.md) if you want to build your own client.

### Pairing walkthrough

1. On the computer: `pixeltui serve`. It prints the addresses it's reachable
   at and a **pairing QR + code**.
2. On the phone: scan the QR (or type the URL and code).
3. Done — the device receives a token (stored **hashed** in
   `~/.pixeltui/devices.json`) and never needs the code again. Pairing codes
   are single-use and rotate after repeated bad attempts.

### Remote access options

| Mode | Command | URL you get | Notes |
|---|---|---|---|
| LAN only | `pixeltui serve` | `http://<lan-ip>:8787` | home Wi-Fi only |
| Tailscale | `pixeltui serve --tunnel tailscale` | `http://<host>.ts.net:8787` | **recommended** — private WireGuard mesh; pixeltui just detects and advertises your tailnet name |
| Cloudflare | `pixeltui serve --tunnel cloudflare` | random `*.trycloudflare.com` | public HTTPS, no account; needs `cloudflared` |
| ngrok | `pixeltui serve --tunnel ngrok` | ngrok domain | public HTTPS; needs ngrok + authtoken |
| BYO | `pixeltui serve --url https://music.example.com` | yours | you run the tunnel / reverse proxy |

Cloudflare and ngrok are spawned as child processes: pixeltui waits for the
public URL, bakes it into the pairing QR, and tears the tunnel down on exit.
Save your preferred mode in `setup` → *Companion server* so a plain
`pixeltui serve` does the right thing.

The server binds plain HTTP — **don't port-forward it to the open
internet**. On public tunnels the per-device bearer token is the only gate;
a private mesh like Tailscale is the safer default.

### When the address changes

A Cloudflare *quick* tunnel gets a new random URL every start. The server
advertises **all** of its addresses to paired clients, and the app falls
back across them automatically — e.g. to the LAN address when you're home.
If no known address works (fresh tunnel URL while you're away), paste the
new URL in the app under **Settings → Server → Change Address** — the
pairing carries over. For a stable address, use Tailscale.

### What's shared

- **Library**: likes and playlists are the same M3U8 files in
  `~/.pixeltui/library` — clients read *and write* them, so changes made
  anywhere appear everywhere.
- **History**: clients report plays to `/api/played`, landing in the same
  `history.jsonl` — phone listening feeds Recently Played, stats,
  recommendations, and scrobbling exactly like TUI plays.
- **Everything else**: search across all sources, artist/album pages,
  charts, stations and daily mixes, engine recommendations, synced lyrics —
  REST for actions, Server-Sent Events for live state, range-aware streaming
  throughout (Subsonic/local served directly; YouTube via a natively
  resolved AAC/m4a CDN URL — instant seek, no transcoding).

### Device management

```sh
pixeltui devices               # list paired devices
pixeltui devices revoke <id>   # unpair one (restart a running serve to apply)
```

---

## Offline behavior summary

| Feature | Offline? |
|---|---|
| Local files & a reachable LAN Subsonic | yes — fully |
| Likes, playlists, queue, sessions | yes — all local files |
| Recommendations | yes, via `graph.bin` + cache (build with `build-graph`, top up with `cache warm`) |
| Previously played YouTube tracks | sometimes — stream URLs are cached but expire |
| New YouTube searches / streams | no |
| Lyrics | no (LRCLIB lookup) |
| Scrobbling | deferred — spooled (max 500) and retried on next launch |

---

## Data & privacy

**Everything stays in `~/.pixeltui/` on your machine:**

| Path | What it is |
|---|---|
| `config.json` | configuration (mode `0600` — may hold a password) |
| `cache.db` | bbolt cache of API results + stream URLs |
| `graph.bin` | offline recommendation graph |
| `library/playlists/*.m3u8` | playlists, incl. the reserved "Liked Songs" |
| `library/history.jsonl` | play history (ListenBrainz-style) |
| `library/session.json` | queue + playback state |
| `library/scrobble-spool.jsonl` | offline scrobbles awaiting submission |
| `devices.json` | paired companion devices (tokens hashed at rest) |
| `local-index.json` | local-file scan index (mtime-cached) |
| `bin/`, `ytdlp-venv/`, `mpv.app/` | self-contained tools pixeltui installed |

**What leaves your machine, and only when you use the feature:**

- **YouTube / InnerTube** — search queries and stream resolution for
  YouTube playback, plus chart fetches.
- **Last.fm** — artist/track lookups for recommendations (with your API
  key); scrobbles, now-playing updates, and loves *only if* scrobbling is
  enabled.
- **ListenBrainz** — scrobbles, only if you configured a token.
- **LRCLIB** — track title/artist when you open lyrics (`y`). Free, no
  account.
- **Subsonic** — only your own configured server.
- **GitHub** — `pixeltui update` and `doctor --fix` download releases.

There is no telemetry, no account, and no pixeltui-operated server anywhere
in the loop. The companion server is something *you* run; with Tailscale the
traffic never touches a third party's relay readably (WireGuard end-to-end).

---

## Troubleshooting & FAQ

**First move for almost anything:** `pixeltui doctor`. Every failing row
says how to fix it; `--fix` does the fixable ones for you.

**No pause/seek/volume, and no OS Now Playing.**
mpv is missing — playback fell back to ffplay/afplay, which can't be
controlled. `pixeltui doctor --fix` installs mpv (bundle on macOS,
standalone on Windows, package manager on Linux). Custom location? Set
`PIXELTUI_MPV=/path/to/mpv`.

**Nothing plays at all.**
`doctor` will show `player ✗` — none of mpv/ffplay/afplay was found.
Install any of them (mpv via `--fix` is easiest).

**A specific YouTube track won't play.**
The native resolver handles nearly everything; for the rare exception,
yt-dlp is the fallback — make sure it's installed (`doctor --fix`). A
faster pip build is auto-detected at `~/.pixeltui/ytdlp-venv`, or point
`PIXELTUI_YTDLP` at your own binary.

**Recommendations are empty or repetitive.**
- No Last.fm key → set one in `setup` (free).
- Offline with no graph → `pixeltui build-graph` (once).
- Too samey → raise the discovery level (settings `,`, or `--explore 8`),
  try `--deep-cuts`.
- An artist keeps showing up → `x` mutes them for the session, or
  `--no-artist "Name"` from the CLI.

**Scrobbles aren't appearing on Last.fm.**
`doctor` distinguishes the cases: missing secret, "configured but off"
(toggle in settings), or "not authorized yet" → `pixeltui scrobble-auth`.
Offline plays sit in the spool and submit on next launch.

**Local tracks show filenames instead of titles.**
`ffprobe` (part of ffmpeg) is missing — install ffmpeg and re-browse. The
index refreshes changed files automatically.

**The phone app can't reach the server anymore.**
See [When the address changes](#when-the-address-changes). Quick checklist:
is `serve` running? Same network (or Tailscale connected on both ends)? If
it's a fresh Cloudflare quick-tunnel URL, paste the new address in the app
(Settings → Server → Change Address). Revoked devices and config changes
need a `serve` restart.

**Lyrics are out of sync.**
`[` and `]` nudge the offset, `0` resets. No lyrics at all just means
LRCLIB has no synced lyrics for that track.

**How do I move my library to another machine?**
Copy `~/.pixeltui/library/` (and `config.json` if you want settings).
Plain text files — rsync, git, anything works.

**Update or roll back:** `pixeltui update` / `pixeltui update v0.2.4`.
**Start over:** `pixeltui reset <cache|graph|library|config|all>` — tools
are always kept. **Leave entirely:** `pixeltui uninstall` (add `--keep-data`
to spare library + config).
