import { writable } from 'svelte/store';
import type { Track } from '$lib/api/types';

export type TrackContextRequest = {
	track: Track;
	context?: Track[];
	onRemove?: () => void;
	x: number;
	y: number;
};

export const trackContextMenu = writable<TrackContextRequest | null>(null);

export function closeTrackContextMenu() {
	trackContextMenu.set(null);
}

/** Open the one global track menu at the actual secondary-click position. */
export function openTrackContextMenu(event: MouseEvent, track: Track, context?: Track[], onRemove?: () => void) {
	event.preventDefault();
	event.stopPropagation();
	trackContextMenu.set({ track, context, onRemove, x: event.clientX, y: event.clientY });
}

/** Overflow buttons do not have a pointer coordinate worth using; anchor it
 * to the button while the context menu itself remains viewport-level. */
export function openTrackContextMenuFromAnchor(anchor: HTMLElement, track: Track, context?: Track[], onRemove?: () => void) {
	const rect = anchor.getBoundingClientRect();
	trackContextMenu.set({ track, context, onRemove, x: rect.right, y: rect.bottom + 6 });
}
