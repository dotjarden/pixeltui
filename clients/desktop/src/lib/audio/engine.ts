// Web Audio playback engine — mirrors iOS PlayerStore's two-player model.
//
// Two `<audio>` elements, each `MediaElementSource → GainNode → destination`.
// The *active* element plays the current track; the *inactive* element is used
// to prepare the next track for gapless handoff or crossfade. The GainNode per
// element is the "player volume" (carries the equal-power crossfade ramp and the
// linear sleep-fade ramp, scaled by the user/master volume).
//
// Decisions (what's next, when to prepare/crossfade, scrobble, sleep, autoplay)
// live in the player store, which drives a 2 Hz tick reading engine.position()
// /engine.duration(). The engine owns only audio mechanics and emits:
//   onEnded       — active element finished naturally (advance backstop)
//   onPrepared    — inactive element has the next track buffered (gapless ready)
//   onActiveReady — active element actually started playing (clears isPreparing)
//   onError       — active element failed to load
//
// Auth for the stream URL goes in the URL via `?token=` (see streamUrl).

import { resolvePlaybackUrl } from '$lib/audio/source';

export type RepeatMode = 'off' | 'all' | 'one';

export class AudioEngine {
	private ctx: AudioContext | null = null;
	private el1: HTMLAudioElement;
	private el2: HTMLAudioElement;
	private s1!: MediaElementAudioSourceNode;
	private s2!: MediaElementAudioSourceNode;
	private g1!: GainNode;
	private g2!: GainNode;

	private active: 1 | 2 = 1;
	private preparingId: string | null = null;
	private preparedId: string | null = null;
	private prepareFailedId: string | null = null;
	private activeLoadSeq = 0;

	private fadeTimer: ReturnType<typeof setInterval> | null = null;
	private crossfading = false;
	private outgoing: 1 | 2 | null = null;

	private userVolume = 1;
	private sleepBaseVolume: number | null = null;

	// Engine callbacks (set by the player store).
	onEnded?: () => void;
	onPrepared?: (id: string) => void;
	onActiveReady?: () => void;
	onError?: () => void;

	constructor() {
		this.el1 = this.makeEl();
		this.el2 = this.makeEl();
	}

	private makeEl(): HTMLAudioElement {
		const el = new Audio();
		el.crossOrigin = 'anonymous';
		el.preload = 'auto';
		return el;
	}

	private ensureGraph() {
		if (this.ctx) return;
		const ctx = new AudioContext();
		this.g1 = ctx.createGain();
		this.g2 = ctx.createGain();
		this.g1.gain.value = 0;
		this.g2.gain.value = 0;
		this.s1 = ctx.createMediaElementSource(this.el1);
		this.s2 = ctx.createMediaElementSource(this.el2);
		this.s1.connect(this.g1).connect(ctx.destination);
		this.s2.connect(this.g2).connect(ctx.destination);
		this.ctx = ctx;
		this.wire(this.el1, 1);
		this.wire(this.el2, 2);
	}

	private wire(el: HTMLAudioElement, which: 1 | 2) {
		el.addEventListener('ended', () => {
			if (which === this.active && !this.crossfading) this.onEnded?.();
		});
		el.addEventListener('error', () => {
			if (which === this.active) this.onError?.();
		});
		el.addEventListener('playing', () => {
			if (which === this.active) this.onActiveReady?.();
		});
		el.addEventListener('canplaythrough', () => {
			if (which !== this.active && this.preparingId && el.getAttribute('data-id') === this.preparingId) {
				this.preparedId = this.preparingId;
				this.preparingId = null;
				this.onPrepared?.(this.preparedId);
			}
		});
	}

	private get activeEl() {
		return this.active === 1 ? this.el1 : this.el2;
	}
	private get inactiveEl() {
		return this.active === 1 ? this.el2 : this.el1;
	}
	private get activeGain() {
		return this.active === 1 ? this.g1 : this.g2;
	}
	private get inactiveGain() {
		return this.active === 1 ? this.g2 : this.g1;
	}

