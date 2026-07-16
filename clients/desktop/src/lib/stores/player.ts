// Player store: queue, transport, and the 2 Hz tick that drives scrobble,
// gapless prepare, crossfade, sleep fade, and the early-end advance. Mirrors
// iOS PlayerStore.swift. The 2 Hz position lives in its own `position` store
// (PlaybackProgress) so only the scrubber/lyrics redraw on each tick.

import { derived, get, writable } from 'svelte/store';
import { AudioEngine, type RepeatMode } from '$lib/audio/engine';
import { api } from '$lib/api/client';
import type { Lyrics, Track } from '$lib/api/types';

export const current = writable<Track | null>(null);
export const isPlaying = writable(false);
export const isPreparing = writable(false);
export const upNext = writable<Track[]>([]);
export const autoplayIDs = writable<Set<string>>(new Set());
export const shuffleOn = writable(false);
export const repeatMode = writable<RepeatMode>('off');
export const autoplayOn = writable(true);
export const crossfadeSeconds = writable(0);
export const sleepDeadline = writable<number | null>(null);
export const sleepAtTrackEnd = writable(false);
export const notice = writable<string | null>(null);

// Separate PlaybackProgress store — the 2 Hz tick writes here only.
export const position = writable(0);
export const duration = writable(0);
export const volume = writable(1);

export const sleepTimerActive = derived(
	[sleepDeadline, sleepAtTrackEnd],
	([$d, $e]) => $d !== null || $e
);

export const CROSSFADE_PRESETS = [0, 2, 4, 6, 8, 12] as const;
const QUALIFY_FRACTION = 0.5;
const QUALIFY_SECONDS = 240;
const SLEEP_FADE_WINDOW = 20; // seconds — minute-timer fade window
const TRACK_END_FADE_WINDOW = 15; // seconds — end-of-track fade window
const QUEUE_KEY = 'pixeltui.queue.v1';
const SETTINGS_KEY = 'pixeltui.player.settings.v1';
const LYRICS_KEY = 'pixeltui.lyrics.v1';

// Lyrics are warmed when a track starts, then reused by the sheet. This mirrors
// iOS's in-memory + disk cache so opening lyrics never begins a cold request.
const lyricsCache = new Map<string, Lyrics | null>();
const lyricsInflight = new Map<string, Promise<Lyrics | null>>();

function readLyricsCache(track: Track): Lyrics | null | undefined {
	if (lyricsCache.has(track.id)) return lyricsCache.get(track.id);
	try {
		const raw = localStorage.getItem(`${LYRICS_KEY}:${encodeURIComponent(track.id)}`);
		if (!raw) return undefined;
		const parsed = JSON.parse(raw) as Lyrics;
		if (Array.isArray(parsed.synced) && typeof parsed.plain === 'string') {
			lyricsCache.set(track.id, parsed);
			return parsed;
		}
	} catch {
		/* ignore malformed cache */
	}
	return undefined;
}

export function cachedLyrics(track: Track): Lyrics | null | undefined {
	return readLyricsCache(track);
}

export function prefetchLyrics(track: Track): Promise<Lyrics | null> {
	const cached = readLyricsCache(track);
	if (cached !== undefined) return Promise.resolve(cached);
	const existing = lyricsInflight.get(track.id);
	if (existing) return existing;
	const request = api
		.lyrics(track)
		.then((result) => {
			const usable = result.synced.length || result.plain.trim() ? result : null;
			lyricsCache.set(track.id, usable);
			if (usable) {
				try { localStorage.setItem(`${LYRICS_KEY}:${encodeURIComponent(track.id)}`, JSON.stringify(usable)); } catch { /* ignore */ }
			}
			return usable;
		})
		.catch(() => null)
		.finally(() => lyricsInflight.delete(track.id));
	lyricsInflight.set(track.id, request);
	return request;
}

// ── internal engine state ────────────────────────────────────────────────────
let order: Track[] = [];
let index = -1;
let loadToken = 0;
let endingGuard = false;
let autoQueueing = false;
let resumeOnTopUp = false;
let playReported = false;
let playStartedAt = 0;
let tickTimer: ReturnType<typeof setInterval> | null = null;
let sleepTimeout: ReturnType<typeof setTimeout> | null = null;

const engine = new AudioEngine();

