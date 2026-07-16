<script lang="ts">
	// Remote album page: immersive header + numbered track list + a "More by
	// <artist>" rail fetched from /api/artist. Mirrors iOS `RemoteAlbumView`.
	import { page } from '$app/state';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ImmersiveHeader from '$lib/components/ImmersiveHeader.svelte';
	import TrackNumberRow from '$lib/components/TrackNumberRow.svelte';
	import TrackListHeader from '$lib/components/TrackListHeader.svelte';
	import AlbumCard from '$lib/components/AlbumCard.svelte';
	import Rail from '$lib/components/Rail.svelte';
	import { playFromList, playShuffle, startStation } from '$lib/stores/player';
	import { api } from '$lib/api/client';
	import { status } from '$lib/server';
	import type { AlbumPage, ArtistPage } from '$lib/api/types';

	let p = $state<AlbumPage | null>(null);
	let more = $state<ArtistPage | null>(null);
	let loading = $state(true);
	let error = $state('');

	const title = $derived(page.url.searchParams.get('title') ?? 'Album');
	const artist = $derived(page.url.searchParams.get('artist') ?? '');
	const browseId = $derived(page.url.searchParams.get('browse_id') ?? '');
	const online = $derived($status === 'ready');

	let requestSeq = 0;
	async function loadPage(route: { browseId: string; title: string; artist: string }) {
		const seq = ++requestSeq;
		loading = true;
		error = '';
		p = null;
		more = null;
		try {
			const album = await api.albumPage(route.browseId, route.title, route.artist);
			if (seq !== requestSeq) return;
			p = album;
			// "More by artist" rail (exclude the current album by title).
			if (album.artist) {
				api.artistPage(album.artist, true)
					.then((ap) => { if (seq === requestSeq) more = ap; })
					.catch(() => {});
			}
		} catch (e) {
			if (seq === requestSeq) error = e instanceof Error ? e.message : 'Unable to load album';
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	$effect(() => {
		const route = { browseId, title, artist };
		if (online) void loadPage(route);
	});

	const moreAlbums = $derived(
		(more?.albums ?? []).filter((a) => a.title !== (p?.title ?? title))
	);
</script>

<DetailShell title={p?.title ?? title} {loading} immersive>
	{#if p}
		<ImmersiveHeader
			art={p.art}
			title={p.title}
			subtitle={`${p.artist}${p.year ? ` · ${p.year}` : ''}`}
			description={p.description}
			playDisabled={p.tracks.length === 0}
			onPlay={() => p && playFromList(p.tracks, 0)}
			onShuffle={() => p && playShuffle(p.tracks)}
		>
			{#if p?.tracks.length}
				<button class="ed-btn" onclick={() => p && startStation(p.tracks[0])}>Start Radio</button>
			{/if}
		</ImmersiveHeader>

		<section class="album-songs">
			<div class="section-head"><p>Album</p><h2>{p.tracks.length} songs</h2></div>
			<ul class="list track-list numbered">
				<TrackListHeader numbered />
				{#each p.tracks as t, i (t.id)}
					<TrackNumberRow track={t} context={p.tracks} number={i + 1} table />
				{/each}
			</ul>
		</section>

		{#if moreAlbums.length}
			<Rail edge title={`More by ${p.artist}`}>
				{#each moreAlbums as a (a.browse_id || a.title)}
					<AlbumCard album={a} />
				{/each}
			</Rail>
		{/if}
	{:else if !loading}
		<div class="error-state">
			<p>{error || 'Album not found.'}</p>
			{#if error}<button onclick={() => loadPage({ browseId, title, artist })}>Try again</button>{/if}
		</div>
	{/if}
</DetailShell>

<style>
	.album-songs { max-width: 1120px; margin: 0 0 clamp(42px, 5vw, 68px); }
	.section-head { margin-bottom: .85rem; }
	.section-head p { margin: 0 0 .25rem; color: var(--accent-deep); font-size: .68rem; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
	.section-head h2 { margin: 0; font-size: clamp(1.4rem, 2.1vw, 1.9rem); letter-spacing: -.05em; }
	.list { list-style: none; padding: 0; margin: 0; border: 1px solid var(--line); border-radius: 18px; overflow: hidden; background: rgba(255,255,255,.52); box-shadow: 0 8px 24px rgba(47,42,37,.05); }
	.error-state { display: grid; gap: .6rem; justify-items: start; color: #777; }
	.error-state p { margin: 0; }
	.error-state button { border: 1px solid #d8d2cc; border-radius: 999px; background: #fff; padding: .45rem .8rem; cursor: pointer; }
</style>
