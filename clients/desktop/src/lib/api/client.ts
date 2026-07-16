// Typed HTTP client for the embedded pixeltui server. Mirrors iOS
// PixeltuiAPI.swift: all `/api/*` calls carry `Authorization: Bearer <token>`;
// media elements (`<audio>`, `<img>`) can't set headers, so `/api/stream` and
// `/api/art` use `?token=` via `mediaUrl()`.
//
// Base URL + token are sourced from the `$lib/server` stores populated during
// boot (see sidecar orchestration / `get_token`).

import { get } from 'svelte/store';
import { baseUrl, token } from '$lib/server';

export class ApiError extends Error {
	readonly status: number;
	readonly body?: unknown;
	constructor(status: number, message: string, body?: unknown) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.body = body;
	}
}

// A route can be reached from both a rail and a detail transition. Share the
// same in-flight GET instead of making two identical server requests.
const inflightGets = new Map<string, Promise<unknown>>();
const responseCache = new Map<string, { expiresAt: number; value: unknown }>();
const REQUEST_TIMEOUT_MS = 15_000;
const ARTIST_RESOLVE_CONCURRENCY = 3;
let activeArtistResolves = 0;
const queuedArtistResolves: (() => void)[] = [];

// Recommendation circles enrich in the background. Keep those best-effort
// requests from flooding the same upstream provider that serves the page the
// user just clicked into.
function resolveArtistWhenReady<T>(request: () => Promise<T>, urgent = false): Promise<T> {
	return new Promise<T>((resolve, reject) => {
		const start = () => {
			activeArtistResolves += 1;
			request().then(resolve, reject).finally(() => {
				activeArtistResolves -= 1;
				queuedArtistResolves.shift()?.();
			});
		};
		if (activeArtistResolves < ARTIST_RESOLVE_CONCURRENCY) start();
		else if (urgent) queuedArtistResolves.unshift(start);
		else queuedArtistResolves.push(start);
	});
}

function cacheTtl(path: string): number {
	if (path.startsWith('/api/search')) return 30_000;
	if (path.startsWith('/api/charts')) return 5 * 60_000;
	if (path.startsWith('/api/artist') || path.startsWith('/api/album')) return 10 * 60_000;
	if (path.startsWith('/api/trackinfo')) return 10 * 60_000;
	if (path.startsWith('/api/lyrics')) return 10 * 60_000;
	if (path.startsWith('/api/mixes') || path.startsWith('/api/station')) return 5 * 60_000;
	return 0;
}

/** Headers with the bearer token attached when a token is present. */
function authHeaders(): Record<string, string> {
	const t = get(token);
	return t ? { Authorization: `Bearer ${t}` } : {};
}

/**
 * Perform an authenticated JSON (or text) request against the embedded server.
 * Throws {@link ApiError} on non-2xx. 204 returns undefined. Non-JSON bodies
 * (e.g. plain text) are returned as strings.
 */
export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
	const base = get(baseUrl);
	if (!base) throw new Error('server base URL not ready');
	const method = (init.method ?? 'GET').toUpperCase();
	const requestKey = `${method}:${base}${path}`;
	if (method === 'GET') {
		const cached = responseCache.get(requestKey);
		if (cached && cached.expiresAt > Date.now()) return cached.value as T;
		if (cached) responseCache.delete(requestKey);
		const existing = inflightGets.get(requestKey);
		if (existing) return existing as Promise<T>;
	}
	const headers: Record<string, string> = {
		...authHeaders(),
		...((init.headers as Record<string, string> | undefined) ?? {})
	};
	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
	const request = (async () => {
		try {
			const res = await fetch(`${base}${path}`, { ...init, headers, signal: init.signal ?? controller.signal });
			if (!res.ok) {
				let body: unknown;
				try { body = await res.text(); } catch { /* ignore */ }
				throw new ApiError(res.status, `${res.status} ${res.statusText} ${path}`, body);
			}
			if (res.status === 204) return undefined as T;
			const ct = res.headers.get('content-type') ?? '';
			const value = ct.includes('application/json') ? await res.json() : await res.text();
			const ttl = method === 'GET' ? cacheTtl(path) : 0;
			if (ttl) responseCache.set(requestKey, { expiresAt: Date.now() + ttl, value });
			return value as T;
		} finally {
			clearTimeout(timeout);
		}
	})();
	if (method === 'GET') {
		inflightGets.set(requestKey, request);
		request.finally(() => inflightGets.delete(requestKey)).catch(() => {});
	}
	return request;
}

/** POST a JSON body. */
export function apiPost<T>(path: string, body: unknown): Promise<T> {
	return apiRequest<T>(path, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body)
	});
}

/** DELETE with optional JSON body. */
export function apiDelete<T>(path: string, body?: unknown): Promise<T> {
	return apiRequest<T>(path, {
		method: 'DELETE',
		headers: body ? { 'Content-Type': 'application/json' } : undefined,
		body: body ? JSON.stringify(body) : undefined
	});
}

