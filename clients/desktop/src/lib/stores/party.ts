// Party store — REMOTE mode only, mirroring iOS `PartyStore`. The host (a
// "pocket" device) is the speaker; this client joins by code, renders the
// authoritative `PartySnapshot` over SSE, and sends transport/enqueue commands.
// It plays no audio itself — on join it pauses its own standalone playback.
//
// Gotchas honored (see party contract):
//  - transport/seek/enqueue responses are applied immediately (UI updates
//    before the SSE frame lands).
//  - `rev` guards against stale/out-of-order frames on reconnect.
//  - `position` is server-computed at snapshot time; a 1 Hz tick advances the
//    display as `position + (now − snapshot_unix_ms)/1000` while playing.
//  - the host is absent from `members` (only joined remotes appear).

import { derived, get, writable } from 'svelte/store';
import { api, toPayload } from '$lib/api/client';
import { partyStream } from '$lib/sse/party';
import { pause as pausePlayer } from '$lib/stores/player';
import type { PartySnapshot, Track } from '$lib/api/types';

export const room = writable<PartySnapshot | null>(null);
export const joining = writable(false);
export const joinError = writable<string | null>(null);

export const joined = derived(room, ($r) => $r !== null);
/** Ticked playback position for a progress bar (advanced client-side). */
export const displayPosition = writable(0);

const DEVICE_NAME = 'PixelPal Desktop';

let lastRev = 0;
let tickTimer: ReturnType<typeof setInterval> | null = null;

/** Apply a snapshot, dropping stale frames (rev guard for reconnect safety). */
function apply(snap: PartySnapshot) {
	if (snap.rev < lastRev) return;
	lastRev = snap.rev;
	room.set(snap);
}

function startTick() {
	if (tickTimer) return;
	tickTimer = setInterval(() => {
		const r = get(room);
		if (!r) return;
		if (r.playing && !r.paused) {
			const elapsed = (Date.now() - r.snapshot_unix_ms) / 1000;
			const pos = r.position + elapsed;
			const dur = r.track?.duration ?? 0;
			displayPosition.set(dur > 0 ? Math.min(pos, dur) : pos);
		} else {
			displayPosition.set(r.position);
		}
	}, 1000);
}

function stopTick() {
	if (tickTimer) {
		clearInterval(tickTimer);
		tickTimer = null;
	}
}

/** Join a party by its room code. Pauses local playback (this client is silent). */
export async function join(code: string): Promise<void> {
	joining.set(true);
	joinError.set(null);
	try {
		const snap = await api.partyJoin(code.trim(), DEVICE_NAME);
		lastRev = 0;
		apply(snap);
		pausePlayer();
		partyStream.onSnapshot(apply);
		partyStream.start(snap.code);
		startTick();
	} catch (e) {
		joinError.set(e instanceof Error ? e.message : 'Failed to join party');
	} finally {
		joining.set(false);
	}
}

/** Leave the current party and stop the SSE stream. */
export async function leave(): Promise<void> {
	const r = get(room);
	if (r) {
		await api.partyLeave(r.code).catch(() => {});
	}
	partyStream.stop();
	room.set(null);
	lastRev = 0;
	stopTick();
	displayPosition.set(0);
}

export async function next(): Promise<void> {
	const r = get(room);
	if (!r) return;
	try {
		apply(await api.partyNext(r.code));
	} catch {
		/* SSE will correct */
	}
}

export async function togglePause(): Promise<void> {
	const r = get(room);
	if (!r) return;
	try {
		apply(r.paused ? await api.partyResume(r.code) : await api.partyPause(r.code));
	} catch {
		/* SSE will correct */
	}
}

export async function seek(pos: number): Promise<void> {
	const r = get(room);
	if (!r) return;
	try {
		apply(await api.partySeek(r.code, pos));
	} catch {
		/* SSE will correct */
	}
}

/** Add tracks to the party queue (any member can enqueue). */
export async function enqueue(tracks: Track[]): Promise<void> {
	const r = get(room);
	if (!r || tracks.length === 0) return;
	try {
		apply(await api.partyEnqueue(r.code, tracks.map(toPayload)));
	} catch {
		/* SSE will correct */
	}
}