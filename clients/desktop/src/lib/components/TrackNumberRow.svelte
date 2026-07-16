<script lang="ts">
	import TrackMenu from './TrackMenu.svelte';
	import ArtImg from './ArtImg.svelte';
	import { playFromList, current, isPlaying } from '$lib/stores/player';
	import { likedIds } from '$lib/stores/library';
	import { openTrackContextMenu } from '$lib/stores/contextMenu';
	import type { Track } from '$lib/api/types';
	import Icon from './Icon.svelte';

	let {
		track,
		context,
		number,
		onRemove,
		table = false
	}: { track: Track; context: Track[]; number: number; onRemove?: () => void; table?: boolean } = $props();

	const index = $derived(context.findIndex((item) => item.id === track.id));
	const isCurrent = $derived($current?.id === track.id);

	function fmt(seconds: number): string {
		const minutes = Math.floor(seconds / 60);
		return `${minutes}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
	}

	function openContextMenu(event: MouseEvent) {
		openTrackContextMenu(event, track, context, onRemove);
	}
</script>

<li class:playing={isCurrent} class:table-row={table} oncontextmenu={openContextMenu}>
	<button class="row" onclick={() => index >= 0 && playFromList(context, index)}>
		<span class="slot">
			{#if isCurrent}
				<span class="eq" class:active={$isPlaying}><span></span><span></span><span></span></span>
			{:else}
				<span class="num">{number}</span>
			{/if}
		</span>
		<ArtImg ref={track.art} size="42px" />
		<span class="title">{track.track}</span>
		{#if table}
			<span class="artist">{track.artist || '—'}</span>
			<span class="album">{track.album || '—'}</span>
		{:else if $likedIds.has(track.id)}
			<span class="liked" title="Liked"><Icon name="heart" size={14} /></span>
		{/if}
		<span class="duration">{fmt(track.duration)}</span>
	</button>
	<TrackMenu {track} {context} {onRemove} />
</li>

<style>
	li { display: flex; align-items: center; min-width: 0; border-bottom: 1px solid var(--line); }
	li.playing { background: var(--accent-soft); }
	.row { display: flex; align-items: center; gap: .8rem; flex: 1; min-width: 0; padding: .4rem .3rem; border: 0; border-radius: 6px; background: transparent; color: inherit; text-align: left; cursor: pointer; }
	.row :global(img), .row :global(.placeholder) { width: 42px; height: 42px; flex: 0 0 auto; border-radius: 7px; object-fit: cover; }
	.slot { width: 1.6rem; flex: 0 0 1.6rem; text-align: center; }
	.num, .duration { color: var(--muted); font-size: .78rem; font-variant-numeric: tabular-nums; }
	.eq { display: inline-flex; align-items: flex-end; gap: 2px; height: 14px; }
	.eq span { width: 3px; height: 4px; border-radius: 2px; background: var(--accent); }
	.eq.active span { animation: eq .9s ease-in-out infinite; }
	.eq.active span:nth-child(2) { animation-delay: .3s; }
	.eq.active span:nth-child(3) { animation-delay: .15s; }
	@keyframes eq { 0%, 100% { height: 4px; } 50% { height: 14px; } }
	.title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .9rem; }
	li.playing .title { color: var(--accent-deep); font-weight: 650; }
	.liked { color: #e0466e; }
	.duration { margin-left: auto; }

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
