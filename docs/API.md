# pixeltui server — HTTP API reference

The pixeltui companion-app server is started with `pixeltui serve`. It exposes the
same library, history, search, and discovery features the TUI uses, over plain
HTTP, so a phone (or any client) can browse and stream from anywhere via a BYO
tunnel (`--tunnel tailscale|cloudflare|ngrok`).

> **Stability:** this API is **unversioned** and may change between releases
> without notice. Treat unknown JSON fields as additive.

- **Base URL:** `http://<host>:8787` by default (`Addr` is configurable; a
  tunnel gives you a public HTTPS URL instead).
- **Transport:** plain REST (JSON) for actions, plus Server-Sent Events at
  `/events` for live state. No WebSockets.
- **CORS:** wide open (`Access-Control-Allow-Origin: *`, methods `GET, POST,
  OPTIONS`, headers `Authorization, Content-Type, Range`; `Content-Length`,
  `Content-Range`, `Accept-Ranges` are exposed). Browser/PWA clients work out
  of the box.
- **Content type:** all JSON responses are `application/json`. Errors are plain
  text bodies with the appropriate HTTP status code.

---

## Authentication

Everything under `/api/*` and `/events` requires a per-device bearer token.
`/health` and `/pair` are unauthenticated.

### Pairing flow

1. `pixeltui serve` prints a 6-character pairing code (alphabet
   `ABCDEFGHJKLMNPQRSTUVWXYZ23456789` — no ambiguous chars) plus a QR code.
2. The QR encodes a deep link:

   ```
   pixeltui://pair?url=<url-encoded base URL>&code=<CODE>
   ```

3. The client exchanges the code for a durable token:

   `POST /pair`

   ```json
   { "code": "AB23CD", "name": "Jordan's iPhone" }
   ```

   The code may also be supplied as `?code=` in the query string. `name` is
   trimmed, defaults to `"device"`, and is truncated to 40 characters.

   **Response `200`:**

   ```json
   { "token": "64-hex-char-token", "device_id": "a1b2c3d4", "server": "my-mac" }
   ```

   **Response `403`** (`bad pairing code`) on a wrong code.

### Code rotation and lockout

- Codes are **single-use**: a successful pair immediately rotates the code, so
  it cannot be replayed for a second device.
- Each consecutive failed attempt adds a delay of `attempts × 300 ms` before
  the error is returned.
- After **5** consecutive bad attempts the code is rotated entirely and the new
  code/QR is reprinted on the server console.

### Using the token

Send it on every request, either way:

- `Authorization: Bearer <token>` (preferred), or
- `?token=<token>` query parameter — for `<audio>`/`<img>` elements and media
  players that cannot set headers (e.g. `/api/stream`, `/api/art`).

Missing/invalid tokens get `401` with body `unauthorized — pair this device first`.

### Token storage

The plaintext token is returned **exactly once** at pairing. The server stores
only its **SHA-256 hash** in `<dataDir>/devices.json` (i.e.
`~/.pixeltui/devices.json`, mode `0600`), so a leaked file cannot be replayed.
Legacy plaintext records are hashed-and-blanked on first load. Token checks are
constant-time; each device's `last_seen` is refreshed at minute granularity.

---

## Common types

### Track object (`trackDTO`)

Returned by every endpoint that lists tracks (inside a `"tracks"` array):