/**
 * URL for media elements that cannot set headers (`<audio>`, `<img>`): appends
 * `?token=` (or `&token=`) to the path. Used for `/api/stream` and `/api/art`.
 */
export function mediaUrl(path: string): string {
	const base = get(baseUrl);
	const t = get(token);
	const sep = path.includes('?') ? '&' : '?';
	return `${base}${path}${t ? `${sep}token=${encodeURIComponent(t)}` : ''}`;
}

import type {
	Album,
	AlbumPage,
	ArtistHit,
	ArtistPage,
	ArtistPageExtras,
	Charts,
	Device,
	Entities,
	HistoryEntry,
	IdentifyResult,
	LikeResult,
	Lyrics,
	PairingInfo,
	Mix,
	Ok,
	PartySnapshot,
	Sources,
	Station,
	Stats,
	SubsonicPlaylist,
	Track,
	TrackInfo,
	TrackInfoYouTube,
	TrackPayload
} from './types';

/** Build a query string, omitting empty/undefined values. Array values repeat the key. */
function qs(params: Record<string, string | number | boolean | string[] | undefined>): string {
	const u = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v === undefined || v === '' || v === false) continue;
		if (Array.isArray(v)) {
			for (const item of v) u.append(k, String(item));
		} else {
			u.set(k, String(v));
		}
	}
	const s = u.toString();
	return s ? `?${s}` : '';
}

/** Convert a {@link Track} to the persisted write payload. */
export function toPayload(t: Track): TrackPayload {
	return {
		id: t.id,
		track: t.track,
		artist: t.artist,
		album: t.album ?? '',
		duration: t.duration,
		art: t.art ?? ''
	};
}

/**
 * Resolve an art reference to a loadable URL. Absolute URLs (YouTube thumbnails)
 * are public and returned as-is; server-relative refs (`/api/art?id=…`) get the
 * base URL + `?token=` so `<img>` can fetch them without headers.
 */
