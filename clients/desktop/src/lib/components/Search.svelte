<script lang="ts">
	// Search tab — empty query shows browse (charts carousel for 6 countries +
	// stations mosaic of 10 genres); a non-empty query shows unified results
	// (top result, artists rail, songs, albums rail, your-library matches).
	// Remote search is debounced 350ms; local matches 120ms — mirroring iOS.
	import { onDestroy } from 'svelte';
	import { searchQuery } from '$lib/stores/ui';
	import TrackRow from './TrackRow.svelte';
	import TrackCard from './TrackCard.svelte';
	import AlbumCard from './AlbumCard.svelte';
	import ArtistCircle from './ArtistCircle.svelte';
	import ArtImg from './ArtImg.svelte';
	import Skeleton from './Skeleton.svelte';
	import { liked, sources } from '$lib/stores/library';
	import { artists, albums } from '$lib/stores/catalog';
	import { play, current } from '$lib/stores/player';
	import { goChart, goCollection } from '$lib/nav';
	import { api } from '$lib/api/client';
	import { status } from '$lib/server';
	import type { Album, ArtistHit, Entities, Track } from '$lib/api/types';
	import Icon from './Icon.svelte';

	let query = $state('');
	let trimmed = $derived(query.trim());
	$effect(() => {
		if ($searchQuery !== query) query = $searchQuery;
	});

	// remote results
	let remoteSongs = $state<Track[]>([]);
	let remoteArtists = $state<ArtistHit[]>([]);
	let remoteAlbums = $state<Album[]>([]);
	let remoteSearching = $state(false);
	let nothingFound = $state(false);

	// local matches
	let matchedSongs = $state<Track[]>([]);
	let matchedArtists = $state<{ name: string; art?: string }[]>([]);
	let matchedAlbums = $state<Album[]>([]);

	// charts carousel
	const COUNTRIES: { id: string; label: string }[] = [
		{ id: 'ZZ', label: 'Global' },
		{ id: 'US', label: 'United States' },
		{ id: 'GB', label: 'United Kingdom' },
		{ id: 'DE', label: 'Germany' },
		{ id: 'JP', label: 'Japan' },
		{ id: 'BR', label: 'Brazil' }
	];
	let charts: Record<string, Track[]> = $state({});
	let chartsLoading = $state(true);

	const GENRES = ['pop', 'rock', 'hip hop', 'electronic', 'indie', 'rnb', 'jazz', 'metal', 'country', 'classical'];
	const GENRE_COLORS: Record<string, string> = {
		pop: '#ff8fab', rock: '#e0664b', 'hip hop': '#6c5ce7', electronic: '#00b8a9',
		indie: '#f6c453', rnb: '#8e44ad', jazz: '#34495e', metal: '#2c3e50',
		country: '#d4a373', classical: '#7f8c8d'
	};
	let stationBusy = $state<string | null>(null);

	const online = $derived($status === 'ready');

	let remoteTimer: ReturnType<typeof setTimeout> | null = null;
	let localTimer: ReturnType<typeof setTimeout> | null = null;
	let remoteSeq = 0;

	onDestroy(() => {
		if (remoteTimer) clearTimeout(remoteTimer);
		if (localTimer) clearTimeout(localTimer);
	});

	// Load charts once the server is online (covers online-at-mount and
	// coming-online-after-offline). Re-runs as `online` changes.
	let chartsLoaded = false;
	$effect(() => {
		if (online && !chartsLoaded) {
			chartsLoaded = true;
			void loadCharts();
		}
	});

	async function loadCharts() {
		chartsLoading = true;
		const loadOne = async (country: { id: string }) => {
			try {
				const r = await api.charts(country.id);
				charts = { ...charts, [country.id]: r.tracks };
			} catch {
				/* leave empty */
			}
		};
		// Paint the first useful card before warming the less important regions.
		await loadOne(COUNTRIES[0]);
		chartsLoading = false;
		setTimeout(() => {
			void Promise.all(COUNTRIES.slice(1).map(loadOne));
		}, 0);
	}

	// debounce remote search 350ms on query change
	$effect(() => {
		const q = trimmed;
		const sourceSignature = $sources.join('|');
		void sourceSignature;
		if (remoteTimer) clearTimeout(remoteTimer);
		if (!online) {
			remoteSearching = false;
			return;
		}
		if (q.length < 2) {
			remoteSongs = [];
			remoteArtists = [];
			remoteAlbums = [];
			nothingFound = false;
			remoteSearching = false;
			return;
		}
		remoteSearching = true;
		remoteTimer = setTimeout(() => void runRemoteSearch(q), 350);
	});

	// debounce local matches 120ms
	$effect(() => {
		const q = trimmed.toLowerCase();
		if (localTimer) clearTimeout(localTimer);
		if (!q) {
			matchedSongs = [];
			matchedArtists = [];
			matchedAlbums = [];
			return;
		}
		localTimer = setTimeout(() => {
			matchedSongs = $liked.filter((t) => matches(t, q)).slice(0, 4);
			matchedArtists = $artists.filter((a) => a.name.toLowerCase().includes(q)).slice(0, 2).map((a) => ({ name: a.name, art: a.art }));
			matchedAlbums = $albums.filter((a) => a.title.toLowerCase().includes(q) || a.artist.toLowerCase().includes(q)).slice(0, 2);
		}, 120);
	});

	function matches(t: Track, q: string): boolean {
		return (
			t.track.toLowerCase().includes(q) ||
			t.artist.toLowerCase().includes(q) ||
			(t.album ?? '').toLowerCase().includes(q)
		);
	}

	/** Build useful entity shelves from playable hits when a provider's richer
	 * entity search is unavailable. This is deliberately a fallback, not a
	 * replacement: canonical browse IDs from `/api/search/entities` still win. */
	function entitiesFromTracks(tracks: Track[]): Entities {
		const artists = new Map<string, ArtistHit>();
		const albums = new Map<string, Album>();
		for (const track of tracks) {
			const artist = track.artist.trim();
			if (artist) {
				const key = artist.toLocaleLowerCase();
				if (!artists.has(key)) artists.set(key, { name: artist, art: track.art });
			}
			const title = track.album?.trim();
			if (title) {
				const key = `${title}|${artist}`.toLocaleLowerCase();
				if (!albums.has(key)) albums.set(key, { title, artist, browse_id: '', art: track.art });
			}
		}
		return { artists: [...artists.values()].slice(0, 8), albums: [...albums.values()].slice(0, 10) };
	}

	function mergeEntities(preferred: Entities, tracks: Track[]): Entities {
		const fallback = entitiesFromTracks(tracks);
		const artists = new Map<string, ArtistHit>();
		for (const artist of [...preferred.artists, ...fallback.artists]) {
			const key = artist.name.toLocaleLowerCase();
			if (!artists.has(key)) artists.set(key, artist);
		}
		const albums = new Map<string, Album>();
		for (const album of [...preferred.albums, ...fallback.albums]) {
			const key = `${album.title}|${album.artist}`.toLocaleLowerCase();
			if (!albums.has(key)) albums.set(key, album);
		}
		return { artists: [...artists.values()].slice(0, 8), albums: [...albums.values()].slice(0, 10) };
	}

	async function runRemoteSearch(q: string) {
		const seq = ++remoteSeq;
		let songsDone = false;
		let entitiesDone = false;
		remoteSongs = [];
		remoteArtists = [];
		remoteAlbums = [];
		nothingFound = false;
		const updateEmpty = () => {
			if (seq !== remoteSeq || !songsDone || !entitiesDone) return;
			nothingFound = remoteSongs.length === 0 && remoteArtists.length === 0 && remoteAlbums.length === 0;
		};

		// iOS searches every configured source concurrently. The old desktop path
		// silently defaulted to YouTube, then blocked all painting on the slower
		// entities request. Paint songs as soon as they arrive and hydrate artist
		// and album rails independently.
		const searchSources = $sources.length ? [...$sources] : ['youtube'];
		let pendingSources = searchSources.length;
		const seen = new Set<string>();
		for (const source of searchSources) {
			void api.search(q, source)
				.then((tracks) => {
					if (seq !== remoteSeq) return;
					// Paint the first source immediately instead of waiting for the
					// slowest provider, while still deduplicating the combined rail.
					remoteSongs = remoteSongs.concat(tracks.filter((track) => {
						if (seen.has(track.id)) return false;
						seen.add(track.id);
						return true;
					}));
					// Paint artist and album types as soon as tracks arrive. The
					// dedicated entity request may arrive later (or fail) but it must
					// never leave desktop search looking like a songs-only list.
					const merged = mergeEntities({ artists: remoteArtists, albums: remoteAlbums }, remoteSongs);
					remoteArtists = merged.artists;
					remoteAlbums = merged.albums;
				})
				.catch(() => {})
				.finally(() => {
					if (seq !== remoteSeq) return;
					pendingSources -= 1;
					if (pendingSources === 0) {
						songsDone = true;
						remoteSearching = false;
						updateEmpty();
					}
				});
		}

		void api.searchEntities(q)
			.then((ents) => {
				if (seq !== remoteSeq) return;
				const merged = mergeEntities({ artists: ents.artists ?? [], albums: ents.albums ?? [] }, remoteSongs);
				remoteArtists = merged.artists;
				remoteAlbums = merged.albums;
			})
			.catch(() => {
				if (seq !== remoteSeq) return;
				const merged = mergeEntities({ artists: [], albums: [] }, remoteSongs);
				remoteArtists = merged.artists;
				remoteAlbums = merged.albums;
			})
			.finally(() => {
				if (seq !== remoteSeq) return;
				entitiesDone = true;
				updateEmpty();
			});
	}

	async function playStation(tag: string) {
		stationBusy = tag;
		try {
			const r = await api.station(tag);
			if (r.tracks.length) play(r.tracks[0], r.tracks);
		} catch {
			/* ignore */
		} finally {
			stationBusy = null;
		}
	}

	const topResult = $derived(remoteSongs[0]);
	const resultSongs = $derived(remoteSongs.slice(1, 9));
