<script lang="ts">
	// Remote artist page: immersive header + top songs + albums/singles/similar
	// rails. Mirrors iOS `RemoteArtistView`.
	import { page } from '$app/state';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import TrackNumberRow from '$lib/components/TrackNumberRow.svelte';
	import TrackListHeader from '$lib/components/TrackListHeader.svelte';
	import AlbumCard from '$lib/components/AlbumCard.svelte';
	import ArtistCircle from '$lib/components/ArtistCircle.svelte';
	import ArtImg from '$lib/components/ArtImg.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import Rail from '$lib/components/Rail.svelte';
	import { playFromList, playShuffle, startStation } from '$lib/stores/player';
	import { goCollection } from '$lib/nav';
	import { api } from '$lib/api/client';
	import { status } from '$lib/server';
	import type { ArtistPage } from '$lib/api/types';

	let p = $state<ArtistPage | null>(null);
	let loading = $state(true);
	let error = $state('');

	function decodeRouteName(value: string): string {
		try { return decodeURIComponent(value); } catch { return value; }
	}

	const name = $derived(decodeRouteName(page.params.name ?? ''));
	const browseId = $derived(page.url.searchParams.get('browse_id') ?? '');
	const routeArt = $derived(page.url.searchParams.get('art') ?? '');
	const online = $derived($status === 'ready');

	let requestSeq = 0;
	async function loadExtras(routeName: string, seq: number) {
		try {
			const extras = await api.artistExtras(routeName);
			if (seq === requestSeq && p) p = { ...p, ...extras };
		} catch {
			// Stats and recommendations are progressive enhancement; the artist
			// page is already usable without them.
		}
	}

	async function loadPage(routeName: string, routeBrowseId = '', initialArt = '') {
		const seq = ++requestSeq;
		loading = true;
		error = '';
		p = null;
		try {
			const artist = await api.artistPage(routeName, true, routeBrowseId, initialArt);
			if (seq === requestSeq) {
				p = artist;
				void loadExtras(routeName, seq);
			}
		} catch (e) {
			if (seq === requestSeq) error = e instanceof Error ? e.message : 'Unable to load artist';
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	$effect(() => {
		const routeName = name;
		const routeBrowseId = browseId;
		const initialArt = routeArt;
		if (routeName && online) void loadPage(routeName, routeBrowseId, initialArt);
	});

	// Artist artwork is independent of releases. Never substitute an album or
	// track cover here: an absent portrait should use ArtImg's neutral fallback.
	const art = $derived(p?.art ?? routeArt ?? undefined);
	const subtitle = $derived.by(() => {
		const parts: string[] = [];
		if (p?.stats?.listeners) parts.push(`${p.stats.listeners.toLocaleString()} listeners`);
		if (p?.stats?.playcount) parts.push(`${p.stats.playcount.toLocaleString()} plays`);
		return parts.join(' · ');
	});
	const description = $derived(p?.description ?? p?.stats?.bio);
	const topTen = $derived(p?.top_songs.slice(0, 10) ?? []);
</script>

<DetailShell title={p?.name ?? name} {loading} immersive>
	{#if p}
		{@const artist = p}
		<section class="artist-stage" aria-label={artist.name}>
			<ArtImg ref={art} eager alt="" />
			<div class="stage-shade"></div>
			<div class="stage-content">
				<div class="stage-copy">
					<p class="stage-kicker">Artist</p>
					<h1>{artist.name}</h1>
					{#if subtitle}<p class="stage-stats">{subtitle}</p>{/if}
					{#if description}<p class="stage-bio">{description}</p>{/if}
				</div>
				<div class="stage-actions">
					<button class="stage-play" onclick={() => playFromList(artist.top_songs, 0)} disabled={artist.top_songs.length === 0}><Icon name="play" size={16} /> Play</button>
					<button class="stage-icon" onclick={() => playShuffle(artist.top_songs)} disabled={artist.top_songs.length === 0} title="Shuffle" aria-label="Shuffle"><Icon name="shuffle" size={17} /></button>
					{#if artist.top_songs.length}<button class="stage-radio" onclick={() => startStation(artist.top_songs[0])}>Radio</button>{/if}
				</div>
			</div>
		</section>

		{#if artist.top_songs.length}
			<section class="top-songs">
				<div class="section-head"><div><p>Most played</p><h2>Top Songs</h2></div></div>
				<ul class="list track-list numbered">
					<TrackListHeader numbered />
					{#each topTen as t, i (t.id)}
						<TrackNumberRow track={t} context={artist.top_songs} number={i + 1} table />
					{/each}
				</ul>
				{#if artist.top_songs.length > 10}<button class="all-songs" onclick={() => goCollection({ title: `${artist.name} · Top Songs`, subtitle: 'Top songs', tracks: artist.top_songs })}><span>View all {artist.top_songs.length} songs</span><Icon name="next" size={16} /></button>{/if}
			</section>
		{/if}

		{#if artist.albums.length}
			<Rail edge title="Albums">
				{#each artist.albums as a (a.browse_id || a.title)}
					<AlbumCard album={a} />
				{/each}
			</Rail>
		{/if}

		{#if artist.singles.length}
			<Rail edge title="Singles">
				{#each artist.singles as a (a.browse_id || a.title)}
					<AlbumCard album={a} />
				{/each}
			</Rail>
		{/if}

		{#if artist.similar_artists?.length}
			<Rail edge title="Similar Artists">
				{#each artist.similar_artists as a (a.name)}
					<ArtistCircle name={a.name} art={a.art} browseId={a.browse_id} subtitle={a.listeners ? `${a.listeners}` : undefined} />
				{/each}
			</Rail>
		{/if}
	{:else if !loading}
		<div class="error-state">
			<p>{error || 'Artist not found.'}</p>
			{#if error}<button onclick={() => loadPage(name, browseId, routeArt)}>Try again</button>{/if}
		</div>
	{/if}
</DetailShell>

<style>
	/* The portrait is the page, not an object beside a title. The stage extends
	 * to the app edges and keeps actions at the point of intent. */
	.artist-stage {
		position: relative;
		isolation: isolate;
		height: clamp(460px, 59vh, 670px);
		width: calc(100vw - 32px);
		margin: 0 0 clamp(42px, 5vw, 72px) calc((100% - 100vw) / 2 + 16px);
		overflow: hidden;
		border-radius: 28px;
		background: #252321;
		box-shadow: 0 24px 64px rgba(47,42,37,.19), 0 5px 16px rgba(47,42,37,.08);
		color: #fff;
	}
	.artist-stage :global(img), .artist-stage :global(.placeholder) { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
	.artist-stage :global(img) { object-position: 50% 28%; filter: saturate(1.06) contrast(1.03); }
	.stage-shade { position: absolute; z-index: 1; inset: 0; background: linear-gradient(90deg, rgba(12,11,10,.74) 0%, rgba(12,11,10,.25) 58%, rgba(12,11,10,.08) 100%), linear-gradient(0deg, rgba(12,11,10,.78), transparent 56%); }
	.stage-content { position: absolute; z-index: 2; inset: 0; display: flex; flex-direction: column; justify-content: flex-end; align-items: flex-start; padding: clamp(28px, 5vw, 76px); }
	.stage-copy { max-width: min(680px, 68%); }
	.stage-kicker { margin: 0 0 .55rem; color: rgba(255,255,255,.7) !important; font-size: .68rem; font-weight: 800; letter-spacing: .14em; text-transform: uppercase; }
	.artist-stage h1 { margin: 0; color: #fff !important; font-size: clamp(3.8rem, 8.5vw, 9.4rem); font-weight: 790; letter-spacing: -.085em; line-height: .8; text-wrap: balance; }
	.stage-stats { margin: 1rem 0 0; color: rgba(255,255,255,.75) !important; font-size: .9rem; }
	.stage-bio { display: -webkit-box; max-width: 64ch; margin: .85rem 0 0; overflow: hidden; color: rgba(255,255,255,.72) !important; font-size: .86rem; line-height: 1.5; -webkit-box-orient: vertical; -webkit-line-clamp: 2; line-clamp: 2; }
	.stage-actions { display: flex; align-items: center; gap: .5rem; margin-top: 1.25rem; }
	.stage-play, .stage-icon, .stage-radio { display: inline-flex; align-items: center; justify-content: center; gap: 7px; min-height: 42px; border: 1px solid rgba(255,255,255,.18); font: inherit; font-size: .82rem; font-weight: 720; cursor: pointer; }
	.stage-play { padding: 0 1.05rem; border-color: #fff; border-radius: 999px; background: #fff; color: #24211f; box-shadow: 0 8px 22px rgba(0,0,0,.18); }
	.stage-icon { width: 42px; padding: 0; border-radius: 50%; background: rgba(20,18,17,.38); color: #fff; backdrop-filter: blur(12px); }
	.stage-radio { padding: 0 .95rem; border-radius: 999px; background: rgba(20,18,17,.38); color: #fff; backdrop-filter: blur(12px); }
	.stage-play:hover { background: var(--accent-soft); }
	.stage-icon:hover, .stage-radio:hover { background: rgba(255,255,255,.18); }
	.stage-play:disabled, .stage-icon:disabled { opacity: .45; cursor: default; }
	.top-songs { max-width: 1120px; margin: 0 0 clamp(46px, 5vw, 70px); }
	.section-head { margin-bottom: .85rem; }
	.section-head p { margin: 0 0 .25rem; color: var(--accent-deep); font-size: .68rem; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
	.section-head h2 { margin: 0; font-size: clamp(1.45rem, 2.2vw, 2rem); letter-spacing: -.055em; }
	.list { list-style: none; padding: 0; margin: 0; border: 1px solid var(--line); border-radius: 18px; overflow: hidden; background: rgba(255,255,255,.52); box-shadow: 0 8px 24px rgba(47,42,37,.05); }
	.all-songs { display: flex; align-items: center; justify-content: space-between; width: 100%; min-height: 52px; margin-top: 10px; padding: 0 .9rem 0 1rem; border: 1px solid var(--line); border-radius: 14px; background: rgba(255,255,255,.6); color: var(--accent-deep); font: inherit; font-size: .84rem; font-weight: 720; cursor: pointer; }
	.all-songs:hover { background: var(--surface-strong); border-color: var(--line-strong); }
	.error-state { display: grid; gap: .6rem; justify-items: start; color: #777; }
	.error-state p { margin: 0; }
	.error-state button { border: 1px solid #d8d2cc; border-radius: 999px; background: #fff; padding: .45rem .8rem; cursor: pointer; }
	@container (max-width: 700px) { .artist-stage { height: 540px; width: calc(100vw - 24px); margin-left: calc((100% - 100vw) / 2 + 12px); border-radius: 22px; } .stage-content { padding: 28px; } .stage-copy { max-width: 92%; } .artist-stage h1 { font-size: clamp(3.5rem, 13vw, 6rem); } .stage-bio { display: none; } }
	@media (prefers-reduced-motion: reduce) { .stage-play, .stage-icon, .stage-radio { transition: none; } }
</style>
