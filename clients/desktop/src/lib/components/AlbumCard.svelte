<script lang="ts">
	// Horizontal-rail album card: square art + title + artist. Tap opens the
	// album detail route. Mirrors iOS `AlbumCard`.
	import ArtImg from './ArtImg.svelte';
	import { goAlbum, prefetchAlbum } from '$lib/nav';
	import type { Album } from '$lib/api/types';

	let { album, width = 140 }: { album: Album; width?: number } = $props();
</script>

<button class="card" style="width:{width}px" onclick={() => goAlbum(album)} onpointerenter={() => prefetchAlbum(album)}>
	<div class="art" style="width:{width}px;height:{width}px">
		<ArtImg ref={album.art} />
	</div>
	<span class="title">{album.title}</span>
	<span class="artist">{album.artist}{#if album.year} · {album.year}{/if}</span>
</button>

<style>
	.card {
		display: flex;
		flex-direction: column;
		border: none;
		background: transparent;
		text-align: left;
		cursor: pointer;
		padding: 0;
		border-radius: 16px;
	}
	.card:hover {
		background: var(--surface-strong);
	}
	.art {
		border-radius: 16px;
		overflow: hidden;
		background: #253242;
		margin-bottom: 0.35rem;
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
</style>
