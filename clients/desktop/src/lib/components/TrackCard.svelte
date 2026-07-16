<script lang="ts">
	// Horizontal-rail track card: square art + title + artist, optional rank
	// overlay (for charts). Tap plays from its context. Mirrors iOS rail cards
	// (track cards play on tap; the context menu lives on rows).
	import ArtImg from './ArtImg.svelte';
	import TrackMenu from './TrackMenu.svelte';
	import { playFromList, current } from '$lib/stores/player';
	import { openTrackContextMenu } from '$lib/stores/contextMenu';
	import type { Track } from '$lib/api/types';

	let {
		track,
		context,
		rank
	}: { track: Track; context: Track[]; rank?: number } = $props();

	const i = $derived(context.findIndex((t) => t.id === track.id));

	function openContextMenu(event: MouseEvent) {
		openTrackContextMenu(event, track, context);
	}
</script>

<div class="card-wrap" role="group" aria-label={track.track} oncontextmenu={openContextMenu}>
	<button
		class="card"
		class:playing={$current?.id === track.id}
		onclick={() => i >= 0 && playFromList(context, i)}
	>
		<div class="art">
			<ArtImg ref={track.art} />
			{#if rank !== undefined}<span class="rank">{rank + 1}</span>{/if}
		</div>
		<span class="title">{track.track}</span>
		<span class="artist">{track.artist}</span>
	</button>
	<div class="card-menu"><TrackMenu {track} {context} /></div>
</div>

<style>
	.card {
		display: flex;
		flex-direction: column;
		width: 100%;
		border: none;
		background: transparent;
		text-align: left;
		cursor: pointer;
		padding: 0;
		border-radius: 16px;
	}
	.card-wrap {
		position: relative;
		width: 130px;
		min-width: 130px;
	}
	.card-menu {
		position: absolute;
		top: 6px;
		right: 6px;
		opacity: 0;
		transition: opacity .15s ease;
	}
	.card-wrap:hover .card-menu,
	.card-wrap:focus-within .card-menu { opacity: 1; }
	.card:hover {
		background: var(--surface-strong);
	}
	.art {
		position: relative;
		width: 130px;
		height: 130px;
		border-radius: 16px;
		overflow: hidden;
		background: #253242;
		margin-bottom: 0.35rem;
	}
	.rank {
		position: absolute;
		left: 6px;
		bottom: 4px;
		font-size: 1.6rem;
		font-weight: 800;
		color: #fff;
		text-shadow: 0 1px 4px rgba(0, 0, 0, 0.6);
		font-variant-numeric: tabular-nums;
	}
	.title {
		font-size: 0.82rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.artist {
		font-size: 0.74rem;
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.card.playing .title {
		color: var(--accent-deep);
		font-weight: 600;
	}
</style>
