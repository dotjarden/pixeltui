<script lang="ts">
	// Playlist rail card: 2×2 collage of the playlist's first distinct album
	// arts (single-cover fallback), title + "Playlist · N songs". Tap opens the
	// playlist route. Mirrors iOS `playlistCard` + `CollageArtwork`.
	import { goPlaylist } from '$lib/nav';
	import ArtImg from './ArtImg.svelte';
	import type { Track } from '$lib/api/types';

	let { name, tracks }: { name: string; tracks: Track[] } = $props();

	// up to 4 distinct album arts
	const arts = $derived.by(() => {
		const seen = new Set<string>();
		const out: (string | undefined)[] = [];
		for (const t of tracks) {
			const key = t.album ?? t.id;
			if (seen.has(key)) continue;
			seen.add(key);
			out.push(t.art);
			if (out.length >= 4) break;
		}
		return out;
	});
	const tiles = $derived(Array.from({ length: 4 }, (_, i) => arts[i % arts.length]));
	const initials = $derived(name.split(/\s+/).slice(0, 2).map((word) => word[0]).join('').toUpperCase());
</script>

<button class="card" onclick={() => goPlaylist(name)}>
	<div class="collage" class:single={arts.length === 1}>
		{#if arts.length === 0}
			<div class="generated-cover" aria-hidden="true">{initials || 'P'}</div>
		{:else}
			{#each tiles as art, i (i)}<ArtImg ref={art} />{/each}
		{/if}
	</div>
	<span class="title">{name}</span>
	<span class="sub">Playlist · {tracks.length} songs</span>
</button>

<style>
	.card {
		display: flex;
		flex-direction: column;
		width: 132px;
		border: none;
		background: transparent;
		text-align: left;
		cursor: pointer;
		padding: 0;
		border-radius: 16px;
	}
	.card:hover {
		background: rgba(255,255,255,.06);
	}
	.collage {
		width: 132px;
		height: 132px;
		border-radius: 16px;
		overflow: hidden;
		background: #263748;
		margin-bottom: 0.35rem;
		display: grid;
		grid-template-columns: 1fr 1fr;
		grid-template-rows: 1fr 1fr;
	}
	.collage :global(img), .collage :global(.placeholder) {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.collage.single :global(img), .collage.single :global(.placeholder) {
		grid-column: 1 / -1;
		grid-row: 1 / -1;
	}
	.generated-cover {
		grid-column: 1 / -1;
		grid-row: 1 / -1;
		display: grid;
		place-items: center;
		background: linear-gradient(145deg, #495d85, #a05e6a);
		color: rgba(255,255,255,.94);
		font-size: 2rem;
		font-weight: 760;
		letter-spacing: -.08em;
	}
	.title {
		font-size: 0.82rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		font-size: 0.72rem;
		color: #91a0af;
	}
</style>
