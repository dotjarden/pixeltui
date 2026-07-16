// Downloads store: client-side saving of `/api/stream` bytes to
// `~/.pixeltui/downloads/<sanitized-id>.<ext>` (via Rust commands) + an
// `index.json` manifest the Rust side owns. Offline playback loads the saved
// file through the Tauri asset protocol and re-issues it as a same-origin
// `blob:` URL (so Web Audio's MediaElementSource isn't tainted by the
// cross-origin `asset://` origin). Mirrors iOS `DownloadStore`.
//
// State machine per track: `none | downloading(fraction) | downloaded`
// (no queue/failed/resume — a failed download just returns to `none`).

import { derived, get, writable } from 'svelte/store';
import { invoke } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';
import { convertFileSrc } from '@tauri-apps/api/core';
import { liked, likedIds } from '$lib/stores/library';
import type { Track } from '$lib/api/types';

export interface DownloadEntry {
	id: string;
	track: string;
	artist: string;
	album: string;
	duration: number;
	art: string;
	fileName: string;
	bytes: number;
}

interface DownloadRequest {
	id: string;
	track: string;
	artist: string;
	album: string;
	duration: number;
	art: string;
}

/** Manifest rows (for the Downloads page). */
export const entries = writable<DownloadEntry[]>([]);
/** id → fraction 0..1 for in-flight downloads. */
export const progress = writable<Record<string, number>>({});

/** id set of downloaded tracks — cheap membership check for menus/badges. */
export const downloadedIds = derived(entries, ($e) => new Set($e.map((d) => d.id)));

/**
 * id → 'downloading' | 'downloaded' for UI gating. Recomputed when either the
 * manifest or the in-flight progress map changes.
 */
export const downloadStateMap = derived([entries, progress], ([$entries, $progress]) => {
	const m = new Map<string, 'downloading' | 'downloaded'>();
	for (const id of Object.keys($progress)) m.set(id, 'downloading');
	for (const e of $entries) if (!$progress[e.id]) m.set(e.id, 'downloaded');
	return m;
});

const DOWNLOAD_LIKED_KEY = 'pixeltui.downloadLiked.v1';
/** When on: liking a track downloads it, unliking removes it. */
export const downloadLikedOn = writable<boolean>(
	(() => {
		try {
			return localStorage.getItem(DOWNLOAD_LIKED_KEY) === '1';
		} catch {
			return false;
		}
	})()
);

// ── internal maps ────────────────────────────────────────────────────────────
/** id → absolute on-disk path (dir + fileName). */
const pathMap = new Map<string, string>();
/** id → cached same-origin blob URL (LRU-bounded). */
const blobCache = new Map<string, string>();
const BLOB_CACHE_MAX = 4;
let downloadsDir = '';

// ── init / events ────────────────────────────────────────────────────────────

let wired = false;

/** Load the manifest + wire Rust progress/done/error events. Call once at boot. */
export async function initDownloads(): Promise<void> {
	if (wired) return;
	wired = true;
	try {
		downloadsDir = await invoke<string>('downloads_dir_path');
	} catch {
		downloadsDir = '';
	}
	await refreshEntries();

	listen<{ id: string; fraction: number }>('download://progress', (e) => {
		progress.update((p) => ({ ...p, [e.payload.id]: e.payload.fraction }));
	});
	listen<{ entry: DownloadEntry }>('download://done', (e) => {
		const entry = e.payload.entry;
		progress.update((p) => {
			const next = { ...p };
			delete next[entry.id];
			return next;
		});
		pathMap.set(entry.id, downloadsDir ? `${downloadsDir}/${entry.fileName}` : '');
		entries.update((list) => {
			const i = list.findIndex((d) => d.id === entry.id);
			return i >= 0 ? list.map((d, j) => (j === i ? entry : d)) : [...list, entry];
		});
	});
	listen<{ id: string; message: string }>('download://error', (e) => {
		progress.update((p) => {
			const next = { ...p };
			delete next[e.payload.id];
			return next;
		});
	});
}

async function refreshEntries(): Promise<void> {
	const list = await invoke<DownloadEntry[]>('list_downloads');
	entries.set(list);
	pathMap.clear();
	for (const e of list) pathMap.set(e.id, downloadsDir ? `${downloadsDir}/${e.fileName}` : '');
}

// ── blob URL management (Web-Audio-clean offline playback) ───────────────────

