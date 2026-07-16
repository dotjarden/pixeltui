// Centralized navigation helpers. Detail pages are built in Phase 4; these
// route to SvelteKit paths that the tab UI and TrackMenu both use, so the
// navigation surface is stable regardless of whether the target is a stub yet.
import { goto } from '$app/navigation';
import { openCollection } from '$lib/stores/collection';
import { api } from '$lib/api/client';
import type { Album, Track } from '$lib/api/types';

export function goArtist(name: string, browseId = '', art = '') {
	const q = new URLSearchParams();
	if (browseId) q.set('browse_id', browseId);
	if (art) q.set('art', art);
	const query = q.toString();
	goto(`/artist/${encodeURIComponent(name)}${query ? `?${query}` : ''}`);
}

/** Warm detail data while a pointer rests on an entity card. The route request
 * deduplicates against this prefetch, so clicking after hover paints from the
 * existing response instead of starting a second network round trip. */
export function prefetchArtist(name: string, browseId = '', art = '') {
	void api.artistPage(name, true, browseId, art).catch(() => {});
}

export function goAlbum(album: Album) {
	const q = new URLSearchParams({
		title: album.title,
		artist: album.artist
	});
	if (album.browse_id) q.set('browse_id', album.browse_id);
	goto(`/album?${q.toString()}`);
}

export function prefetchAlbum(album: Album) {
	void api.albumPage(album.browse_id, album.title, album.artist).catch(() => {});
}

export function goPlaylist(name: string) {
	goto(`/playlist/${encodeURIComponent(name)}`);
}

export function goChart(country: string, title?: string) {
	const q = new URLSearchParams({ country });
	if (title) q.set('title', title);
	goto(`/chart?${q.toString()}`);
}

export function goTrackInfo(track: Track) {
	const q = new URLSearchParams({
		title: track.track,
		artist: track.artist,
		source: track.source
	});
	if (track.album) q.set('album', track.album);
	if (track.duration) q.set('duration', String(track.duration));
	if (track.art) q.set('art', track.art);
	goto(`/trackinfo/${encodeURIComponent(track.id)}?${q.toString()}`);
}

export function goCollection(c: Parameters<typeof openCollection>[0]) {
	openCollection(c);
	goto('/collection');
}