```json
{
  "id": "yt:dQw4w9WgXcQ",
  "track": "Song Title",
  "artist": "Artist Name",
  "album": "Album Name",
  "duration": 213,
  "art": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg",
  "source": "youtube"
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | string | Opaque stream id, `<kind>:<value>` — see [Stream ids](#stream-ids) |
| `track` | string | Title |
| `artist` | string | Artist |
| `album` | string | Omitted when empty |
| `duration` | int | Seconds |
| `art` | string | Absolute URL (YouTube), or server-relative `/api/art?id=...` (Subsonic/local). Omitted when empty |
| `source` | string | `youtube`, `subsonic`, or `local` |

### Track payload (client → server)

Write endpoints (`/api/like`, `/api/playlist/add`, `/api/played`,
`/api/nowplaying`) accept the same shape back:

```json
{ "id": "yt:abc", "track": "...", "artist": "...", "album": "...", "duration": 213, "art": "..." }
```

`id` and `track` are required; the rest is metadata. A round-trip
client → library → client preserves the track unchanged.

### Stream ids

| Prefix | Meaning | Value |
|---|---|---|
| `yt:` | YouTube Music | video id |
| `su:` | Subsonic | song id |
| `lo:` | Local file | base64url-encoded absolute file path |

### Album object (`albumDTO`)

```json
{ "title": "Album", "artist": "Artist", "year": "2021", "browse_id": "MPREb_...", "art": "https://..." }
```

`year` and `art` are omitted when empty. `browse_id` feeds `/api/album`.

---

## Endpoint reference

Auth = bearer token required. All bodies are JSON. Write endpoints are
`POST`-only and return `405` for other methods, `400` (`bad body`) for invalid
JSON, and `503` (`library not available`) when the server runs without a
library.

### Pairing & devices

#### `GET /health` — no auth

```json
{ "ok": true, "name": "my-mac", "service": "pixeltui", "version": "v0.9.0" }
```

#### `POST /pair` — no auth

See [Pairing flow](#pairing-flow).

#### `GET /api/devices` — auth

Lists paired devices (no token material). `current` marks the caller.

```json
{
  "devices": [
    { "id": "a1b2c3d4", "name": "Jordan's iPhone", "created": "2026-06-01T10:00:00Z",
      "last_seen": "2026-06-11T08:30:00Z", "current": true }
  ]
}
```

#### `POST /api/devices/revoke` — auth

Body `{ "id": "a1b2c3d4" }` (or `?id=`). The device's token stops working
immediately.

| Status | Meaning |
|---|---|
| `200` | `{ "ok": true }` |
| `400` | missing id |
| `404` | unknown device |
| `405` | not POST |

---

### Catalog & search

#### `GET /api/sources` — auth

Capabilities and addresses. See [Endpoint advertising](#endpoint-advertising).

```json
{ "sources": ["youtube", "subsonic", "local"], "name": "my-mac",
  "endpoints": ["https://abc.trycloudflare.com", "http://192.168.1.20:8787"] }
```

`youtube` is always present; `subsonic`/`local` appear only when configured.

#### `GET /api/search` — auth

| Param | Type | Default | Notes |
|---|---|---|---|
| `q` | string | — | required; `400` if missing/blank |
| `source` | string | YouTube | `subsonic`, `local`, anything else = YouTube Music |

Returns `{ "tracks": [trackDTO, ...] }` (up to 40). YouTube results are cached
**10 min** (per lowercased query, cap 64 entries). `400` if `source=subsonic`
but Subsonic is not configured; `502` on upstream failure.

#### `GET /api/search/entities` — auth

Artist and album entities for a query (the rails next to track results).

| Param | Type | Notes |
|---|---|---|
| `q` | string | required |

```json
{
  "artists": [ { "name": "Artist", "art": "https://..." } ],
  "albums":  [ { "title": "...", "artist": "...", "year": "2021", "browse_id": "...", "art": "..." } ]
}
```

Up to 8 artists and 10 albums, fetched in parallel. Cached **10 min**.

#### `GET /api/charts` — auth

| Param | Type | Default | Notes |
|---|---|---|---|
| `country` | string | `ZZ` | 2-letter code; `ZZ`/empty = global |

```json
{ "tracks": [trackDTO, ...], "country": "US" }
```

Top 50 from YouTube Music. Cached **30 min** per country (cap 16). `502` on
upstream failure.

#### `GET /api/artist` — auth

Full artist page resolved by name.

| Param | Type | Notes |
|---|---|---|
| `name` | string | required |

```json
{
  "name": "Artist",
  "top_songs": [trackDTO, ...],
  "albums":  [albumDTO, ...],
  "singles": [albumDTO, ...],
  "stats": { "listeners": 1200000, "playcount": 53000000,
             "tags": ["indie rock", "indie"], "bio": "..." }
}
```

`stats` (Last.fm listeners/playcount/up-to-4 tags/bio) appears only when the
server has a Last.fm key. Cached **1 h** per lowercased name (cap 64). `404` if
no artist matches; `502` on upstream failure.

#### `GET /api/album` — auth

| Param | Type | Notes |
|---|---|---|
| `browse_id` | string | from an artist page or album search |
| `title` | string | used (with `artist`) to resolve the album when `browse_id` is absent |
| `artist` | string | optional, improves resolution |

`400` if neither `browse_id` nor `title` given; `404` if resolution finds
nothing.

```json
{ "title": "Album", "artist": "Artist", "year": "2021", "art": "https://...",
  "tracks": [trackDTO, ...] }