engine.onActiveReady = () => {
	isPreparing.set(false);
	isPlaying.set(true);
};
engine.onError = () => {
	isPreparing.set(false);
	notice.set('Playback error');
};
engine.onEnded = () => handleDidFinish();
engine.onPrepared = () => {
	/* preparedId is tracked on the engine; no store action needed */
};

// ── queue helpers ────────────────────────────────────────────────────────────

function variantKey(t: Track): string {
	const norm = (s: string) =>
		s
			.toLowerCase()
			.replace(/\s*\([^)]*\)\s*/g, ' ')
			.replace(/\s*\[[^\]]*\]\s*/g, ' ')
			.replace(/\s+/g, ' ')
			.trim();
	return `${norm(t.artist)}${norm(t.track)}`;
}

function freshForQueue(tracks: Track[]): Track[] {
	const seenId = new Set(order.map((t) => t.id));
	const seenVariant = new Set(order.map(variantKey));
	const fresh: Track[] = [];
	for (const t of tracks) {
		if (seenId.has(t.id)) continue;
		const vk = variantKey(t);
		if (seenVariant.has(vk)) continue;
		seenId.add(t.id);
		seenVariant.add(vk);
		fresh.push(t);
	}
	return fresh;
}

function refreshUpNext() {
	upNext.set(order.slice(index + 1));
}

function recentlyPlayed(): Track[] {
	const out: Track[] = [];
	const seen = new Set<string>();
	for (let i = index; i >= 0 && out.length < 8; i--) {
		const t = order[i];
		if (!t || seen.has(t.id)) continue;
		seen.add(t.id);
		out.push(t);
	}
	return out;
}

function gaplessUpcoming(): Track | null {
	const rep = get(repeatMode);
	if (rep === 'one') return null;
	const next = index + 1;
	if (next < order.length) return order[next];
	if (rep === 'all' && order.length) return order[0];
	return null;
}

// ── persistence ──────────────────────────────────────────────────────────────

function saveQueue() {
	try {
		localStorage.setItem(QUEUE_KEY, JSON.stringify({ order, index }));
	} catch {
		/* ignore */
	}
}

function saveSettings() {
	try {
		localStorage.setItem(
			SETTINGS_KEY,
			JSON.stringify({
				shuffle: get(shuffleOn),
				repeat: get(repeatMode),
				autoplay: get(autoplayOn),
				crossfade: get(crossfadeSeconds),
				volume: get(volume)
			})
		);
	} catch {
		/* ignore */
	}
}

function loadSettings() {
	try {
		const raw = localStorage.getItem(SETTINGS_KEY);
		if (!raw) return;
		const s = JSON.parse(raw);
		if (typeof s.shuffle === 'boolean') shuffleOn.set(s.shuffle);
		if (s.repeat) repeatMode.set(s.repeat);
		if (typeof s.autoplay === 'boolean') autoplayOn.set(s.autoplay);
		if (typeof s.crossfade === 'number') crossfadeSeconds.set(s.crossfade);
		if (typeof s.volume === 'number') {
			volume.set(s.volume);
			engine.setVolume(s.volume);
		}
	} catch {
		/* ignore */
	}
}

/** Restore the saved queue paused (call after the library is loaded). */
export function restoreQueue() {
	try {
		const raw = localStorage.getItem(QUEUE_KEY);
		if (!raw) return;
		const q = JSON.parse(raw);
		if (!Array.isArray(q.order) || q.order.length === 0) return;
		order = q.order;
		index = Math.min(Math.max(q.index ?? 0, 0), order.length - 1);
		refreshUpNext();
		load(index, false); // paused
	} catch {
		/* ignore */
	}
}

// ── core load / advance ─────────────────────────────────────────────────────

function nowPlaying(song: Track) {
	void api.nowPlaying(song);
}

function load(i: number, autoplay: boolean) {
	loadToken++;
	endingGuard = false;
	index = i;
	const song = order[i];
	if (!song) {
		stopAll();
		return;
	}
	current.set(song);
	position.set(0);
	duration.set(song.duration || 0);
	void prefetchLyrics(song);
	playReported = false;
	playStartedAt = Math.floor(Date.now() / 1000);
	isPreparing.set(autoplay);
	refreshUpNext();
	if (autoplay) startTick();
	else stopTick();

	if (engine.hasPrepared === song.id) {
		// gapless handoff (hard swap — manual skip or natural advance w/o crossfade)
		engine.commitPrepared();
		if (autoplay) {
			void engine.play();
			nowPlaying(song);
		}
	} else {
		void engine.loadActive(song.id, autoplay);
		if (autoplay) nowPlaying(song);
	}
	saveQueue();
	// Restoring a paused queue should not trigger recommendation work at launch.
	topUpAutoQueue(autoplay);
}

