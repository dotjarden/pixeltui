<script lang="ts">
	import DetailShell from '$lib/components/DetailShell.svelte';
	import AlbumCard from '$lib/components/AlbumCard.svelte';
	import { albums } from '$lib/stores/catalog';
</script>

<DetailShell title="Albums" subtitle={`${$albums.length} albums`} showHeading>
	{#if $albums.length}
		<div class="grid">
			{#each $albums as album (album.browse_id || album.title)}
				<AlbumCard {album} width={164} />
			{/each}
		</div>
	{:else}
		<p class="empty">Albums will appear as your library syncs.</p>
	{/if}
</DetailShell>

<style>
	.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(164px, 1fr)); gap: 1.4rem 1rem; }
	.grid :global(.card-wrap) { width: 100%; min-width: 0; }
	.empty { color: var(--muted); }
</style>