```

Up to 60 ordered tracks. If the album header has no cover, the first track's
thumbnail is used. Cached **24 h** per `browse_id` (cap 64).

#### `GET /api/lyrics` — auth

| Param | Type | Notes |
|---|---|---|
| `artist` | string | at least one of `artist`/`track` required |
| `track` | string | |
| `duration` | int (sec) | optional, improves LRCLIB matching |
| `id` | string | optional `yt:<videoid>` — enables YouTube Music plain-text fallback |

```json
{
  "synced": [ { "t": 12.3, "text": "First line" } ],
  "plain": "First line\nSecond line"
}
```

`synced` is empty when only plain lyrics exist; both can be empty (lyrics not
found is still `200`). Cached **24 h** per `artist|track` (cap 200) — only
non-empty results are cached, so transient upstream failures don't blank a
track's lyrics for the TTL.

---

### Library

All library data is the same on-disk library the TUI uses
(`~/.pixeltui/library` M3U8 files): one shared library across TUI and every
client. Last write wins per playlist file. Every successful write broadcasts an
SSE `library` event.

#### `GET /api/liked` — auth

`{ "tracks": [trackDTO, ...] }` — the Liked Songs playlist (empty array if no
library).

#### `POST /api/like` — auth

Body: track payload plus `liked`:

```json
{ "liked": true, "id": "yt:abc", "track": "...", "artist": "...", "album": "...", "duration": 213, "art": "..." }
```

`liked: true` likes (and mirrors the like to Last.fm "love" when a scrobbler is
configured), `false` unlikes. Response: `{ "ok": true, "liked": true }`.
`400` on a bad/unknown track id or missing title.

#### `GET /api/playlists` — auth

`{ "playlists": ["Road Trip", "Focus"] }` — names only.

#### `GET /api/playlist?name=<name>` — auth

`{ "tracks": [trackDTO, ...] }`. `400` if `name` is missing.

#### `POST /api/playlist/create` — auth

Body `{ "name": "Road Trip" }`. `400` for empty names or the reserved Liked
Songs name. Response `{ "ok": true }`.

#### `POST /api/playlist/rename` — auth

Body `{ "name": "Old", "new_name": "New" }`. `400` if either side is empty or
is Liked Songs. Response `{ "ok": true }`.

#### `POST /api/playlist/delete` — auth

Body `{ "name": "Road Trip" }`. `400` for Liked Songs. Response `{ "ok": true }`.

#### `POST /api/playlist/add` — auth

Body: `{ "name": "Road Trip", ...track payload }`. Appends to the playlist
(created if missing), deduplicated by track id — adding a duplicate is a
no-op `{ "ok": true }`. `400` for a bad name (empty or Liked Songs) or bad
track payload.

#### `POST /api/playlist/remove` — auth

Body `{ "name": "Road Trip", "ids": ["yt:abc", "su:42"] }`. Removes all
matching tracks. `400` if `name` is Liked Songs (use `/api/like`); `404` if the
playlist doesn't exist. Response `{ "ok": true }`.

#### `GET /api/local` — auth

`{ "tracks": [trackDTO, ...] }` — the scanned local-files index (served from
the on-disk scan cache, scanning on demand if cold).

#### Subsonic passthrough — auth, all `400` if Subsonic isn't configured

| Endpoint | Returns |
|---|---|
| `GET /api/subsonic/starred` | `{ "tracks": [...] }` — starred songs |
| `GET /api/subsonic/playlists` | `{ "playlists": [...] }` — server playlist objects |
| `GET /api/subsonic/playlist?id=<id>` | `{ "tracks": [...] }`; `400` if `id` missing |

---

### History & stats

Plays reported here land in the same `history.jsonl` the TUI writes — feeding
Recently Played, stats, recommendations, and scrobbling exactly as if the track
had played in the TUI.

#### `POST /api/nowplaying` — auth

Body: track payload. Announces the current track to configured scrobble
services (fire-and-forget; nothing is recorded). Response `{ "ok": true }`.

#### `POST /api/played` — auth

Records **one qualified play**. The **client enforces the 50%-or-4-minutes
rule** (same as the TUI) — the server records whatever you send, so only call
this once a play qualifies.

Body: track payload plus optional `started_at` (unix seconds; `0`/absent =
now):

```json
{ "id": "yt:abc", "track": "...", "artist": "...", "duration": 213, "started_at": 1760000000 }
```

Appends to shared history, scrobbles to every configured service (async,
spooled on failure), and broadcasts an SSE `library` event with data
`"history"`. Response `{ "ok": true }`.

#### `GET /api/history` — auth

| Param | Type | Default | Notes |
|---|---|---|---|
| `limit` | int | 50 | max 500 |
| `unique` | `1` | off | collapse repeat plays of the same artist+track |

```json
{ "tracks": [ { "id": "yt:abc", "track": "...", "artist": "...", "duration": 213,
                "art": "...", "source": "youtube", "played_at": 1760000000 } ] }
