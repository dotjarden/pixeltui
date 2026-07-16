<script lang="ts">
	import ArtImg from './ArtImg.svelte';
	import TrackMenu from './TrackMenu.svelte';
	import { playFromList, addToQueue, current } from '$lib/stores/player';
	import { likedIds } from '$lib/stores/library';
	import { openTrackContextMenu } from '$lib/stores/contextMenu';
	import type { Track } from '$lib/api/types';
	import Icon from './Icon.svelte';

	let {
		track,
		context,
		index,
		time,
		table = false
	}: { track: Track; context: Track[]; index: number; time?: string; table?: boolean } = $props();

	function fmt(seconds: number): string {
		const minutes = Math.floor(seconds / 60);
		return `${minutes}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
	}

	function openContextMenu(event: MouseEvent) {
		openTrackContextMenu(event, track, context);
	}
</script>

<li class:playing={$current?.id === track.id} class:table-row={table} oncontextmenu={openContextMenu}>
	<button class="row" onclick={() => playFromList(context, index)}>
		<ArtImg ref={track.art} size="42px" />
		<span class="title">{track.track}</span>
		{#if table}
			<span class="artist">{track.artist || '—'}</span>
			<span class="album">{track.album || '—'}</span>
		{:else}
			<span class="artist compact">{track.artist}{#if track.album} · {track.album}{/if}</span>
		{/if}
		{#if !table && $likedIds.has(track.id)}<span class="liked" title="Liked"><Icon name="heart" size={14} /></span>{/if}
		<span class="duration">{time ?? fmt(track.duration)}</span>
	</button>
	{#if !table}<button class="add" onclick={() => addToQueue(track)} title="Add to queue" aria-label="Add to queue"><Icon name="plus" size={16} /></button>{/if}
	<TrackMenu {track} {context} />
</li>

<style>
	li { display: flex; align-items: center; min-width: 0; border-bottom: 1px solid var(--line); }
	li.playing { background: var(--accent-soft); }
	.row { display: flex; align-items: center; gap: .7rem; flex: 1; min-width: 0; padding: .35rem .3rem; border: 0; border-radius: 6px; background: transparent; color: inherit; text-align: left; cursor: pointer; }
	.row :global(img), .row :global(.placeholder) { width: 42px; height: 42px; flex: 0 0 auto; border-radius: 7px; object-fit: cover; }
	.title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .88rem; }
	li.playing .title { color: var(--accent-deep); font-weight: 650; }
	.artist { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--muted); font-size: .78rem; }
	.compact { max-width: 42%; }
	.duration { margin-left: auto; color: var(--muted); font-size: .76rem; font-variant-numeric: tabular-nums; white-space: nowrap; }
	.liked { color: #e0466e; }
	.add { padding: 0 .5rem; border: 0; background: transparent; color: var(--muted); cursor: pointer; }
	.add:hover { color: var(--accent-deep); }

	li.table-row { display: grid; grid-template-columns: var(--track-grid); column-gap: var(--track-gap); padding: 0 var(--track-inline); }
	.table-row .row { display: grid; grid-column: 1 / -2; grid-template-columns: var(--track-content-grid); column-gap: var(--track-gap); padding: .42rem 0; }
	.table-row :global(.wrap) { align-self: center; }
	.table-row .title, .table-row .artist, .table-row .album { align-self: center; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.table-row .artist, .table-row .album { color: var(--muted); font-size: .8rem; }
	.table-row .duration { align-self: center; margin: 0; text-align: right; }
	@container (max-width: 520px) {
		li.table-row { display: flex; padding: 0; }
		.table-row .row { display: flex; padding: .4rem .3rem; }
		.table-row .artist, .table-row .album { display: none; }
		.table-row .duration { margin-left: auto; }
	}
</style>
