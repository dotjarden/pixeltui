<script lang="ts">
	// Home tab — discovery rails in iOS order: hero (recents + mix + for-you),
	// your playlists, made-for-you (recommend), daily mixes, top charts, artists.
	// Online-only rails (recommend/mixes/charts) fetch concurrently on mount and
	// show skeletons until resolved.
	import Rail from './Rail.svelte';
	import TrackCard from './TrackCard.svelte';
	import PlaylistCard from './PlaylistCard.svelte';
	import ArtistCircle from './ArtistCircle.svelte';
	import ArtImg from './ArtImg.svelte';
	import Icon from './Icon.svelte';
	import Skeleton from './Skeleton.svelte';
	import { playFromList, current } from '$lib/stores/player';
	import { liked, playlists, playlistCache, recents, loadPlaylist } from '$lib/stores/library';
	import { artists } from '$lib/stores/catalog';
	import { goCollection } from '$lib/nav';
	import { api } from '$lib/api/client';
	import { status } from '$lib/server';
	import type { Mix, Track } from '$lib/api/types';

	let recSongs = $state<Track[]>([]);
	let recsLoading = $state(true);
	let chartSongs = $state<Track[]>([]);
	let chartsLoading = $state(true);
	let mixes = $state<Mix[]>([]);
	let mixesLoading = $state(true);

	const online = $derived($status === 'ready');

	// hero pool: recents (songs) + first mix + first recommend
	const heroSongs = $derived(($recents as Track[]).slice(0, 6));
	type HeroItem = { kind: 'song' | 'mix' | 'foryou'; track?: Track; mix?: Mix; caption: string };
	const heroItems = $derived.by(() => {
		const items: HeroItem[] = [];
		for (const t of heroSongs.slice(0, 3)) items.push({ kind: 'song', track: t, caption: 'RECENTLY PLAYED' });
		if (mixes[0]) items.push({ kind: 'mix', mix: mixes[0], caption: 'DAILY MIX' });
		if (recSongs[0]) items.push({ kind: 'foryou', track: recSongs[0], caption: 'FOR YOU' });
		return items;
	});
	const primaryHero = $derived(heroItems[0]);
	const secondaryHeroes = $derived(heroItems.slice(1, 4));

	let discoveryStarted = false;

	// The library arrives after the sidecar handshake. Keep playlist artwork
	// loading reactive so Home does not render empty cards forever on a cold boot.
	$effect(() => {
		const names = $playlists.slice(0, 12);
		const cache = $playlistCache;
		for (const name of names) {
			if (!cache[name]) void loadPlaylist(name).catch(() => {});
		}
	});

	// Start discovery when the server becomes ready, not only when Home first
	// mounts. This fixes the common cold-start race between the shell and Tauri.
	$effect(() => {
		if (!online) {
			recsLoading = false;
			chartsLoading = false;
			mixesLoading = false;
			return;
		}
		if (discoveryStarted) return;
		discoveryStarted = true;
		recsLoading = true;
		chartsLoading = true;
		mixesLoading = true;
		void api.recommendations(20, [], [])
			.then((r) => (recSongs = r))
			.catch(() => {})
			.finally(() => (recsLoading = false));
		void api.charts('ZZ')
			.then((c) => (chartSongs = c.tracks))
			.catch(() => {})
			.finally(() => (chartsLoading = false));
		void api.mixes()
			.then((m) => (mixes = m))
			.catch(() => {})
			.finally(() => (mixesLoading = false));
	});

	function playHeroSong(t: Track | undefined) {
		if (!t) return;
		const ctx = heroSongs.length ? heroSongs : [t];
		playFromList(ctx, Math.max(0, ctx.findIndex((x) => x.id === t.id)));
	}
	function heroArt(item: HeroItem): string | undefined {
		return item.track?.art ?? item.mix?.tracks[0]?.art;
	}
	function heroTitle(item: HeroItem): string {
		return item.track?.track ?? item.mix?.title ?? '';
	}
	function heroSubtitle(item: HeroItem): string {
		return item.track?.artist ?? item.mix?.tag ?? '';
	}
	function playHero(item: HeroItem) {
		if (item.track) playHeroSong(item.track);
		else if (item.mix?.tracks.length) playFromList(item.mix.tracks, 0);
	}
</script>

