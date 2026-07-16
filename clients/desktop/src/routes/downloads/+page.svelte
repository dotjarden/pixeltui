<script lang="ts">
	// Downloads page: in-flight progress + the on-disk manifest. Tracks play
	// from their local copy (the engine resolves downloaded ids to a blob URL),
	// so this page works even with the server offline. Mirrors iOS
	// `DownloadedView`.
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ArtImg from '$lib/components/ArtImg.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import {
		entries,
		progress,
		downloadLikedOn,
		setDownloadLiked,
		removeDownload,
		cancelDownload,
		removeAllDownloads
	} from '$lib/stores/downloads';
	import { playFromList } from '$lib/stores/player';
	import type { Track, Capabilities } from '$lib/api/types';
	import type { DownloadEntry } from '$lib/stores/downloads';

	// Reconstruct a playable Track from a manifest row. Capabilities are
	// permissive — the row's primary action is local playback; server-side
	// actions (radio, station) still work via the id when the server is up.
	function toTrack(e: DownloadEntry): Track {
		const caps: Capabilities = {
			start_station: false,
			go_to_artist: !!e.artist && e.artist !== 'Unknown Artist',
			go_to_album: !!e.album && e.album !== 'Singles',
			radio: false,
			download: true,
			lyrics: true
		};
		return {
			id: e.id,
			track: e.track,
			artist: e.artist,
			album: e.album || undefined,
			duration: e.duration,
			art: e.art || undefined,
			source: e.id.startsWith('yt:')
				? 'youtube'
				: e.id.startsWith('su:')
					? 'subsonic'
					: 'local',
			capabilities: caps
		};
	}

	const tracks = $derived<Track[]>($entries.map(toTrack));
	const inflight = $derived(Object.entries($progress));
	const dlOn = $derived($downloadLikedOn);

	function fmtBytes(n: number): string {
		if (!n) return '';
		const mb = n / (1024 * 1024);
		return mb >= 1 ? `${mb.toFixed(1)} MB` : `${(n / 1024).toFixed(0)} KB`;
	}
	function pct(f: number): number {
		return Math.round((f || 0) * 100);
	}
	function play(i: number) {
		playFromList(tracks, i);
	}
	function remove(id: string) {
		void removeDownload(id);
	}
	function cancel(id: string) {
		void cancelDownload(id);
	}
	function clearAll() {
		if ($entries.length && window.confirm(`Remove all ${$entries.length} downloads?`)) {
			void removeAllDownloads();
		}
	}
	function toggleLiked(on: boolean) {
		setDownloadLiked(on);
	}
</script>

<DetailShell
	title="Downloads"
	subtitle={$entries.length ? `${$entries.length} saved` : ''}
	loading={false}
	showHeading
>
	<div class="toolbar">
		<label class="toggle">
			<input type="checkbox" checked={dlOn} onchange={(e) => toggleLiked(e.currentTarget.checked)} />
			<span>Download Liked Songs</span>
		</label>
		{#if $entries.length}
			<button class="clear" onclick={clearAll}>Remove All</button>
		{/if}
	</div>

	{#if inflight.length}
		<h3 class="sec">Downloading</h3>
		<ul class="list">
			{#each inflight as [id, frac] (id)}
				<li class="row">
					<div class="meta">
						<div class="title">{id}</div>
						<div class="bar"><div class="fill" style="width:{pct(frac)}%"></div></div>
					</div>
					<span class="pct">{pct(frac)}%</span>
					<button class="x" onclick={() => cancel(id)}>Cancel</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#if tracks.length}
		<ul class="list">
			{#each tracks as t, i (t.id)}
				<li class="row">
					<button class="play" onclick={() => play(i)} aria-label="Play">
						<ArtImg ref={t.art} size="44px" />
					</button>
					<button class="text" onclick={() => play(i)}>
						<span class="title">{t.track}</span>
						<span class="sub">{t.artist}</span>
					</button>
					<span class="size">{fmtBytes($entries[i].bytes)}</span>
					<button class="x" onclick={() => remove(t.id)} aria-label="Remove"><Icon name="close" size={14} /></button>
				</li>
			{/each}
		</ul>
	{:else if !inflight.length}
		<p class="empty">
			No downloads yet. Use the More menu on any track and choose <strong>Download</strong>, or turn on
			<strong>Download Liked Songs</strong> above.
		</p>
	{/if}
</DetailShell>

<style>
	.toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		margin-bottom: 1rem;
	}
	.toggle {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.88rem;
		cursor: pointer;
	}
	.clear {
		border: 1px solid #e0e0e0;
		background: transparent;
		border-radius: 999px;
		padding: 0.3rem 0.9rem;
		font: inherit;
		font-size: 0.8rem;
		cursor: pointer;
		color: #c33;
	}
	.clear:hover {
		background: #fff0f0;
	}
	.sec {
		font-size: 0.85rem;
		margin: 0 0 0.4rem;
		color: #666;
	}
	.list {
		list-style: none;
		padding: 0;
		margin: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		border-bottom: 1px solid #f0f0f0;
		padding: 0.35rem 0;
	}
	.play {
		border: none;
		background: transparent;
		padding: 0;
		cursor: pointer;
		border-radius: 6px;
		overflow: hidden;
		flex-shrink: 0;
	}
	.text {
		flex: 1;
		min-width: 0;
		border: none;
		background: transparent;
		text-align: left;
		cursor: pointer;
		display: flex;
		flex-direction: column;
		padding: 0;
	}
	.title {
		font-size: 0.9rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		font-size: 0.76rem;
		color: #888;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.size {
		font-size: 0.72rem;
		color: #999;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.bar {
		flex: 1;
		height: 5px;
		background: #eee;
		border-radius: 3px;
		overflow: hidden;
		margin-top: 0.3rem;
	}
	.fill {
		height: 100%;
		background: #2a6df6;
	}
	.pct {
		font-size: 0.75rem;
		color: #555;
		font-variant-numeric: tabular-nums;
		min-width: 2.6rem;
		text-align: right;
	}
	.x {
		border: none;
		background: transparent;
		cursor: pointer;
		color: #999;
		font-size: 0.85rem;
		padding: 0.2rem 0.4rem;
		border-radius: 5px;
	}
	.x:hover {
		color: #c33;
		background: #fff0f0;
	}
	.empty {
		color: #888;
		line-height: 1.6;
	}
</style>
