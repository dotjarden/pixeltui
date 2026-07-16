// Playback URL resolution: a downloaded track plays from its on-disk copy (via
// a same-origin `blob:` URL from the downloads store) instead of streaming from
// the server. Falls back to `/api/stream` when the track isn't downloaded or
// the local file can't be read. Used by the audio engine for both the active
// load and the gapless prepare of the next track.

import { streamUrl } from '$lib/api/client';
import { localPlaybackUrl } from '$lib/stores/downloads';

/** A playable URL for `id`: local blob if downloaded, else the stream URL. */
export async function resolvePlaybackUrl(id: string): Promise<string> {
	const local = await localPlaybackUrl(id);
	return local ?? streamUrl(id);
}