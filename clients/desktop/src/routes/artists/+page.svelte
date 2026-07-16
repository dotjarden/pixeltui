<script lang="ts">
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ArtistCircle from '$lib/components/ArtistCircle.svelte';
	import { artists } from '$lib/stores/catalog';
</script>

<DetailShell title="Artists" subtitle={`${$artists.length} artists`} showHeading>
	{#if $artists.length}
		<div class="grid">
			{#each $artists as artist (artist.name)}
				<ArtistCircle name={artist.name} art={artist.art} subtitle={`${artist.songs.length} songs`} size={92} />
			{/each}
		</div>
	{:else}
		<p class="empty">Artists will appear as your library syncs.</p>
	{/if}
</DetailShell>

<style>
	.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(112px, 1fr)); gap: 1.35rem 1rem; }
	.grid :global(.artist) { width: 100%; }
	.empty { color: var(--muted); }
</style>