function advance(by: number, manual: boolean) {
	const rep = get(repeatMode);
	let next = index + by;
	if (next >= order.length) {
		if (rep === 'all') next = next % order.length;
		else if (!manual && get(autoplayOn)) {
			// queue ran dry — let a top-up land, then resume
			resumeOnTopUp = true;
			topUpAutoQueue();
			return;
		} else {
			stopAtEnd();
			return;
		}
	}
	if (next < 0) next = rep === 'all' ? order.length - 1 : 0;
	load(next, true);
}

function stopAtEnd() {
	engine.pause();
	isPlaying.set(false);
	stopTick();
}

function stopAll() {
	engine.stop();
	isPlaying.set(false);
	isPreparing.set(false);
	current.set(null);
	position.set(0);
	duration.set(0);
	stopTick();
}

function handleDidFinish() {
	if (endingGuard) return;
	endingGuard = true;

	if (get(sleepAtTrackEnd)) {
		sleepAtTrackEnd.set(false);
		engine.pause();
		isPlaying.set(false);
		engine.restoreVolume();
		engine.seek(0, get(duration));
		position.set(0);
		if (sleepTimeout) {
			clearTimeout(sleepTimeout);
			sleepTimeout = null;
		}
		sleepDeadline.set(null);
		endingGuard = false;
		return;
	}

	const rep = get(repeatMode);
	if (rep === 'one') {
		load(index, true);
		return;
	}
	advance(1, false);
}

// ── tick-driven decisions ───────────────────────────────────────────────────

function maybePrepareNext(pos: number, dur: number) {
	if (dur <= 1 || !engine.isPlaying || engine.isCrossfading) return;
	const boundary = Math.max(5, Math.min(dur * 0.75, dur - 40));
	if (pos < boundary) return;
	const song = gaplessUpcoming();
	if (!song || engine.hasPrepared === song.id) return;
	engine.prepareNext(song.id);
}

function beginCrossfade(song: Track, fade: number) {
	// Bookkeeping moves at fade start (mirror iOS beginCrossfade).
	loadToken++;
	endingGuard = false;
	playReported = false;
	playStartedAt = Math.floor(Date.now() / 1000);
	const next = order.findIndex((t) => t.id === song.id);
	if (next >= 0) index = next;
	current.set(song);
	position.set(0);
	duration.set(song.duration || 0);
	isPreparing.set(false);
	isPlaying.set(true);
	refreshUpNext();
	nowPlaying(song);
	engine.startCrossfade(fade);
	saveQueue();
	topUpAutoQueue();
}

function maybeBeginCrossfade(pos: number, dur: number) {
	const fade = get(crossfadeSeconds);
	if (fade <= 0 || !engine.isPlaying || engine.isCrossfading) return;
	if (get(sleepTimerActive)) return; // no crossfade while a sleep timer is armed
	if (get(repeatMode) === 'one') return;
	if (dur <= fade * 2) return;
	if (pos < dur - fade) return;
	const song = gaplessUpcoming();
	if (!song || engine.hasPrepared !== song.id) return;
	beginCrossfade(song, fade);
}

function updateSleepFade(pos: number, dur: number) {
	const deadline = get(sleepDeadline);
	const endTrack = get(sleepAtTrackEnd);
	let fraction: number | null = null;
	if (deadline !== null) {
		const remaining = (deadline - Date.now()) / 1000;
		if (remaining <= SLEEP_FADE_WINDOW) fraction = remaining / SLEEP_FADE_WINDOW;
	} else if (endTrack && dur > 1) {
		const remaining = dur - pos;
		if (remaining <= TRACK_END_FADE_WINDOW) fraction = remaining / TRACK_END_FADE_WINDOW;
	}
	if (fraction === null) {
		engine.restoreVolume();
		return;
	}
	engine.sleepFade(fraction);
}

