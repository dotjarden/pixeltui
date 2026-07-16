<script lang="ts">
	// Local playlist detail: immersive header (Play/Shuffle + rename/delete) +
	// numbered, removable track list. Mirrors iOS `PlaylistDetailView`.
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ImmersiveHeader from '$lib/components/ImmersiveHeader.svelte';
	import TrackNumberRow from '$lib/components/TrackNumberRow.svelte';
	import TrackListHeader from '$lib/components/TrackListHeader.svelte';
	import { playFromList, playShuffle } from '$lib/stores/player';
	import { loadPlaylist, playlistCache, scheduleResync } from '$lib/stores/library';
	import { api } from '$lib/api/client';
	import type { Track } from '$lib/api/types';

	let loading = $state(true);

	const name = $derived(decodeURIComponent($page.params.id ?? ''));
	const tracks = $derived<Track[]>($playlistCache[name] ?? []);
	const art = $derived(tracks.find((t) => t.art)?.art);

	let requestSeq = 0;
	async function loadPage() {
		const seq = ++requestSeq;
		loading = true;
		try {
			await loadPlaylist(name);
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	$effect(() => {
		const key = name;
		void key;
		void loadPage();
	});

	async function removeTrack(t: Track) {
		await api.playlistRemove(name, [t.id]);
		await loadPlaylist(name);
	}

	function rename() {
		const nn = window.prompt('Rename playlist', name);
		if (nn && nn.trim() && nn !== name) {
			void api.playlistRename(name, nn.trim()).then(() => {
				scheduleResync();
				goto(`/playlist/${encodeURIComponent(nn.trim())}`);
			});
		}
	}

	function del() {
		if (window.confirm(`Delete playlist “${name}”?`)) {
			void api.playlistDelete(name).then(() => {
				scheduleResync();
				goto('/');
			});
		}
	}
</script>

<DetailShell title={name} {loading} immersive>
	{#if loading}
		<!-- skeleton -->
	{:else}
		<ImmersiveHeader
			art={art}
			title={name}
			subtitle={`${tracks.length} songs`}
			playDisabled={tracks.length === 0}
			onPlay={() => playFromList(tracks, 0)}
			onShuffle={() => playShuffle(tracks)}
		>
			<button class="ed-btn" onclick={rename}>Rename</button>
			<button class="ed-btn danger" onclick={del}>Delete</button>
		</ImmersiveHeader>

		{#if tracks.length}
			<ul class="list track-list numbered">
				<TrackListHeader numbered />
				{#each tracks as t, i (t.id)}
					<TrackNumberRow track={t} context={tracks} number={i + 1} table onRemove={() => removeTrack(t)} />
				{/each}
			</ul>
		{:else}
			<p class="empty">This playlist is empty. Add tracks from any menu.</p>
		{/if}
	{/if}
</DetailShell>

<style>
	.list {
		list-style: none;
		padding: 0;
		margin: 0;
		border: 1px solid var(--line);
		border-radius: 18px;
		overflow: hidden;
		background: rgba(255,255,255,.52);
		box-shadow: 0 8px 24px rgba(47,42,37,.05);
	}
	.empty {
		color: #888;
	}
</style>
