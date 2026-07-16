<script lang="ts">
	// Library tab — single scrolling page (iOS LibraryView is not segmented):
	// liked banner, category rows, subsonic section, your-artists rail,
	// recently-added grid. Category rows navigate to list/detail routes or to
	// the generic collection page with inline tracks.
	import { goto } from '$app/navigation';
	import Rail from './Rail.svelte';
	import AlbumCard from './AlbumCard.svelte';
	import ArtistCircle from './ArtistCircle.svelte';
	import {
		liked,
		playlists,
		sources,
		recents
	} from '$lib/stores/library';
	import { artists, albums, recentAlbums } from '$lib/stores/catalog';
	import { playFromList } from '$lib/stores/player';
	import { goCollection, goPlaylist } from '$lib/nav';
	import { api } from '$lib/api/client';
	import type { SubsonicPlaylist, Track } from '$lib/api/types';
	import Icon from './Icon.svelte';

	let subsonicPlaylists = $state<SubsonicPlaylist[]>([]);
	let localSongs = $state<Track[]>([]);
	let sourceLoadSeq = 0;

	const hasSubsonic = $derived($sources.includes('subsonic'));
	const hasLocal = $derived($sources.includes('local'));

	// Sources arrive asynchronously during the shared library refresh. React to
	// that state instead of reading it once at mount (which left cold-boot
	// Library pages empty). Guard responses so a source change cannot paint stale
	// data from an earlier request.
	$effect(() => {
		const sourceKey = $sources.join(',');
		if (!sourceKey) return;
		const seq = ++sourceLoadSeq;
		if (!hasSubsonic) subsonicPlaylists = [];
		if (!hasLocal) localSongs = [];
		if (hasSubsonic) {
			void api.subsonicPlaylists()
				.then((p) => { if (seq === sourceLoadSeq) subsonicPlaylists = p; })
				.catch(() => {});
		}
		if (hasLocal) {
			void api.localTracks()
				.then((t) => { if (seq === sourceLoadSeq) localSongs = t; })
				.catch(() => {});
		}
	});

	function playLiked() {
		if ($liked.length) playFromList($liked, 0);
	}

	function artistTracks(): Track[] {
		return $artists.flatMap((a) => a.songs);
	}

	const categories = $derived.by(() => {
		const rows: { label: string; icon: string; sub?: string; onclick: () => void; disabled?: boolean }[] = [
			{ label: 'Playlists', icon: 'queue', sub: `${$playlists.length}`, onclick: () => goto('/playlists') },
			{ label: 'Artists', icon: 'users', sub: `${$artists.length}`, onclick: () => goto('/artists') },
			{ label: 'Albums', icon: 'library', sub: `${$albums.length}`, onclick: () => goto('/albums') },
			{ label: 'Songs', icon: 'lyrics', sub: `${$liked.length}`, onclick: () => goCollection({ title: 'All Songs', tracks: $liked }) },
			{ label: 'Recently Played', icon: 'clock', sub: `${$recents.length}`, onclick: () => goCollection({ title: 'Recently Played', tracks: $recents as Track[] }) },
			{ label: 'Downloaded', icon: 'download', onclick: () => goto('/downloads') },
			{ label: 'History', icon: 'clock', onclick: () => goto('/history') },
			{ label: 'Stats', icon: 'chart', onclick: () => goto('/stats') }
		];
		return rows;
	});
</script>

<div class="library-page">
<section class="liked-banner">
	<div class="collage"><Icon name="heart" size={31}/></div>
	<div class="meta">
		<span class="title">Liked Songs</span>
		<span class="sub">{$liked.length} songs</span>
	</div>
	<button class="play" onclick={playLiked} disabled={$liked.length === 0}><Icon name="play" size={14}/> Play</button>
	<button class="open" onclick={() => goCollection({ title: 'Liked Songs', symbol: 'heart', tracks: $liked })}>Open</button>
</section>