	/** Current playback position of the active element, in seconds. */
	position(): number {
		return this.activeEl.currentTime || 0;
	}

	/** Catalog duration wins (AVPlayer/audio duration is wrong for some yt); else element duration; else 1. */
	duration(catalog: number): number {
		if (catalog > 0) return catalog;
		const d = this.activeEl.duration;
		if (Number.isFinite(d) && d > 0) return d;
		return 1;
	}

	get isPlaying(): boolean {
		return !this.activeEl.paused && !this.activeEl.ended;
	}

	get isCrossfading(): boolean {
		return this.crossfading;
	}

	get hasPrepared(): string | null {
		return this.preparedId;
	}

	get prepareFailed(): string | null {
		return this.prepareFailedId;
	}

	clearPrepared() {
		this.preparedId = null;
		this.preparingId = null;
	}

	/** Resolve + play a track on the active element (no prepared handoff). */
	async loadActive(id: string, autoplay: boolean): Promise<void> {
		const seq = ++this.activeLoadSeq;
		this.ensureGraph();
		// Restoring a paused queue must stay silent. Calling play() and then
		// pausing creates a race where the `playing` event fires during launch.
		this.cancelCrossfade();
		this.preparedId = null;
		this.preparingId = null;
		const el = this.activeEl;
		el.setAttribute('data-id', id);
		const url = await resolvePlaybackUrl(id);
		if (seq !== this.activeLoadSeq) return;
		el.preload = autoplay ? 'auto' : 'none';
		el.src = url;
		el.currentTime = 0;
		this.activeGain.gain.value = this.userVolume;
		this.inactiveGain.gain.value = 0;
		if (!autoplay) {
			return;
		}
		await this.ctx!.resume();
		try { await el.play(); } catch { this.onError?.(); }
	}

	/** Preload the next track onto the inactive element (gapless prep). */
	async prepareNext(id: string) {
		this.ensureGraph();
		if (this.preparingId === id || this.preparedId === id || this.prepareFailedId === id) return;
		this.preparingId = id;
		this.preparedId = null;
		const el = this.inactiveEl;
		el.setAttribute('data-id', id);
		const url = await resolvePlaybackUrl(id);
		// A skip/load may have cleared the prepare while we were resolving the
		// (possibly local) URL — don't load a stale track onto the inactive el.
		if (this.preparingId !== id) return;
		el.src = url;
		el.currentTime = 0;
		this.inactiveGain.gain.value = 0;
		el.load();
		// Fallback: if canplaythrough never fires but the element is ready enough,
		// mark prepared on 'canplay' too.
		const onCanPlay = () => {
			if (this.preparingId === id) {
				this.preparedId = id;
				this.preparingId = null;
				this.onPrepared?.(id);
			}
			el.removeEventListener('canplay', onCanPlay);
		};
		el.addEventListener('canplay', onCanPlay, { once: true });
	}

	/**
	 * Gapless handoff (crossfade off): the prepared inactive element becomes
	 * active and plays at full volume; the old active is paused + cleared.
	 * Only call when hasPrepared === nextId.
	 */
	commitPrepared() {
		const id = this.preparedId;
		if (!id) return;
		const oldActive = this.active;
		const oldEl = this.activeEl;
		this.active = this.active === 1 ? 2 : 1;
		this.preparedId = null;
		this.preparingId = null;
		this.activeGain.gain.value = this.userVolume;
		void this.activeEl.play();
		// tear down the outgoing element after the swap
		oldEl.pause();
		oldEl.removeAttribute('data-id');
		oldEl.src = '';
		if (oldActive === 1) this.g1.gain.value = 0;
		else this.g2.gain.value = 0;
	}

