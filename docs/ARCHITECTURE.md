# pixeltui — architecture

A developer's map of the system: what the pieces are, where truth lives, how
data flows, and where the concurrency is. For the user-facing guide see
[GUIDE.md](GUIDE.md); for the companion-app HTTP API see [API.md](API.md).

## The big picture

pixeltui is a **hub-and-spoke** system. One Go binary contains everything: the
terminal player (the hub), the CLI commands, and the companion-app server (a
door into the same hub). The iOS app (PixelPal, `clients/ios`) is a remote client
of that server. There is exactly **one source of truth** — the `~/.pixeltui/`
directory on the machine running the TUI/server — and everything else is a
cache or a view of it.

```
                       ┌────────────────────────────────────────────┐
                       │              pixeltui binary               │
                       │                                            │
  ┌────────────┐        │  ┌────────┐   ┌─────────┐   ┌───────────┐  │
  │  iOS app   │  HTTP   │  │ server │   │   TUI   │   │    CLI    │  │
  │ (PixelPal) │◄───────►│  │ serve  │   │ player  │   │ commands  │  │
  └────────────┘  + SSE  │  └───┬────┘   └────┬────┘   └─────┬─────┘  │
                       │      │             │              │        │
                       │      ▼             ▼              ▼        │
                       │  ┌──────────────────────────────────────┐  │
                       │  │      shared packages (see below)     │  │
                       │  └──────────────────┬───────────────────┘  │
                       └─────────────────────┼──────────────────────┘
                                             ▼
                                      ~/.pixeltui/
                                  (the source of truth)
```

External services: **YouTube Music** (search/metadata/streams, unauthenticated),
**Last.fm** (similarity data for recommendations; scrobbling), **ListenBrainz**
(scrobbling), **LRCLIB** (lyrics), and optionally a **Subsonic/Navidrome**
server and **local folders**.

## Packages

All Go code lives under `tui/` (module `github.com/dotjarden/pixeltui`).

| Package | Responsibility |
|---|---|
| `tui/` (main) | CLI entry point and every subcommand: player launch, `setup`, `serve`, `build-graph`, `cache`, `doctor`, `update`, `devices`, `export`, `reset`, `uninstall`. |
| `tui/tui` | The interactive player: a Bubble Tea (Elm-style) state machine — search, browse, queue, playlists, lyrics, charts, settings — plus playback process management (mpv/ffplay, IPC control socket). |
| `tui/server` | The companion-app HTTP server: ~40 REST endpoints + SSE, pairing/auth, streaming proxy/relay, discovery (mixes/stations/radio/recommendations). See [API.md](API.md). |
| `tui/engine` | Recommendation scorer. Signals: similarity 0.40, artist-novelty 0.35, popularity 0.20, serendipity 0.05; liked-artist affinity boost; max 2 tracks per artist. |
| `tui/store` | Data layer behind the engine: the **hybrid** four-layer source (static graph → fresh cache → live Last.fm → stale cache), the bbolt cache (`cache.db`), the gob+gzip graph (`graph.bin`), and the parallel BFS graph builder. |
| `tui/innertube` | Native YouTube stream resolution via the InnerTube `/player` endpoint (ANDROID_VR client → pre-signed CDN URLs, no signature cipher). One stdlib HTTP call, ~0.2s. Used by both the TUI player and the server; yt-dlp is only a fallback at call sites. |
| `tui/library` | The portable on-disk library: M3U8 playlists (incl. the reserved "Liked Songs"), append-only `history.jsonl`, `session.json`. Mutex-guarded writes; last-write-wins at file granularity. |
| `tui/lastfm` | Read-only Last.fm API client (similar tracks/artists, top tracks, tags, artist info). Connection-pooled, gzip. |
| `tui/ytm` | YouTube Music metadata: search, resolve (artist/track → video id), radio, charts, artist/album pages, plain lyrics. Unauthenticated. |
| `tui/subsonic` | Minimal OpenSubsonic client (Navidrome, Airsonic…): browse, search, starred, playlists, direct stream URLs. Stable per-client salted token auth. |
| `tui/local` | Local-folder indexing: ffprobe tags (filename fallback), mtime-keyed JSON index so unchanged files are never re-probed. |
| `tui/scrobble` | Fan-out scrobbling to Last.fm + ListenBrainz: async now-playing/scrobble/love, offline JSONL spool (max 500) retried on next launch. |
| `tui/lyrics` | Synced (LRC) + plain lyrics from LRCLIB. |
| `tui/download` | yt-dlp-driven downloads with embedded tags + cover art, written in `Artist/Album/Title.ext` layout (Subsonic-ready). |
| `tui/config` | `~/.pixeltui/config.json` load/save with env-var overrides. Written 0600 (may hold a Subsonic password). |

## Source of truth

Everything authoritative lives in `~/.pixeltui/`:

| Data | File | Written by | Read by |
|---|---|---|---|
| Likes & playlists | `library/playlists/*.m3u8` | TUI directly; clients via server | TUI, server, engine (affinity) |
| Play history | `library/history.jsonl` | TUI on scrobble; server on `/api/played` | stats, history views, mix seeding |
| Session / Up Next | `library/session.json` | TUI on exit | TUI on launch |
| Scrobble spool | `library/scrobble-spool.jsonl` | scrobbler on failure | scrobbler retry on launch |
| Config & credentials | `config.json` | `setup`, `scrobble-auth` | everything |
| Last.fm API cache | `cache.db` (bbolt) | hybrid store, `cache warm` | engine; also stores resolved stream URLs |
| Artist graph | `graph.bin` | `build-graph` | engine (instant offline lookups) |
| Local-file index | `local-index.json` | local scanner | local source |
| Paired devices | `devices.json` (SHA-256 token hashes) | server pair/revoke | server auth, `devices` CLI |

