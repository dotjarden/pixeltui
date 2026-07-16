// UI chrome state shared between the browse shell and the global layout
// (queue sheet + lyrics sheet open/close, toggled from the MiniPlayer but
// rendered once in the layout).
import { writable } from 'svelte/store';

export const queueOpen = writable(false);
export const lyricsOpen = writable(false);
export const searchQuery = writable('');
