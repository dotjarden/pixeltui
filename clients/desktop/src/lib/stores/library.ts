// Library/catalog store: liked tracks, playlists, sources, advertised
// endpoints, and recent listens. Mirrors iOS ServerStore's resync logic —
// a 1.5s debounce around a full refresh (TUI edits land in bursts), a
// `refreshInFlight` guard so stacked hints don't queue duplicate refreshes,
// and a recents-only fast path for `history` hints.

import { derived, get, writable } from 'svelte/store';
import { api } from '$lib/api/client';
import { events } from '$lib/sse/events';
import type { HistoryEntry, Track } from '$lib/api/types';

export const liked = writable<Track[]>([]);
export const playlists = writable<string[]>([]);
export const playlistCache = writable<Record<string, Track[]>>({});
export const sources = writable<string[]>([]);
export const endpoints = writable<string[]>([]);
export const serverName = writable<string>('');
export const recents = writable<HistoryEntry[]>([]);

/** id set of liked tracks — cheap membership check for menus/rows. */
export const likedIds = derived(liked, ($liked) => new Set($liked.map((t) => t.id)));

let resyncTimer: ReturnType<typeof setTimeout> | null = null;
let recentsTimer: ReturnType<typeof setTimeout> | null = null;
let refreshInFlight = false;
const playlistInflight = new Map<string, Promise<Track[]>>();

/** Full catalog refresh: liked + playlists + sources/endpoints/name. */
export async function refresh() {
	if (refreshInFlight) return;
	refreshInFlight = true;
	try {
		// Resolve the visible library independently. A slow sources/endpoint
		// response must not hold liked songs and playlists behind a blank shell.
		const requests = [
			api.liked().then((l) => liked.set(l)),
			api.playlists().then((p) => playlists.set(p)),
			api.sources().then((s) => {
				sources.set(s.sources);
				endpoints.set(s.endpoints);
				serverName.set(s.name);
			})
		];
		await Promise.allSettled(requests);
	} finally {
		refreshInFlight = false;
	}
}

/** Debounced full refresh (1.5s of quiet, mirroring iOS `scheduleResync`). */
export function scheduleResync() {
	if (resyncTimer) clearTimeout(resyncTimer);
	resyncTimer = setTimeout(() => {
		resyncTimer = null;
		refresh();
	}, 1500);
}

/** Fetch recent listens; only update if the id list changed (own-play no-op). */
export async function refreshRecents() {
	const h = await api.history(24, true);
	const next = h.map((t) => t.id).join('|');
	const prev = get(recents)
		.map((t) => t.id)
		.join('|');
	if (next !== prev) recents.set(h);
}

/** Debounced recents-only sync for `history` hints. */
export function scheduleRecentsSync() {
	if (recentsTimer) clearTimeout(recentsTimer);
	recentsTimer = setTimeout(() => {
		recentsTimer = null;
		refreshRecents();
	}, 1500);
}

/** Load (and cache) one playlist's tracks. */
export async function loadPlaylist(name: string): Promise<Track[]> {
	const existing = playlistInflight.get(name);
	if (existing) return existing;
	const request = api.playlistTracks(name)
		.then((t) => {
			playlistCache.update((c) => ({ ...c, [name]: t }));
			return t;
		})
		.finally(() => playlistInflight.delete(name));
	playlistInflight.set(name, request);
	return request;
}

/**
 * Toggle like on a track and patch the local store optimistically (the server
 * confirms via a `library` SSE hint that triggers a resync). Mirrors iOS
 * `library.toggleLike`.
 */
export async function toggleLike(t: Track): Promise<boolean> {
	const wasLiked = get(likedIds).has(t.id);
	if (wasLiked) {
		liked.update((l) => l.filter((x) => x.id !== t.id));
	} else {
		liked.update((l) => [t, ...l]);
	}
	try {
		const r = await api.setLike(t, !wasLiked);
		// download-liked linkage (dynamic import avoids a static cycle with
		// the downloads store, which imports `liked` from here).
		import('$lib/stores/downloads')
			.then((d) => d.applyDownloadLiked(t, r.liked))
			.catch(() => {});
		return r.liked;
	} catch {
		// roll back on failure
		if (wasLiked) liked.update((l) => [t, ...l]);
		else liked.update((l) => l.filter((x) => x.id !== t.id));
		return wasLiked;
	}
}

/** Add a track to a named playlist (fires a server `library` hint → resync). */
export async function addToPlaylist(name: string, t: Track): Promise<void> {
	await api.playlistAdd(name, t);
	await loadPlaylist(name);
}

/**
 * Wire server SSE hints to the debounced resync/recents paths. Call once after
 * the stream is started.
 */
export function connectLibraryEvents() {
	events.on('hello', (name) => serverName.set(name));
	events.on('library', (hint) => {
		// Hints: "liked" | "playlists" | "library" | "history".
		if (hint === 'history') scheduleRecentsSync();
		else scheduleResync();
	});
	events.on('endpoints', () => {
		// Tunnel re-established at a new URL — refresh advertised endpoints.
		api.sources()
			.then((s) => endpoints.set(s.endpoints))
			.catch(() => {});
	});
}
