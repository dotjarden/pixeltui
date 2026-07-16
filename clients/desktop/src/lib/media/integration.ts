// OS media integration: global keyboard shortcuts + MediaSession metadata.
//
// Global shortcuts (registered via tauri-plugin-global-shortcut) work even
// when the app is not focused. They are best-effort — if the OS has already
// claimed a combo, registration fails silently and we fall back to the
// browser MediaSession API for media keys.
//
// MediaSession lets compatible OS shells (macOS Control Center / iOS lock
// screen don't apply here; Windows and some Linux DEs show it) display
// now-playing metadata and route play/pause/next/prev media keys to the app.

import { get } from 'svelte/store';
import { register, unregisterAll, type ShortcutEvent } from '@tauri-apps/plugin-global-shortcut';
import { current, isPlaying, next, previous, togglePlayPause } from '$lib/stores/player';

let registered = false;

function handleShortcut(e: ShortcutEvent) {
	if (e.state !== 'Pressed') return;
	switch (e.shortcut) {
		case 'MediaPlayPause':
		case 'CommandOrControl+Shift+Space':
			void togglePlayPause();
			break;
		case 'MediaTrackNext':
		case 'CommandOrControl+Shift+Right':
			void next();
			break;
		case 'MediaTrackPrevious':
		case 'CommandOrControl+Shift+Left':
			void previous();
			break;
	}
}

/** Register global media shortcuts. Safe to call multiple times. */
export async function registerMediaShortcuts(): Promise<void> {
	if (registered) return;
	try {
		await register(
			[
				'MediaPlayPause',
				'MediaTrackNext',
				'MediaTrackPrevious',
				'CommandOrControl+Shift+Space',
				'CommandOrControl+Shift+Right',
				'CommandOrControl+Shift+Left'
			],
			handleShortcut
		);
		registered = true;
	} catch {
		// Some platforms/DEs claim media keys; MediaSession is the fallback.
	}
}

export async function unregisterMediaShortcuts(): Promise<void> {
	if (!registered) return;
	try {
		await unregisterAll();
	} catch {
		/* ignore */
	}
	registered = false;
}

// ── MediaSession metadata ────────────────────────────────────────────────────

function updateMediaSession() {
	if (!('mediaSession' in navigator)) return;
	const ms = navigator.mediaSession;
	const t = get(current);
	if (t) {
		ms.metadata = new MediaMetadata({
			title: t.track,
			artist: t.artist,
			album: t.album ?? '',
			artwork: t.art ? [{ src: t.art, sizes: '512x512', type: 'image/jpeg' }] : []
		});
	} else {
		ms.metadata = null;
	}
}

function updatePlaybackState() {
	if (!('mediaSession' in navigator)) return;
	navigator.mediaSession.playbackState = get(isPlaying) ? 'playing' : 'paused';
}

/** Wire the player stores to MediaSession metadata/playback state. */
export function initMediaSession() {
	if (!('mediaSession' in navigator)) return () => {};

	const unsubCurrent = current.subscribe(updateMediaSession);
	const unsubPlaying = isPlaying.subscribe(updatePlaybackState);

	navigator.mediaSession.setActionHandler('play', () => {
		if (!get(isPlaying)) void togglePlayPause();
	});
	navigator.mediaSession.setActionHandler('pause', () => {
		if (get(isPlaying)) void togglePlayPause();
	});
	navigator.mediaSession.setActionHandler('previoustrack', () => previous());
	navigator.mediaSession.setActionHandler('nexttrack', () => next());

	return () => {
		unsubCurrent();
		unsubPlaying();
		navigator.mediaSession.setActionHandler('play', null);
		navigator.mediaSession.setActionHandler('pause', null);
		navigator.mediaSession.setActionHandler('previoustrack', null);
		navigator.mediaSession.setActionHandler('nexttrack', null);
	};
}