<div class="home-immersive">
	{#if primaryHero}
		<section class="featured-stage" aria-label="Resume listening">
			<article class="feature-artwork">
				<ArtImg ref={heroArt(primaryHero)} eager alt="" />
				<div class="feature-shade">
					<p>{primaryHero.caption}</p>
					<h2>{heroTitle(primaryHero)}</h2>
					<span>{heroSubtitle(primaryHero)}</span>
					<button class="feature-play" class:playing={$current?.id === primaryHero.track?.id} onclick={() => playHero(primaryHero)}><Icon name="play" size={16} /> Play</button>
				</div>
			</article>
			{#if secondaryHeroes.length}
				<aside class="continue-glass" aria-label="Continue listening">
					<p>Continue listening</p>
					{#each secondaryHeroes as item}
						<button onclick={() => playHero(item)}>
							<ArtImg ref={heroArt(item)} alt="" />
							<span><strong>{heroTitle(item)}</strong><small>{heroSubtitle(item)}</small></span>
							<Icon name="play" size={14} />
						</button>
					{/each}
				</aside>
			{/if}
		</section>
	{/if}

	<div class="home-feed">
		{#if !online}
			<div class="banner">Connecting to the embedded server…</div>
		{/if}

{#if $playlists.length}
	<Rail edge title="Your Playlists" action={{ label: 'See All', onclick: () => {} }}>
		{#each $playlists.slice(0, 12) as name}
			<PlaylistCard {name} tracks={$playlistCache[name] ?? []} />
		{/each}
	</Rail>
{/if}

{#if online}
	<Rail edge title="Made For You" action={{ label: 'See All', onclick: () => goCollection({ title: 'Made For You', subtitle: 'Recommended for you', symbol: 'sparkles', tracks: recSongs }) }}>
		{#if recsLoading}
			<Skeleton />
		{:else}
			{#each recSongs.slice(0, 12) as t (t.id)}
				<TrackCard track={t} context={recSongs} />
			{/each}
		{/if}
	</Rail>

	{#if mixes.length}
		<Rail edge title="Daily Mixes">
			{#each mixes as m (m.title)}
				<button class="mixcard" onclick={() => goCollection({ title: m.title, subtitle: m.tag, symbol: 'sparkles', tracks: m.tracks })}>
					<ArtImg ref={m.tracks[0]?.art} />
					<div class="mixmeta"><span class="t">{m.title}</span><span class="s">{m.tag}</span></div>
				</button>
			{/each}
		</Rail>
	{/if}

	<Rail edge title="Top Charts" action={{ label: 'See All', onclick: () => goCollection({ title: 'Top Charts · Global', tracks: chartSongs }) }}>
		{#if chartsLoading}
			<Skeleton />
		{:else}
			{#each chartSongs.slice(0, 12) as t, i (t.id)}
				<TrackCard track={t} context={chartSongs} rank={i} />
			{/each}
		{/if}
	</Rail>
{/if}

{#if $artists.length}
	<Rail edge title="Artists">
		{#each $artists.slice(0, 12) as a}
			<ArtistCircle name={a.name} art={a.art} subtitle={`${a.songs.length} songs`} />
		{/each}
	</Rail>
{/if}

{#if $liked.length === 0 && !recsLoading}
	<p class="empty">Your library is empty. Like a track or search to get started.</p>
{/if}
	</div>
</div>

<style>
	.banner {
		background: var(--accent-soft);
		color: var(--accent-deep);
		padding: .65rem .85rem;
		border-radius: 12px;
		font-size: 0.84rem;
		margin-bottom: 1.35rem;
	}
	.home-immersive { padding-bottom: 2.5rem; }
	.featured-stage {
		position: relative;
		height: clamp(430px, 56vh, 620px);
		margin-inline: clamp(14px, 2.25vw, 34px);
		border-radius: clamp(22px, 2.5vw, 32px);
		background: #242324;
		color: #fff;
		overflow: hidden;
		box-shadow: 0 24px 66px rgba(39, 33, 30, .19), 0 4px 14px rgba(39, 33, 30, .08);
	}
	.feature-artwork {
		position: relative;
		overflow: hidden;
		height: 100%;
		isolation: isolate;
	}
	.feature-artwork :global(img), .feature-artwork :global(.placeholder) {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.feature-shade {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		justify-content: end;
		align-items: start;
		gap: .35rem;
		padding: clamp(30px, 5.5vw, 80px);
		padding-right: min(43vw, 500px);
		background: linear-gradient(90deg, rgba(13,12,12,.61), rgba(13,12,12,.12) 68%), linear-gradient(0deg, rgba(13,12,12,.70), transparent 61%);
		text-align: left;
	}
	.feature-shade p {
		margin: 0 0 .3rem;
		font-size: .68rem;
		font-weight: 780;
		letter-spacing: .13em;
		color: rgba(255,255,255,.72);
	}
	.feature-shade h2 {
		max-width: min(12ch, 84%);
		margin: 0;
		color: #fff !important;
		font-size: clamp(2.4rem, 5vw, 5.8rem);
		font-weight: 760;
		letter-spacing: -.065em;
		line-height: .92;
		text-wrap: balance;
	}
	.feature-shade > span { max-width: 48ch; color: rgba(255,255,255,.78); font-size: .95rem; }
	.feature-play {
		display: inline-flex;
		align-items: center;
		gap: .5rem;
		margin-top: 1.05rem;
		min-height: 42px;
		padding: 0 .95rem;
		border: 0;
		border-radius: 999px;
		background: #fff;
		color: #1e1d1b;
		font: inherit;
		font-size: .82rem;
		font-weight: 760;
		cursor: pointer;
		transition: transform .18s ease, background .18s ease;
	}
	.feature-play:hover { background: var(--accent-soft); transform: translateY(-1px); }
	.feature-play.playing { background: var(--accent); color: #fff; }
	.continue-glass {
		position: absolute;
		right: clamp(16px, 2.5vw, 34px);
		bottom: clamp(16px, 2.5vw, 34px);
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: min(330px, 35vw);
		padding: 14px;
		border: 1px solid rgba(255,255,255,.20);
		border-radius: 18px;
		background: rgba(22, 20, 20, .42);
		box-shadow: 0 12px 30px rgba(0,0,0,.18);
		backdrop-filter: blur(26px) saturate(1.18);
	}
	.continue-glass > p { margin: 2px 5px 7px; color: rgba(255,255,255,.70); font-size: .68rem; font-weight: 760; letter-spacing: .11em; text-transform: uppercase; }
	.continue-glass button { display: grid; grid-template-columns: 40px minmax(0,1fr) 18px; align-items: center; gap: .65rem; width: 100%; padding: .48rem; border: 0; border-radius: 11px; background: transparent; color: #fff; text-align: left; cursor: pointer; }
	.continue-glass button:hover { background: rgba(255,255,255,.14); }
	.continue-glass button :global(img), .continue-glass button :global(.placeholder) { width: 40px; height: 40px; border-radius: 7px; object-fit: cover; }
	.continue-glass button span { min-width: 0; display: grid; gap: .15rem; }
	.continue-glass button strong, .continue-glass button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.continue-glass button strong { font-size: .8rem; font-weight: 680; }
	.continue-glass button small { color: rgba(255,255,255,.62); font-size: .72rem; }
	.continue-glass button :global(svg) { opacity: .55; }
	.home-feed { padding-top: clamp(34px, 4.5vw, 64px); }
	.home-feed :global(.rail) { margin-bottom: clamp(34px, 4vw, 58px) !important; }
	.mixcard {
		display: flex;
		flex-direction: column;
		width: 150px;
		border: none;
		background: transparent;
		cursor: pointer;
		padding: 0;
		border-radius: 16px;
	}
	.mixcard :global(img),
	.mixcard :global(.placeholder) {
		width: 150px;
		height: 150px;
		border-radius: 16px;
		object-fit: cover;
		margin-bottom: 0.35rem;
	}
	.mixmeta {
		display: flex;
		flex-direction: column;
	}
	.mixmeta .t { font-size: .88rem; font-weight: 680; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.mixmeta .s { margin-top: .1rem; color: var(--muted); font-size: .74rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.empty {
		color: #888;
		padding: 1rem 0;
	}
	@media (max-width: 900px) {
		.featured-stage { height: 560px; margin-inline: 12px; border-radius: 22px; }
		.feature-shade { padding: 30px; padding-bottom: 184px; }
		.continue-glass { right: 14px; bottom: 14px; left: 14px; width: auto; display: grid; grid-template-columns: repeat(3, minmax(0,1fr)); padding: 10px; }
		.continue-glass > p { grid-column: 1 / -1; }
		.continue-glass button { grid-template-columns: 36px minmax(0,1fr); gap: .45rem; padding: .38rem; }
		.continue-glass button :global(img), .continue-glass button :global(.placeholder) { width: 36px; height: 36px; }
		.continue-glass button :global(svg) { display: none; }
	}
</style>