function touchBlobCache(id: string, url: string) {
	blobCache.delete(id);
	blobCache.set(id, url);
	while (blobCache.size > BLOB_CACHE_MAX) {
		const oldest = blobCache.keys().next().value;
		if (oldest === undefined) break;
		const u = blobCache.get(oldest);
		if (u) URL.revokeObjectURL(u);
		blobCache.delete(oldest);
	}
}

function revokeBlob(id: string) {
	const u = blobCache.get(id);
	if (u) URL.revokeObjectURL(u);
	blobCache.delete(id);
}

/**
 * Same-origin `blob:` URL for a downloaded track, or `null` if not on disk.
 * Fetches the asset-protocol URL and re-issues as a blob so the Web Audio graph
 * (MediaElementSource) isn't silenced by cross-origin tainting.
 */
export async function localPlaybackUrl(id: string): Promise<string | null> {
	const cached = blobCache.get(id);
	if (cached) {
		touchBlobCache(id, cached); // mark most-recent
		return cached;
	}
	const path = pathMap.get(id);
	if (!path) return null;
	try {
		const resp = await fetch(convertFileSrc(path));
		if (!resp.ok) return null;
		const blob = await resp.blob();
		const url = URL.createObjectURL(blob);
		touchBlobCache(id, url);
		return url;
	} catch {
		return null;
	}
}

// ── actions ──────────────────────────────────────────────────────────────────

function toRequest(t: Track): DownloadRequest {
	return {
		id: t.id,
		track: t.track,
		artist: t.artist,
		album: t.album ?? '',
		duration: t.duration,
		art: t.art ?? ''
	};
}

/** Start downloading a track (no-op if already downloaded or in flight). */
export async function downloadTrack(t: Track): Promise<void> {
	if (get(downloadedIds).has(t.id) || get(progress)[t.id] != null) return;
	progress.update((p) => ({ ...p, [t.id]: 0 }));
	try {
		await invoke<DownloadEntry>('download_track', { req: toRequest(t) });
	} catch {
		progress.update((p) => {
			const next = { ...p };
			delete next[t.id];
			return next;
		});
	}
}

/** Cancel an in-flight download. */
export async function cancelDownload(id: string): Promise<void> {
	await invoke('cancel_download', { id });
	progress.update((p) => {
		const next = { ...p };
		delete next[id];
		return next;
	});
}

/** Delete one download (file + manifest row). */
export async function removeDownload(id: string): Promise<void> {
	await invoke('remove_download', { id });
	revokeBlob(id);
	pathMap.delete(id);
	entries.update((list) => list.filter((d) => d.id !== id));
}

/** Delete every download. */
export async function removeAllDownloads(): Promise<void> {
	await invoke('remove_all_downloads');
	for (const u of blobCache.values()) URL.revokeObjectURL(u);
	blobCache.clear();
	pathMap.clear();
	entries.set([]);
	progress.set({});
}

// ── download-liked linkage ───────────────────────────────────────────────────

export function setDownloadLiked(on: boolean) {
	downloadLikedOn.set(on);
	try {
		localStorage.setItem(DOWNLOAD_LIKED_KEY, on ? '1' : '0');
	} catch {
		/* ignore */
	}
	if (on) void backfillLikedDownloads();
	else void removeAllLikedDownloads();
}

/**
 * Called by `library.toggleLike` (via dynamic import to avoid a static cycle):
 * like → download, unlike → remove. Only acts when the toggle is on.
 */
export function applyDownloadLiked(t: Track, nowLiked: boolean): void {
	if (!get(downloadLikedOn)) return;
	if (nowLiked) void downloadTrack(t);
	else void removeDownload(t.id);
}

async function backfillLikedDownloads(): Promise<void> {
	const have = get(downloadedIds);
	const inFlight = get(progress);
	for (const t of get(liked)) {
		if (have.has(t.id) || inFlight[t.id] != null) continue;
		void downloadTrack(t);
	}
}

async function removeAllLikedDownloads(): Promise<void> {
	const likedSet = get(likedIds);
	for (const d of get(entries)) {
		if (likedSet.has(d.id)) void removeDownload(d.id);
	}
}

/** Sync state for a single track: 'none' | 'downloading' | 'downloaded'. */
export function downloadState(id: string): 'none' | 'downloading' | 'downloaded' {
	if (get(progress)[id] != null) return 'downloading';
	if (get(downloadedIds).has(id)) return 'downloaded';
	return 'none';
}