	/**
	 * Equal-power crossfade from the active element into the prepared inactive
	 * one, over `fade` seconds. Ramp clocked by the incoming element's
	 * currentTime (so pausing freezes the fade, like iOS). Bookkeeping (index,
	 * current, reporters) must be updated by the caller BEFORE calling this.
	 */
	startCrossfade(fade: number) {
		const id = this.preparedId;
		if (!id || fade <= 0) {
			this.commitPrepared();
			return;
		}
		this.ensureGraph();
		this.crossfading = true;
		this.outgoing = this.active;
		const incomingEl = this.inactiveEl;
		const incomingGain = this.inactiveGain;
		const outgoingGain = this.activeGain;
		// promote incoming to active immediately (identity moves to incoming)
		this.active = this.active === 1 ? 2 : 1;
		this.preparedId = null;
		this.preparingId = null;
		incomingGain.gain.value = 0;
		void incomingEl.play();
		const startCtx = this.ctx!.currentTime;
		this.fadeTimer = setInterval(() => {
			const elapsed = incomingEl.currentTime || 0;
			const f = Math.min(Math.max(elapsed / fade, 0), 1);
			const phase = (f * Math.PI) / 2;
			const gIn = Math.sin(phase) * this.userVolume;
			const gOut = Math.cos(phase) * this.userVolume;
			incomingGain.gain.value = gIn;
			outgoingGain.gain.value = gOut;
			if (f >= 1) this.finishCrossfade();
		}, 50);
	}

	private finishCrossfade() {
		if (this.fadeTimer) {
			clearInterval(this.fadeTimer);
			this.fadeTimer = null;
		}
		const outEl = this.outgoing === 1 ? this.el1 : this.el2;
		const outGain = this.outgoing === 1 ? this.g1 : this.g2;
		outEl.pause();
		outEl.removeAttribute('data-id');
		outEl.src = '';
		outGain.gain.value = 0;
		this.activeGain.gain.value = this.userVolume;
		this.crossfading = false;
		this.outgoing = null;
	}

	cancelCrossfade() {
		if (!this.crossfading) return;
		this.finishCrossfade();
	}

	async play() {
		this.ensureGraph();
		this.activeEl.preload = 'auto';
		await this.ctx!.resume();
		await this.activeEl.play();
	}

	pause() {
		this.activeEl.pause();
		if (this.crossfading) {
			// pause both sides (togglePlayPause mid-crossfade)
			(this.outgoing === 1 ? this.el1 : this.el2).pause();
		}
	}

	async togglePlayPause() {
		if (this.isPlaying) this.pause();
		else await this.play();
	}

	seek(sec: number, dur: number) {
		const clamped = Math.min(Math.max(0, sec), dur);
		this.activeEl.currentTime = clamped;
	}

	/** User/master volume (0..1). Scales the active gain (and ramp targets). */
	setVolume(v: number) {
		this.userVolume = Math.min(Math.max(v, 0), 1);
		if (!this.crossfading && this.sleepBaseVolume === null) {
			this.activeGain.gain.value = this.userVolume;
		}
	}

	getVolume(): number {
		return this.userVolume;
	}

	/**
	 * Linear sleep-fade: ramp the active gain to `fraction` of the base volume.
	 * fraction=1 restores full. Call each tick while a sleep timer is armed.
	 */
	sleepFade(fraction: number) {
		if (this.crossfading) return;
		if (this.sleepBaseVolume === null) this.sleepBaseVolume = this.userVolume;
		const base = this.sleepBaseVolume;
		this.activeGain.gain.value = base * Math.min(Math.max(fraction, 0), 1);
	}

	/** Undo any in-progress sleep fade (outside the fade window). */
	restoreVolume() {
		if (this.sleepBaseVolume === null) return;
		this.activeGain.gain.value = this.userVolume;
		this.sleepBaseVolume = null;
	}

	stop() {
		this.activeLoadSeq++;
		this.cancelCrossfade();
		this.el1.pause();
		this.el2.pause();
		this.el1.src = '';
		this.el2.src = '';
		this.g1 && (this.g1.gain.value = 0);
		this.g2 && (this.g2.gain.value = 0);
		this.preparedId = null;
		this.preparingId = null;
	}
}