</script>

<div class="search-page" class:has-results={!!trimmed}>
{#if !trimmed}
	{#if online}
		<section class="browse-lead">
			<p>DISCOVER</p>
			<h2>Find your next favorite.</h2>
		</section>
		<section class="block edge-block">
			<div class="section-head"><h3>Top Charts</h3></div>
			{#if chartsLoading}
				<div class="carousel"><Skeleton count={3} width={220} height={170} /></div>
			{:else}
				<div class="carousel">
					{#each COUNTRIES as c}
						{@const top = charts[c.id]?.[0]}
						{@const top3 = charts[c.id]?.slice(0, 3) ?? []}
						<button class="chartcard" onclick={() => goChart(c.id, c.label)}>
							<ArtImg ref={top?.art} />
							<div class="cscrim">
								<span class="ctitle">{c.label} Top {charts[c.id]?.length ?? 0}</span>
								<div class="thumbs">
									{#each top3 as t}
										<ArtImg ref={t.art} />
									{/each}
								</div>
							</div>
						</button>
					{/each}
				</div>
			{/if}
		</section>

		<section class="block stations-block">
			<h3>Stations</h3>
			<div class="mosaic">
				{#each GENRES as g}
					<button
						class="tile"
						style="background:linear-gradient(135deg, {GENRE_COLORS[g]}, {GENRE_COLORS[g]}cc)"
						onclick={() => playStation(g)}
						disabled={stationBusy === g}
					>
						<span class="gname">{g}</span>
						{#if stationBusy === g}<span class="eq">···</span>{/if}
					</button>
				{/each}
			</div>
		</section>
	{/if}
{:else}
	{#if !online}
		<p class="offline-note"><Icon name="offline" size={15} /> You’re offline — showing matches from your library.</p>
	{/if}
	{#if nothingFound && !remoteSearching}
		<p class="empty">No results for “{trimmed}”.</p>
	{/if}

	{#if remoteSearching && remoteSongs.length === 0}
		<div class="block"><Skeleton count={4} width={0} height={48} /></div>
	{/if}

	{#if topResult}
		<section class="block top-result-block">
			<h3>Top Result</h3>
			<button class="topcard" class:playing={$current?.id === topResult.id} onclick={() => play(topResult, [topResult])}>
				<ArtImg ref={topResult.art} size="92px" />
				<div class="topmeta">
					<span class="ttitle">{topResult.track}</span>
					<span class="tartist">{topResult.artist}</span>
					<span class="badge">Song</span>
				</div>
				<span class="topplay"><Icon name="play" size={14} /></span>
			</button>
		</section>
	{/if}

	{#if remoteArtists.length}
		<section class="block">
			<h3>Artists</h3>
			<div class="rail edge-scroll">
				{#each remoteArtists as a (a.name)}
					<ArtistCircle name={a.name} art={a.art} browseId={a.browse_id} />
				{/each}
			</div>
		</section>
	{/if}

	{#if resultSongs.length}
		<section class="block">
			<h3>Songs</h3>
			<div class="song-surface"><ul class="songlist">
				{#each resultSongs as t, i (t.id)}
					<TrackRow track={t} context={remoteSongs} index={i + 1} />
				{/each}
			</ul></div>
			{#if remoteSongs.length > 9}
				<button class="seeall" onclick={() => goCollection({ title: `Songs · “${trimmed}”`, tracks: remoteSongs })}>See All</button>
			{/if}
		</section>
	{/if}

	{#if remoteAlbums.length}
		<section class="block">
			<h3>Albums</h3>
			<div class="rail edge-scroll">
				{#each remoteAlbums as a (a.browse_id || a.title)}
					<AlbumCard album={a} />
				{/each}
			</div>
		</section>
	{/if}

	{#if matchedSongs.length || matchedArtists.length || matchedAlbums.length}
		<section class="block">
			<h3>Your Library</h3>
			{#if matchedArtists.length}
				<div class="rail edge-scroll">
					{#each matchedArtists as a (a.name)}
						<ArtistCircle name={a.name} art={a.art} />
					{/each}
				</div>
			{/if}
			{#if matchedAlbums.length}
				<div class="rail edge-scroll">
					{#each matchedAlbums as a (a.title)}
						<AlbumCard album={a} />
					{/each}
				</div>
			{/if}
			{#if matchedSongs.length}
				<ul class="songlist">
					{#each matchedSongs as t, i (t.id)}
						<TrackRow track={t} context={matchedSongs} index={i} />
					{/each}
				</ul>
			{/if}
		</section>
	{/if}
{/if}
</div>

<style>
	.block {
		margin-bottom: 1.4rem;
	}
	h3 {
		font-size: 0.95rem;
		margin: 0 0 0.6rem;
	}
	.empty {
		color: #888;
		padding: 1.5rem 0;
		text-align: center;
	}
	.offline-note {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		margin: 0 0 1.1rem;
		color: var(--muted);
		font-size: 0.88rem;
	}
	.carousel,
	.rail {
		display: flex;
		gap: 0.8rem;
		overflow-x: auto;
		padding-bottom: 0.3rem;
	}
	.chartcard {
		position: relative;
		width: 220px;
		height: 170px;
		flex-shrink: 0;
		border: none;
		border-radius: 12px;
		overflow: hidden;
		cursor: pointer;
		padding: 0;
		background: #eee;
	}
	.chartcard :global(img),
	.chartcard :global(.placeholder) {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.cscrim {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		justify-content: flex-end;
		gap: 0.4rem;
		padding: 0.6rem;
		background: linear-gradient(transparent 40%, rgba(0, 0, 0, 0.6));
		color: #fff;
		text-align: left;
	}
	.ctitle {
		font-size: 0.84rem;
		font-weight: 600;
	}
	.thumbs {
		display: flex;
		gap: 0.25rem;
	}
	.thumbs :global(img),
	.thumbs :global(.placeholder) {
		width: 30px;
		height: 30px;
		border-radius: 4px;
	}
	.mosaic {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 0.6rem;
	}
	.tile {
		position: relative;
		height: 90px;
		border: none;
		border-radius: 10px;
		cursor: pointer;
		color: #fff;
		text-align: left;
		padding: 0.7rem;
		font: inherit;
		overflow: hidden;
	}
	.gname {
		font-size: 0.9rem;
		font-weight: 700;
		text-transform: capitalize;
		text-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
	}
	.eq {
		position: absolute;
		right: 0.6rem;
		bottom: 0.5rem;
		font-size: 0.8rem;
	}
	.topcard {
		display: flex;
		align-items: center;
		gap: 0.8rem;
		border: 1px solid #eee;
		background: #fff;
		border-radius: 10px;
		padding: 0.6rem 0.8rem;
		cursor: pointer;
		width: 100%;
		text-align: left;
		font: inherit;
	}
	.topcard.playing {
		border-color: #2a6df6;
		background: #eef4ff;
	}
	.topcard :global(img),
	.topcard :global(.placeholder) {
		border-radius: 8px;
		flex-shrink: 0;
	}
	.topmeta {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.ttitle {
		font-size: 0.95rem;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.tartist {
		font-size: 0.8rem;
		color: #666;
	}
	.badge {
		font-size: 0.66rem;
		color: #888;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
	.topplay {
		color: #2a6df6;
		font-size: 1rem;
	}
	.songlist {
		list-style: none;
		padding: 0;
		margin: 0;
	}
	.seeall {
		border: none;
		background: transparent;
		color: #2a6df6;
		font-size: 0.8rem;
		cursor: pointer;
		padding: 0.3rem 0;
	}

	/* Search uses the page as a canvas: labels retain a comfortable inset while
	 * artwork rails travel all the way to the desktop window edge. */
	.search-page { min-width: 0; padding-bottom: 2rem; }
	.search-page .block { margin-bottom: clamp(32px, 4vw, 54px); }
	.search-page h3 { margin-bottom: .85rem; font-size: 1.1rem; font-weight: 720; letter-spacing: -.035em; }
	.browse-lead { max-width: 1480px; margin: 0 auto clamp(30px, 4vw, 54px); }
	.browse-lead p { margin: 0 0 .35rem; color: var(--accent-deep); font-size: .68rem; font-weight: 800; letter-spacing: .13em; }
	.browse-lead h2 { margin: 0; font-size: clamp(2rem, 3.2vw, 3.4rem); font-weight: 760; letter-spacing: -.06em; line-height: .98; }
	.edge-block { width: 100vw; margin-inline: calc((100% - 100vw) / 2); }
	.edge-block .section-head { padding-inline: clamp(20px, 5vw, 76px); }
	.edge-block .carousel { padding-inline: clamp(20px, 5vw, 76px) 8px; }
	.edge-scroll { width: 100vw; margin-inline: calc((100% - 100vw) / 2); padding-inline: clamp(20px, 5vw, 76px) 8px; scroll-padding-inline: clamp(20px, 5vw, 76px); }
	.search-page .carousel, .search-page .rail { gap: 14px; }
	.search-page .chartcard { border-radius: 18px; background: #e4e1dd; box-shadow: 0 8px 20px rgba(43,36,32,.12); transition: transform .2s ease, box-shadow .2s ease; }
	.search-page .chartcard:hover { transform: translateY(-2px); box-shadow: 0 14px 28px rgba(43,36,32,.17); }
	.search-page .cscrim { padding: .85rem; }
	.search-page .ctitle { font-size: .88rem; font-weight: 720; }
	.search-page .thumbs :global(img), .search-page .thumbs :global(.placeholder) { border-radius: 6px; }
	.stations-block { max-width: 1480px; margin-inline: auto; }
	.search-page .mosaic { gap: 12px; }
	.search-page .tile { height: 108px; border-radius: 16px; padding: .9rem; box-shadow: 0 7px 16px rgba(48,41,37,.10); transition: transform .18s ease, filter .18s ease; }
	.search-page .tile:hover { transform: translateY(-2px); filter: saturate(1.08) brightness(1.03); }
	.search-page .gname { font-size: .95rem; font-weight: 760; }
	.top-result-block { max-width: 760px; }
	.search-page .topcard { border-color: var(--line); background: rgba(255,255,255,.76); border-radius: 20px; padding: .8rem; box-shadow: var(--shadow-soft); transition: transform .18s ease, box-shadow .18s ease; }
	.search-page .topcard:hover { transform: translateY(-1px); box-shadow: 0 16px 38px rgba(47,42,37,.13); }
	.search-page .topcard :global(img), .search-page .topcard :global(.placeholder) { border-radius: 13px; }
	.search-page .ttitle { font-size: 1rem; font-weight: 720; }
	.search-page .tartist { color: var(--muted); }
	.search-page .badge { color: var(--quiet); }
	.search-page .topplay { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 50%; background: var(--accent); color: #fff; }
	.song-surface { overflow: hidden; border: 1px solid var(--line); border-radius: 18px; background: rgba(255,255,255,.6); box-shadow: 0 8px 24px rgba(47,42,37,.05); }
	.search-page .seeall { color: var(--accent-deep); }
	@media (max-width: 820px) {
		.edge-scroll { padding-inline: 18px; }
		.search-page .mosaic { grid-template-columns: repeat(2, minmax(0, 1fr)); }
	}
</style>
