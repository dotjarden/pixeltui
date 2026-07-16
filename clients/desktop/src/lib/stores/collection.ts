// Transient hand-off for the generic "song collection" page (Liked, Recently
// Played, Made For You see-all, Daily Mix, Songs search see-all, source-genre
// collections). iOS passes the songs inline in its Route enum; the web client
// stages them here right before `goto('/collection')` and the route reads them
// on mount. Safe because Tauri runs SvelteKit client-side (no reload).

import { writable } from 'svelte/store';
import type { Track } from '$lib/api/types';

export interface Collection {
	title: string;
	subtitle?: string;
	symbol?: string;
	tracks: Track[];
}

export const collection = writable<Collection | null>(null);

export function openCollection(c: Collection) {
	collection.set(c);
}