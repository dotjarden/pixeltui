<script lang="ts">
	import DetailShell from '$lib/components/DetailShell.svelte';
	import { playlists } from '$lib/stores/library';
	import { goPlaylist } from '$lib/nav';
	import Icon from '$lib/components/Icon.svelte';
</script>

<DetailShell title="Playlists" subtitle={`${$playlists.length} playlists`} showHeading>
	{#if $playlists.length}
		<ul class="list">
			{#each $playlists as name (name)}
				<li><button onclick={() => goPlaylist(name)}><span class="icon"><Icon name="queue" size={18}/></span><span>{name}</span><Icon name="next" size={15}/></button></li>
			{/each}
		</ul>
	{:else}
		<p class="empty">Playlists will appear as your library syncs.</p>
	{/if}
</DetailShell>

<style>
	.list { list-style: none; padding: 0; margin: 0; border: 1px solid var(--line); border-radius: 16px; overflow: hidden; background: var(--surface); }
	li + li { border-top: 1px solid var(--line); }
	li button { display: flex; align-items: center; gap: .8rem; width: 100%; padding: .9rem 1rem; border: 0; background: transparent; color: var(--text); text-align: left; cursor: pointer; }
	li button:hover { background: var(--surface-strong); }
	li button > span:nth-child(2) { flex: 1; }
	.icon { display: grid; place-items: center; color: var(--accent-deep); }
	.empty { color: var(--muted); }
</style>
