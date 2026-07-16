<script lang="ts">
	// Circular artist avatar + name. Tap opens the artist detail route.
	// Mirrors iOS `ArtistCircle`.
	import ArtImg from './ArtImg.svelte';
	import { goArtist, prefetchArtist } from '$lib/nav';
	import { api } from '$lib/api/client';
	import type { ArtistHit } from '$lib/api/types';

	let { name, art, subtitle, browseId = '', size = 76 }: { name: string; art?: string; subtitle?: string; browseId?: string; size?: number } = $props();
	let resolved = $state<ArtistHit | null>(null);
	let opening = $state(false);
	let resolutionKey = '';
	const displayArt = $derived(resolved?.art || art);
	const displayBrowseId = $derived(resolved?.browse_id || browseId);

	// Last.fm often gives us an image but not YouTube's canonical artist ID.
	// Resolve that card independently, after the rail is painted, so it cannot
	// hold the artist page hostage. It also fills any artwork Last.fm omitted.
	$effect(() => {
		const key = `${name}|${art ?? ''}|${browseId}`;
		if (key === resolutionKey) return;
		resolutionKey = key;
		resolved = null;
		if (art && browseId) return;
		void api.resolveArtist(name).then((hit) => {
			if (resolutionKey === key) resolved = hit;
		}).catch(() => {});
	});

	async function openArtist() {
		if (opening) return;
		opening = true;
		try {
			// Do not route before this card has had a chance to obtain its stable
			// browse ID. Otherwise a fast click races the background lookup and
			// sends the detail page through fragile name-only resolution.
			let hit = resolved;
			if (!displayBrowseId) {
				hit = await api.resolveArtist(name, true).catch(() => null);
				if (hit) resolved = hit;
			}
			goArtist(name, hit?.browse_id || displayBrowseId, hit?.art || displayArt);
		} finally {
			opening = false;
		}
	}
</script>

<button class="artist" onclick={openArtist} onpointerenter={() => prefetchArtist(name, displayBrowseId, displayArt)} aria-busy={opening}>
	<div class="circle" style="width:{size}px;height:{size}px">
		<ArtImg ref={displayArt} radius={size} />
	</div>
	<span class="name">{name}</span>
	{#if subtitle}<span class="sub">{subtitle}</span>{/if}
</button>

<style>
	.artist {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.3rem;
		border: none;
		background: transparent;
		cursor: pointer;
		padding: 0;
		width: 96px;
	}
	.circle {
		border-radius: 50%;
		overflow: hidden;
		background: linear-gradient(135deg, #c9d4ef, #e7c9ef);
	}
	.name {
		font-size: 0.78rem;
		text-align: center;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 96px;
	}
	.sub {
		font-size: 0.68rem;
		color: #888;
	}
</style>