```

Most-recent first; each entry is a track object plus `played_at` (unix
seconds).

#### `GET /api/stats` — auth

| Param | Type | Default | Notes |
|---|---|---|---|
| `days` | int | 0 | window in days; `0` = all time |

```json
{
  "days": 30,
  "plays": 412,
  "unique_tracks": 180,
  "unique_artists": 64,
  "seconds": 91200,
  "top_artists": [ { "name": "Artist", "plays": 42 } ],
  "top_tracks":  [ { "name": "Song", "artist": "Artist", "plays": 17,
                     "art": "https://...", "id": "yt:abc" } ]
}
```

Top lists hold up to 10 entries; `art` and `id` (a playable stream id) are
included for tracks when known. `503` if no library.

---

### Discovery

These need server-side configuration: `/api/radio` only needs YouTube;
`/api/recommend`, `/api/mixes`, `/api/station` return **`503`** when the
server has no Last.fm key (set one with `pixeltui setup`).

#### `GET /api/radio` — auth

YouTube Music's native radio (watch playlist) for a seed track.

| Param | Type | Default | Notes |
|---|---|---|---|
| `id` | string | — | required, must be `yt:<videoid>`; `400` otherwise |
| `n` | int | 25 | clamped to 1–50 |
| `exclude` | string | — | comma-separated artist names to drop ("mute artist") |

`{ "tracks": [trackDTO, ...] }` — the seed track itself is removed. Cached
**10 min** per `id|n|exclude` (cap 64). `502` on upstream failure.

#### `GET /api/recommend` — auth

pixeltui's own recommendation engine, resolved to playable YouTube tracks.

| Param | Type | Default | Notes |
|---|---|---|---|
| `seed` | string, repeatable | — | `Artist\|Track` per value; up to 4 used |
| `artist`, `track` | string | — | single-seed shorthand, used when no `seed` given |
| `n` | int | 20 | clamped to 1–40 |
| `exclude` | string | — | comma-separated artists to drop |

Seed precedence: explicit `seed` params → `artist`/`track` → up to 4 random
liked tracks from the shared library. `404` (`no seeds — like some tracks
first`) when nothing seeds. Unresolvable candidates are dropped, so fewer than
`n` tracks may return.

`{ "tracks": [trackDTO, ...] }`. Cached **30 min** per seeds+n+exclude (cap 8).
`502` on engine failure, `503` without a Last.fm key.

#### `GET /api/mixes` — auth

Daily mixes: your most-played artists (last 90 days of history + likes, a like
counts as 2 plays) grouped by their dominant Last.fm tag; the top 4 tags each
become a multi-seed mix.

```json
{
  "mixes": [
    { "title": "Indie Rock Mix", "tag": "indie rock", "tracks": [trackDTO, ...] }
  ]
}
```

Up to 4 mixes of ~25 tracks (mixes with fewer than 5 playable tracks are
dropped). `{ "mixes": [] }` when there's no listening data. Cached **24 h**
(non-empty results only). `503` without Last.fm key or library.

#### `GET /api/station` — auth

A genre station for one Last.fm tag.

| Param | Type | Notes |
|---|---|---|
| `tag` | string | required (Last.fm tag, e.g. `shoegaze`); `400` if missing |

Seeds come from your own listening when ≥2 of your top artists match the tag,
otherwise from Last.fm's tag chart. `404` if no artists can be found for the
tag.

```json
{ "tag": "shoegaze", "tracks": [trackDTO, ...] }
```

Up to ~30 tracks. Cached **1 h** per tag (cap 16, non-empty only). `502` on
engine failure, `503` without a Last.fm key.

---

### Playback & streaming

#### `GET /api/stream?id=<stream id>` — auth (use `?token=` for media players)

Serves audio for any track id:

| Prefix | Behavior |
|---|---|
| `lo:` | The base64url-decoded path is validated (must be an audio file under a configured local dir, else `403`), then served with `http.ServeFile` — fully range-aware. |
| `su:` | Proxied to the Subsonic server's stream URL; your `Range` header is forwarded and `Content-Type`/`Content-Length`/`Accept-Ranges`/`Content-Range` are passed back. Subsonic credentials never reach the client. `400` if Subsonic isn't configured. |
| `yt:` | Resolved to a pre-signed AAC/m4a CDN URL (native InnerTube, ~0.2 s; yt-dlp fallback) and proxied. See below. |

`400` (`bad id` / `unknown source`) for malformed ids.

**YouTube range behavior** — two paths:

- **Request has a `Range` header** (normal playback): plain proxy of that
  range. Clients get `Content-Length` + byte ranges → instant seeking and
  proper buffering. No transcoding (iOS/Android play AAC natively).
- **No `Range` header** (a full-file download): served as one continuous `200`
  response **relayed in sequential 2 MiB ranged chunks** from the CDN, because
  googlevideo throttles un-ranged transfers to ~32 KB/s while ranged chunks
  run at full speed. `Content-Length` is set from the first chunk's
  `Content-Range` total. If the relay breaks mid-stream the client sees a
  short body.

Resolved CDN URLs are cached on disk until the URL's own `expire=` timestamp;
concurrent range bursts (AVPlayer) collapse onto a single resolution
(singleflight). If no m4a rendition exists, a legacy fallback transcodes via
`yt-dlp | ffmpeg` to ADTS AAC (`audio/aac`, no ranges, no seeking); `503` if
yt-dlp/ffmpeg are missing on the server.

#### `GET /api/art?id=<stream id>` — auth

Cover art, **only** for `su:` and `lo:` ids — YouTube tracks carry a public
thumbnail URL directly in `track.art`, never via this endpoint.

| Prefix | Behavior |
|---|---|
| `su:` | Proxied from the Subsonic cover-art endpoint (`id` here is the cover id from the track payload, already wired into `track.art`). |
| `lo:` | The file's embedded cover, extracted with ffmpeg on first request and cached as JPEG under `<dataDir>/artcache`. Served with `Cache-Control: max-age=86400`. `404` if there's no embedded art or ffmpeg is missing (negative results are cached too). `403` for paths outside configured dirs. |

`400` (`bad id`) otherwise.

---

### Events (SSE)

#### `GET /events` — auth

Standard Server-Sent Events stream (`Content-Type: text/event-stream`).

On connect:

```
event: hello
data: "my-mac"
```

Then, whenever the library changes:

```
event: library
data: "liked"
```

`data` is a JSON-quoted string hinting what changed: `"liked"`, `"playlists"`,
`"history"`, or `"library"` (generic). Treat it as a **hint to refetch** — the
event carries no payload, and slow clients may drop events (each connection
has an 8-message buffer).

Events fire from:

- **Client writes** — like/unlike, playlist create/rename/delete/add/remove,
  and `/api/played` broadcast immediately.
- **TUI (or external) edits** — the server polls the library directory's
  file mtimes/sizes every **3 seconds** and broadcasts `data: "library"` when
  anything changed.

Keepalive: a comment line `: ping` is sent every **20 seconds** so proxies
don't kill idle connections.

---

## Endpoint advertising

Quick tunnels (cloudflare/ngrok) mint a **new URL on every server start**, so
a stored base URL can go stale. `/api/sources` advertises every address the
server currently answers on:

```json
{ "sources": ["youtube"], "name": "my-mac",
  "endpoints": ["https://random-words.trycloudflare.com", "http://192.168.1.20:8787"] }