The iOS app keeps **mirrors, never truth**: a catalog snapshot for offline
browsing, likes/playlists in UserDefaults, the token in the Keychain, downloads
and art on disk. On every refresh the server's state wins; offline edits are
journaled and replayed *before* the authoritative refresh so they aren't lost.

Concurrent writers (TUI + several clients) are serialized per file; conflicts
resolve last-write-wins at M3U8 granularity. Fine for a personal library;
two people editing the same playlist simultaneously would lose one edit.

## Data flow for the key tasks

### Playing a track (TUI)

1. **Search/browse** → `ytm.Search` (or Subsonic/local) fills candidates.
2. **Resolve** — `resolveStreamURL` (tui/tui/player.go):
   on-disk URL cache → **`innertube.Resolve`** (~0.2s, native) → yt-dlp
   fallback (only for rare playability quirks).
3. **Play** — mpv with an IPC socket (pause/seek/volume/OS Now Playing), else
   ffplay/afplay fallbacks. Subsonic/local tracks skip resolution entirely
   (they already carry a playable URL/path).
4. **Preload** — as you move the cursor, the next track's video id, CDN URL,
   and pixelated cover are resolved in the background, so play is instant.

### Playing a track (iOS via server)

AVPlayer hits `/api/stream?id=yt:<video>`. The server resolves via the same
`innertube` package behind a singleflight group (AVPlayer fires a burst of
range requests at track start; they collapse onto one resolution), caches the
URL until its expiry, and proxies range-aware. Full downloads (no `Range`
header) are relayed as sequential 2 MiB upstream chunks because GoogleVideo
throttles un-ranged transfers to a crawl.

### Liking a track

TUI writes the M3U8 + async Last.fm love. Clients POST `/api/like`; the server
writes the same file, broadcasts an SSE `library` event, and loves on Last.fm.
TUI edits bypass the server, so a 3-second mtime-poll file watcher
(`tui/server/watch.go`) broadcasts SSE when the library directory changes —
that's how a like made in the terminal shows up on the phone in ~5s.

### Scrobbling

Both clients enforce the 50%-or-4-minute rule. The TUI scrobbles itself
(async, spooled offline). The phone never talks to Last.fm: it reports plays
to `/api/played` and the server appends to the shared history + scrobbles. One
history, one Last.fm stream, regardless of where you listened.

### Recommendations

`engine.Recommender` over the hybrid store: fetch seed metadata → build a
candidate pool (parallel fan-out over similar artists/tracks) → score → cap
per-artist → top N. Multi-seed blending (up to 4 seeds) powers stations and
the server's `/api/recommend`. Daily mixes cluster your last-90-days + liked
artists by dominant Last.fm tag (concurrent tag lookups, semaphore 6) into up
to 4 tag mixes. Fully offline operation works off `graph.bin` + stale cache.

## Concurrency map

- **TUI**: preload goroutines (debounced), parallel chart fetches per country,
  async lyrics/recs, fire-and-forget scrobbles.
- **Engine/store**: parallel seed expansion; graph builder = token-bucket
  rate-limited worker pool (resumable BFS, partial save on Ctrl-C).
- **Server**: singleflight on stream resolution; 9 in-memory TTL caches
  (10m–24h) for search/lyrics/charts/artist/album/radio/recs/mixes/stations;
  semaphore-bounded fan-out (6–8) when resolving bare recommendations to
  playable tracks; SSE hub with per-client channels; mutexes around pairing
  state, device store, visitor token, and each cache.
- **iOS**: everything `async/await` on `@MainActor` stores; catalog refresh
  runs 8 concurrent fetches; image pipeline dedups in-flight downloads and
  decodes only the needed thumbnail bucket (160px/640px) via ImageIO.

## Security model (server)

- Pairing: single-use 6-char code (ambiguity-free alphabet), rotated after
  success or 5 failures (with increasing delay). QR deep link
  `pixeltui://pair?url=…&code=…`.
- Tokens: 256-bit random, stored **hashed** (SHA-256) in `devices.json`,
  constant-time compared, revocable per device.
- Credential isolation: Subsonic credentials never reach clients — tracks are
  re-keyed as `su:<id>` and the server proxies streams/art itself.
- Transport: plain HTTP by design — run it on a LAN or inside a private
  tunnel (Tailscale recommended); public tunnels leave the bearer token as
  the only gate.

## Known trade-offs & watch items

- **InnerTube is unofficial.** YouTube can change it at any time; that's why
  the yt-dlp fallback stays at both call sites and `doctor --fix` can still
  install it. Symptom of breakage: streams fail natively, then succeed via
  yt-dlp (slower).
- **HE-AAC (itag 139) is avoided** unless it's all there is — its SBR
  half-rate base makes players report doubled duration with trailing silence.
- **Last-write-wins library sync** (per file). Acceptable single-user; not a
  multi-user merge.
- **3s watcher poll** is coarse but cheap; rapid TUI edits within a window
  coalesce into one SSE event.
- **Downloads still require yt-dlp** (tag/art embedding via its pipeline).
  Playback does not.

## Building & testing

```sh
make build          # → ./pixeltui
go test ./tui/...   # unit tests across config/library/engine/server/…
make release        # cross-compile darwin/linux/windows, amd64+arm64 → dist/
```

Versioning is injected with `-X main.version` (see Makefile); releases are
tagged `v*` and built by CI (`.github/workflows/release.yml`), which is what
`pixeltui update` pulls from.