export function artUrl(ref?: string, eager = false): string {
	if (!ref) return '';
	const normalized = upsizeArtRef(ref, eager);
	if (/^https?:\/\//i.test(normalized)) return normalized;
	return mediaUrl(normalized);
}

/** Match iOS artwork quality normalization before the browser requests a cover. */
function upsizeArtRef(ref: string, eager: boolean): string {
	if (ref.includes('i.ytimg.com')) {
		let out = ref.split('?')[0];
		const target = eager ? 'hq720' : 'hqdefault';
		out = out.replace(/\/(?:sddefault|hqdefault|mqdefault|maxresdefault|hq720|default)\.jpg$/, `/${target}.jpg`);
		return out;
	}
	if (ref.includes('googleusercontent.com') || ref.includes('ggpht.com')) {
		return ref.replace(/=w\d+-h\d+/, '=w720-h720');
	}
	return ref;
}

/** Full `/api/stream` URL for an `<audio>` element (appends `?token=`). */
export function streamUrl(id: string): string {
	return mediaUrl(`/api/stream?id=${encodeURIComponent(id)}`);
}

// ── endpoint surface (mirrors iOS PixeltuiAPI.swift) ────────────────────────

async function tracks(path: string): Promise<Track[]> {
	return (await apiRequest<{ tracks: Track[] }>(path)).tracks;
}

export const api = {
	// sources / health
	sources: () => apiRequest<Sources>('/api/sources'),
	pairing: () => apiRequest<PairingInfo>('/api/pairing'),

	// library reads
	liked: () => tracks('/api/liked'),
	playlists: async () => (await apiRequest<{ playlists: string[] }>('/api/playlists')).playlists,
	playlistTracks: (name: string) => tracks(`/api/playlist${qs({ name })}`),
	localTracks: () => tracks('/api/local'),
	subsonicStarred: () => tracks('/api/subsonic/starred'),
	subsonicPlaylists: async () =>
		(await apiRequest<{ playlists: SubsonicPlaylist[] }>('/api/subsonic/playlists')).playlists,
	subsonicPlaylistTracks: (id: string) => tracks(`/api/subsonic/playlist${qs({ id })}`),

	// library writes
	setLike: (t: Track, liked: boolean) =>
		apiPost<LikeResult>('/api/like', { liked, ...toPayload(t) }),
	playlistCreate: (name: string) => apiPost<Ok>('/api/playlist/create', { name }),
	playlistRename: (name: string, new_name: string) =>
		apiPost<Ok>('/api/playlist/rename', { name, new_name }),
	playlistDelete: (name: string) => apiPost<Ok>('/api/playlist/delete', { name }),
	playlistAdd: (name: string, t: Track) => apiPost<Ok>('/api/playlist/add', { name, ...toPayload(t) }),
	playlistRemove: (name: string, ids: string[]) =>
		apiPost<Ok>('/api/playlist/remove', { name, ids }),

	// search / browse
	search: (q: string, source = 'youtube') => tracks(`/api/search${qs({ q, source })}`),
	searchEntities: (q: string) =>
		apiRequest<Entities>(`/api/search/entities${qs({ q })}`),
	artistPage: (name: string, fast = false, browseId = '', art = '') =>
		apiRequest<ArtistPage>(`/api/artist${qs({ name, fast: fast ? 1 : undefined, browse_id: browseId, art })}`),
	artistExtras: (name: string) => apiRequest<ArtistPageExtras>(`/api/artist/extras${qs({ name })}`),
	resolveArtist: (name: string, urgent = false) =>
		resolveArtistWhenReady(() => apiRequest<ArtistHit>(`/api/artist/resolve${qs({ name })}`), urgent),
	albumPage: (browse_id: string, title: string, artist: string) =>
		apiRequest<AlbumPage>(`/api/album${qs({ browse_id, title, artist })}`),

	// discovery
	charts: (country = '') => apiRequest<Charts>(`/api/charts${qs({ country })}`),
	radio: (id: string, n = 25, exclude: string[] = []) =>
		tracks(`/api/radio${qs({ id, n, exclude: exclude.join(',') })}`),
	recommendations: (
		n = 20,
		seeds: { artist?: string; track?: string }[] = [],
		exclude: string[] = []
	): Promise<Track[]> => {
		const u = new URLSearchParams();
		if (n) u.set('n', String(n));
		for (const s of seeds.slice(0, 4)) u.append('seed', `${s.artist ?? ''}|${s.track ?? ''}`);
		if (exclude.length) u.set('exclude', exclude.join(','));
		return tracks(`/api/recommend?${u.toString()}`);
	},
	mixes: async () => (await apiRequest<{ mixes: Mix[] }>('/api/mixes')).mixes,
	station: (tag: string) => apiRequest<Station>(`/api/station${qs({ tag })}`),

	// playback / scrobble
	nowPlaying: (t: Track) => apiPost<Ok>('/api/nowplaying', toPayload(t)),
	played: (t: Track, startedAt: number = 0) =>
		apiPost<Ok>('/api/played', { ...toPayload(t), started_at: startedAt }),

	// history / stats
	history: (limit = 50, unique = false): Promise<HistoryEntry[]> =>
		apiRequest<{ tracks: HistoryEntry[] }>(
			`/api/history${qs({ limit, unique: unique ? 1 : undefined })}`
		).then((r) => r.tracks),
	stats: (days = 0) => apiRequest<Stats>(`/api/stats${qs({ days })}`),

	// track info
	trackInfo: (t: Track) =>
		apiRequest<TrackInfo>(
			`/api/trackinfo${qs({ id: t.id, title: t.track, artist: t.artist, album: t.album, duration: t.duration, art: t.art })}`
		),
	trackInfoYouTube: (id: string) =>
		apiRequest<{ youtube?: TrackInfoYouTube }>(`/api/trackinfo/youtube${qs({ id })}`),

	// lyrics
	lyrics: (t: Track) =>
		apiRequest<Lyrics>(`/api/lyrics${qs({ artist: t.artist, track: t.track, duration: t.duration, id: t.id })}`),

	// identify (no UI in v1, but the method is here for parity)
	identify: (fingerprint: number[], duration: number) =>
		apiPost<IdentifyResult>('/api/identify', { fingerprint, duration }),
	/** Upload a recorded WAV clip for the same recognition path used by iOS. */
	identifyAudio: async (audio: Blob, filename = 'recording.wav') => {
		const form = new FormData();
		form.append('audio', audio, filename);
		return apiRequest<IdentifyResult>('/api/identify/audio', {
			method: 'POST',
			body: form
		});
	},

	// devices
	devices: async () => (await apiRequest<{ devices: Device[] }>('/api/devices')).devices,
	revoke: (id: string) => apiPost<Ok>('/api/devices/revoke', { id }),

	// party
	partyCreate: (name?: string) => apiPost<PartySnapshot>('/api/party/create', { name }),
	partyJoin: (code: string, name?: string) =>
		apiPost<PartySnapshot>('/api/party/join', { code, name }),
	partyLeave: (code: string) => apiPost<Ok>('/api/party/leave', { code }),
	partyState: (code: string) => apiRequest<PartySnapshot>(`/api/party/state${qs({ code })}`),
	partyEnqueue: (code: string, tracks: TrackPayload[]) =>
		apiPost<PartySnapshot>('/api/party/enqueue', { code, tracks }),
	partyNext: (code: string) => apiPost<PartySnapshot>('/api/party/next', { code }),
	partyPause: (code: string) => apiPost<PartySnapshot>('/api/party/pause', { code }),
	partyResume: (code: string) => apiPost<PartySnapshot>('/api/party/resume', { code }),
	partySeek: (code: string, pos: number) =>
		apiPost<PartySnapshot>('/api/party/seek', { code, pos })
};