function tick() {
	const song = get(current);
	if (!song) return;
	const pos = engine.position();
	const dur = engine.duration(song.duration || 0);
	position.set(pos);
	if (dur !== get(duration)) duration.set(dur);

	// scrobble: 50% or 4 minutes, whichever first
	if (
		engine.isPlaying &&
		!playReported &&
		dur > 1 &&
		pos >= Math.min(dur * QUALIFY_FRACTION, QUALIFY_SECONDS)
	) {
		playReported = true;
		void api.played(song, playStartedAt);
	}

	updateSleepFade(pos, dur);

	if (engine.isCrossfading) return;

	// early-end clamp (no-crossfade path): end at dur-0.4 to skip trailing silence
	if (!endingGuard && pos >= dur - 0.4) {
		handleDidFinish();
		return;
	}
	maybePrepareNext(pos, dur);
	maybeBeginCrossfade(pos, dur);
}

function startTick() {
	if (tickTimer) return;
	tickTimer = setInterval(tick, 500);
}

function stopTick() {
	if (tickTimer) {
		clearInterval(tickTimer);
		tickTimer = null;
	}
}

// ── autoplay top-up (recommend → radio fallback) ────────────────────────────

function buildSeeds(cur: Track): { artist?: string; track?: string }[] {
	const seeds: { artist?: string; track?: string }[] = [
		{ artist: cur.artist, track: cur.track }
	];
	const artists = new Set<string>([cur.artist.toLowerCase()]);
	for (const t of recentlyPlayed()) {
		if (t.id === cur.id) continue;
		if (artists.has(t.artist.toLowerCase())) continue;
		artists.add(t.artist.toLowerCase());
		seeds.push({ artist: t.artist, track: t.track });
		if (seeds.length >= 4) break;
	}
	return seeds;
}

function buildExclude(): string[] {
	return [...new Set(recentlyPlayed().map((t) => t.artist))];
}

async function topUpAutoQueue(force = false) {
	if (!get(autoplayOn) || autoQueueing || get(repeatMode) !== 'off' || (!force && !get(isPlaying))) return;
	const cur = get(current);
	if (!cur) return;
	if (get(upNext).length > 2) return;
	autoQueueing = true;
	const token = loadToken;
	const seeds = buildSeeds(cur);
	const exclude = buildExclude();
	let fresh: Track[] = [];
	try {
		const rec = await api.recommendations(25, seeds, exclude);
		if (rec.length >= 5) fresh = rec;
		else if (cur.capabilities?.radio) fresh = await api.radio(cur.id, 25, exclude);
	} catch {
		fresh = [];
	}
	autoQueueing = false;
	if (token !== loadToken) return; // a skip invalidated this fetch
	fresh = freshForQueue(fresh);
	if (fresh.length === 0) {
		if (resumeOnTopUp) {
			resumeOnTopUp = false;
			stopAtEnd();
		}
		return;
	}
	autoplayIDs.update((s) => {
		const n = new Set(s);
		fresh.forEach((t) => n.add(t.id));
		return n;
	});
	order = order.concat(fresh);
	refreshUpNext();
	saveQueue();
	if (resumeOnTopUp) {
		resumeOnTopUp = false;
		advance(1, false);
	}
}

// ── public transport API ────────────────────────────────────────────────────

function shuffleArray<T>(a: T[]): T[] {
	const r = [...a];
	for (let i = r.length - 1; i > 0; i--) {
		const j = Math.floor(Math.random() * (i + 1));
		[r[i], r[j]] = [r[j], r[i]];
	}
	return r;
}

export function play(track: Track, context?: Track[]) {
	let ctx = context;
	if (get(shuffleOn) && ctx) {
		ctx = [track, ...shuffleArray(ctx.filter((t) => t.id !== track.id))];
	}
	order = ctx ?? [track];
	index = ctx ? Math.max(0, ctx.findIndex((t) => t.id === track.id)) : 0;
	load(index, true);
}

export function playFromList(tracks: Track[], i: number) {
	if (tracks[i]) play(tracks[i], tracks);
}

/** Play a list shuffled from the start (used by immersive-header Shuffle). */
export function playShuffle(tracks: Track[]) {
	if (tracks.length === 0) return;
	const s = shuffleArray(tracks);
	play(s[0], s);
}

/**
 * Start a track-seeded radio station: fetch `/api/radio?id=` for the seed and
 * play it as a fresh queue. Mirrors iOS `player.startStation(song)`. Used by
 * the track context menu when `capabilities.start_station` is set.
 */