<section class="categories" aria-label="Library sections">
	{#each categories as c}
		<button class="cat" onclick={c.onclick} disabled={c.disabled}>
			<span class="cat-icon"><Icon name={c.icon} size={18}/></span>
			<span class="label">{c.label}</span>
			{#if c.sub !== undefined}<span class="sub">{c.sub}</span>{/if}
			<span class="chev"><Icon name="next" size={14}/></span>
		</button>
	{/each}
	{#if hasLocal}
		<button class="cat" onclick={() => goCollection({ title: 'Server Files', tracks: localSongs })}>
			<span class="cat-icon"><Icon name="library" size={18}/></span>
			<span class="label">Server Files</span>
			<span class="sub">{localSongs.length}</span>
			<span class="chev"><Icon name="next" size={14}/></span>
		</button>
	{/if}
</section>

{#if hasSubsonic && subsonicPlaylists.length}
	<Rail edge title="Subsonic">
		{#each subsonicPlaylists as p (p.ID)}
			<button class="subrow" onclick={() => goPlaylist(p.Name)}>
				<span class="subname">{p.Name}</span>
				<span class="subcount">{p.SongCount} songs</span>
			</button>
		{/each}
	</Rail>
{/if}

{#if $artists.length}
	<Rail edge title="Your Artists">
		{#each $artists.slice(0, 12) as a}
			<ArtistCircle name={a.name} art={a.art} subtitle={`${a.songs.length}`} />
		{/each}
	</Rail>
{/if}

{#if $recentAlbums.length}
	<Rail edge title="Recently Added">
		{#each $recentAlbums.slice(0, 10) as a}
			<AlbumCard album={a} />
		{/each}
	</Rail>
{/if}
</div>

<style>
	.liked-banner {
		display: flex;
		align-items: center;
		gap: 1rem;
		background: linear-gradient(135deg, #7c5cff, #b06bf5);
		color: #fff;
		border-radius: 14px;
		padding: 1.2rem;
		margin-bottom: 1.2rem;
	}
	.collage {
		width: 90px;
		height: 90px;
		border-radius: 10px;
		background: rgba(255, 255, 255, 0.18);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 2.4rem;
		flex-shrink: 0;
	}
	.meta {
		display: flex;
		flex-direction: column;
		flex: 1;
	}
	.title {
		font-size: 1.2rem;
		font-weight: 700;
	}
	.sub {
		font-size: 0.84rem;
		opacity: 0.85;
	}
	.play,
	.open {
		border: none;
		border-radius: 999px;
		padding: 0.5rem 1rem;
		font: inherit;
		font-size: 0.82rem;
		cursor: pointer;
		display: inline-flex;
		align-items: center;
		gap: 7px;
	}
	.play {
		background: #fff;
		color: #5a3fd8;
		font-weight: 600;
	}
	.play:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.open {
		background: rgba(255, 255, 255, 0.2);
		color: #fff;
	}
	.categories {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.4rem;
		margin-bottom: 1.2rem;
	}
	.cat {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		border: 1px solid #eee;
		background: #fff;
		border-radius: 8px;
		padding: 0.6rem 0.8rem;
		cursor: pointer;
		text-align: left;
		font: inherit;
	}
	.cat-icon {
		display: grid;
		place-items: center;
		width: 24px;
		color: var(--accent-deep);
	}
	.cat:hover {
		background: #f6f8fc;
	}
	.cat:disabled {
		opacity: 0.45;
		cursor: default;
	}
	.cat .label {
		flex: 1;
		font-size: 0.9rem;
	}
	.cat .sub {
		font-size: 0.76rem;
		color: #888;
	}
	.chev {
		color: #bbb;
	}
	.subrow {
		display: flex;
		flex-direction: column;
		border: 1px solid #eee;
		background: #fff;
		border-radius: 8px;
		padding: 0.6rem 0.7rem;
		cursor: pointer;
		text-align: left;
		min-width: 160px;
		font: inherit;
	}
	.subrow:hover {
		background: #f6f8fc;
	}
	.subname {
		font-size: 0.86rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.subcount {
		font-size: 0.72rem;
		color: #888;
	}

	/* One artwork-led anchor, followed by an orderly library index. The cards
	 * are deliberately calm so the music, not the chrome, carries the page. */
	.library-page { padding-bottom: 2rem; }
	.library-page .liked-banner {
		min-height: 150px;
		gap: 1.15rem;
		padding: clamp(20px, 3vw, 34px);
		margin-bottom: clamp(22px, 3vw, 36px);
		border: 1px solid rgba(255,255,255,.26);
		border-radius: 24px !important;
		background: linear-gradient(135deg, #ea5b69 0%, #d44f60 54%, #b23f52 100%) !important;
		box-shadow: 0 18px 44px rgba(177,52,70,.23) !important;
	}
	.library-page .collage {
		width: 94px;
		height: 94px;
		border-radius: 18px;
		background: rgba(255,255,255,.17);
		box-shadow: inset 0 1px 0 rgba(255,255,255,.2), 0 9px 20px rgba(102,21,37,.16);
	}
	.library-page .title { font-size: 1.35rem; font-weight: 760; letter-spacing: -.045em; }
	.library-page .play, .library-page .open { font-size: .82rem; }
	.library-page .play { color: #bc4051; font-weight: 720; }
	.library-page .categories { gap: 10px; margin-bottom: clamp(30px, 4vw, 52px); }
	.library-page .cat {
		gap: .65rem;
		min-height: 58px;
		padding: .7rem .85rem;
		border: 1px solid var(--line) !important;
		border-radius: 16px !important;
		background: rgba(255,255,255,.62) !important;
		transition: background .18s ease, box-shadow .18s ease, transform .18s ease;
	}
	.library-page .cat-icon { width: 28px; }
	.library-page .cat .label { font-weight: 650; }
	.library-page .cat:hover { background: var(--surface-strong) !important; box-shadow: 0 8px 18px rgba(47,42,37,.07); transform: translateY(-1px); }
	.library-page .subrow { border-color: var(--line) !important; border-radius: 14px !important; background: rgba(255,255,255,.62) !important; }
	.library-page .subrow:hover { background: var(--surface-strong) !important; }
	@media (max-width: 720px) {
		.library-page .liked-banner { align-items: flex-start; flex-wrap: wrap; min-height: 0; }
		.library-page .meta { min-width: calc(100% - 116px); }
		.library-page .categories { grid-template-columns: 1fr; }
	}
</style>