```

The public/tunnel URL (when configured) comes first, followed by the LAN
address. Clients should:

1. After pairing (and periodically), fetch `/api/sources` and **persist the
   `endpoints` list**.
2. When the stored address stops responding, **walk the saved list** until one
   answers — the LAN address keeps working at home even after a tunnel URL
   rotates.
3. Re-save the list whenever a fetch succeeds.

---

## Building a client — checklist

1. **Pair**: scan the QR (`pixeltui://pair?url=...&code=...`) or let the user
   type URL + code; `POST /pair` with `{code, name}`.
2. **Store the token** securely (Keychain/Keystore) — it's shown exactly once.
   Send it as `Authorization: Bearer ...` (or `?token=` for media URLs).
3. **Fetch `/api/sources`** — learn available sources and save the `endpoints`
   list for address fallback.
4. **Browse the catalog**: `/api/liked`, `/api/playlists` + `/api/playlist`,
   `/api/search` + `/api/search/entities`, `/api/charts`, `/api/artist`,
   `/api/album`, `/api/local`, `/api/subsonic/*`.
5. **Play**: feed `/api/stream?id=...&token=...` to your audio player; use
   `track.art` (absolute, or server-relative `/api/art` for su:/lo:) for
   covers; `/api/lyrics` for lyrics.
6. **Report plays**: `POST /api/nowplaying` when a track starts, and `POST
   /api/played` once it qualifies — **the 50%-or-4-minutes rule is enforced by
   the client**, the server records whatever you send.
7. **Listen on `/events`** and refetch the relevant data on `library` events,
   so likes and playlist edits made in the TUI (or other devices) show up
   live.
8. **Discover**: `/api/radio` for autoplay/up-next, `/api/recommend` (seeded)
   for stations, `/api/mixes` for daily mixes, `/api/station?tag=` for genre
   stations.