export async function startStation(seed: Track): Promise<void> {
	try {
		const tracks = await api.radio(seed.id, 25, []);
		if (tracks.length) {
			play(tracks[0], tracks);
			return;
		}
	} catch {
		/* fall through */
	}
	notice.set('No station available for this track');
}

export function addToQueue(track: Track) {
	if (!get(current)) {
		play(track);
		return;
	}
	order.splice(index + 1, 0, track);
	refreshUpNext();
	saveQueue();
}

export function addToQueueMany(tracks: Track[]) {
	if (!get(current) && tracks.length) {
		play(tracks[0], tracks);
		return;
	}
	const fresh = freshForQueue(tracks);
	order.splice(index + 1, 0, ...fresh);
	refreshUpNext();
	saveQueue();
}

export function next() {
	engine.cancelCrossfade();
	advance(1, true);
}

/** Jump to a track in up-next by its offset from the current track. */
export function jumpTo(queueOffset: number) {
	engine.cancelCrossfade();
	const target = index + 1 + queueOffset;
	if (target >= 0 && target < order.length) load(target, true);
}

/** Remove a track from up-next by its offset from the current track. */
export function removeFromQueue(queueOffset: number) {
	const at = index + 1 + queueOffset;
	if (at < 0 || at >= order.length) return;
	order.splice(at, 1);
	refreshUpNext();
	saveQueue();
}

export function previous() {
	if (engine.position() > 4) {
		engine.seek(0, get(duration));
		position.set(0);
		return;
	}
	engine.cancelCrossfade();
	let prev = index - 1;
	if (prev < 0) prev = get(repeatMode) === 'all' ? order.length - 1 : 0;
	load(prev, true);
}

export async function togglePlayPause() {
	await engine.togglePlayPause();
	const playing = engine.isPlaying;
	isPlaying.set(playing);
	if (playing) startTick();
	else stopTick();
}

/** Pause local playback without toggling (used when entering party mode). */
export function pause() {
	engine.pause();
	isPlaying.set(false);
	stopTick();
}

export function seek(sec: number) {
	engine.seek(sec, get(duration));
	position.set(sec);
}

export function toggleShuffle() {
	const on = !get(shuffleOn);
	shuffleOn.set(on);
	saveSettings();
	if (on && get(current)) {
		const head = order[index];
		const rest = order.filter((_, i) => i !== index);
		order = [head, ...shuffleArray(rest)];
		index = 0;
		refreshUpNext();
		saveQueue();
	}
}

export function cycleRepeat() {
	const order_seq: RepeatMode[] = ['off', 'all', 'one'];
	const cur = get(repeatMode);
	repeatMode.set(order_seq[(order_seq.indexOf(cur) + 1) % 3]);
	engine.clearPrepared(); // repeat change invalidates the prepared next
	saveSettings();
}

export function setCrossfade(sec: number) {
	const v = (CROSSFADE_PRESETS as readonly number[]).includes(sec) ? sec : 0;
	crossfadeSeconds.set(v);
	saveSettings();
}

export function setAutoplay(on: boolean) {
	autoplayOn.set(on);
	saveSettings();
	if (on) topUpAutoQueue(true);
}

export function setVolume(v: number) {
	const clamped = Math.min(Math.max(v, 0), 1);
	volume.set(clamped);
	engine.setVolume(clamped);
	saveSettings();
}

export function setSleepTimer(minutes: number) {
	cancelSleepTimer();
	const deadline = Date.now() + minutes * 60_000;
	sleepDeadline.set(deadline);
	sleepTimeout = setTimeout(() => pauseForSleep(), minutes * 60_000);
}

export function setSleepTimerEndOfTrack() {
	cancelSleepTimer();
	sleepAtTrackEnd.set(true);
}

export function pauseForSleep() {
	engine.cancelCrossfade();
	engine.pause();
	isPlaying.set(false);
	stopTick();
	engine.restoreVolume();
	sleepDeadline.set(null);
	if (sleepTimeout) {
		clearTimeout(sleepTimeout);
		sleepTimeout = null;
	}
}

export function cancelSleepTimer() {
	if (sleepTimeout) {
		clearTimeout(sleepTimeout);
		sleepTimeout = null;
	}
	sleepDeadline.set(null);
	sleepAtTrackEnd.set(false);
	engine.restoreVolume();
}

export function dismissNotice() {
	notice.set(null);
}

// Initialise settings from localStorage on module load.
loadSettings();
