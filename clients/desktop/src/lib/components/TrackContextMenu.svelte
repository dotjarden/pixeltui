<script lang="ts">
	// A single viewport-level menu mirrors iOS's universal SongMenu. It is
	// mounted above every route, so horizontal rails cannot clip it.
	import { get } from 'svelte/store';
	import { onMount, onDestroy } from 'svelte';
	import { addToQueue, startStation } from '$lib/stores/player';
	import { likedIds, playlists, toggleLike, addToPlaylist } from '$lib/stores/library';
	import { downloadTrack, cancelDownload, removeDownload, downloadStateMap } from '$lib/stores/downloads';
	import { joined as partyJoined, enqueue as partyEnqueue } from '$lib/stores/party';
	import { goArtist, goAlbum, goTrackInfo } from '$lib/nav';
	import { trackContextMenu, closeTrackContextMenu } from '$lib/stores/contextMenu';
	import ArtImg from './ArtImg.svelte';
	import Icon from './Icon.svelte';

	let showPlaylists = $state(false);
	let menuEl = $state<HTMLElement>();
	let viewport = $state({ width: 1280, height: 800 });
	const left = $derived(Math.max(8, Math.min(($trackContextMenu?.x ?? 8), viewport.width - 270)));
	const top = $derived(Math.max(8, Math.min(($trackContextMenu?.y ?? 8), viewport.height - 470)));

	function close() { showPlaylists = false; closeTrackContextMenu(); }
	function item() { return get(trackContextMenu); }
	function doAddQueue() { const m = item(); if (m) addToQueue(m.track); close(); }
	function doAddParty() { const m = item(); if (m) void partyEnqueue([m.track]); close(); }
	function doStation() { const m = item(); if (m) void startStation(m.track); close(); }
	function doLike() { const m = item(); if (m) void toggleLike(m.track); close(); }
	function doAddToPlaylist(name: string) { const m = item(); if (m) void addToPlaylist(name, m.track); close(); }
	function doArtist() { const m = item(); if (m) goArtist(m.track.artist); close(); }
	function doAlbum() { const m = item(); if (m?.track.album) goAlbum({ title: m.track.album, artist: m.track.artist, browse_id: '', art: m.track.art }); close(); }
	function doInfo() { const m = item(); if (m) goTrackInfo(m.track); close(); }
	function doDownload() { const m = item(); if (m) void downloadTrack(m.track); close(); }
	function doCancelDownload() { const m = item(); if (m) void cancelDownload(m.track.id); close(); }
	function doRemoveDownload() { const m = item(); if (m) void removeDownload(m.track.id); close(); }
	function doRemove() { const m = item(); m?.onRemove?.(); close(); }
	async function doShare(url?: string) { close(); if (url) await navigator.clipboard.writeText(url).catch(() => {}); }

	function updateViewport() { viewport = { width: window.innerWidth, height: window.innerHeight }; }
	function onWindowClick(event: MouseEvent) { if (menuEl && event.target instanceof Node && menuEl.contains(event.target)) return; close(); }
	function onKey(event: KeyboardEvent) { if (event.key === 'Escape') close(); }
	onMount(() => { updateViewport(); window.addEventListener('resize', updateViewport); window.addEventListener('scroll', close, true); });
	onDestroy(() => { window.removeEventListener('resize', updateViewport); window.removeEventListener('scroll', close, true); });
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKey} />
{#if $trackContextMenu}
	{@const m = $trackContextMenu}
	{@const track = m.track}
	{@const caps = track.capabilities}
	{@const hasArtist = !!track.artist && track.artist !== 'Unknown Artist'}
	{@const hasAlbum = !!track.album && track.album !== 'Singles'}
	{@const dlState = $downloadStateMap.get(track.id)}
	{@const shareUrl = caps?.share_url ?? (track.id.startsWith('yt:') ? `https://music.youtube.com/watch?v=${track.id.slice(3)}` : undefined)}
	<div class="track-context" bind:this={menuEl} style:left={`${left}px`} style:top={`${top}px`} aria-label={`Actions for ${track.track}`}>
		<div class="menu-preview"><ArtImg ref={track.art} alt="" /><span><strong>{track.track}</strong><small>{track.artist}</small></span></div>
		<div class="menu-body">
			<button onclick={doAddQueue}><Icon name="queue" size={16} /> Add to Queue</button>
			{#if $partyJoined}<button onclick={doAddParty}><Icon name="users" size={16} /> Add to Party</button>{/if}
			{#if caps?.start_station}<button onclick={doStation}><Icon name="autoplay" size={16} /> Start Station</button>{/if}
			<div class="sep"></div>
			<button onclick={doLike}><Icon name="heart" size={16} /> {$likedIds.has(track.id) ? 'Remove from Liked' : 'Add to Liked'}</button>
			<div class="submenu">
				<button onclick={() => (showPlaylists = !showPlaylists)}><span><Icon name="library" size={16} /> Add to Playlist</span><Icon name="next" size={14} /></button>
				{#if showPlaylists}<div class="submenu-list">{#if $playlists.length === 0}<span>No playlists yet</span>{:else}{#each $playlists as name}<button onclick={() => doAddToPlaylist(name)}>{name}</button>{/each}{/if}</div>{/if}
			</div>
			{#if caps?.download || hasArtist || hasAlbum}<div class="sep"></div>{/if}
			{#if caps?.download}
				{#if dlState === 'downloading'}<button onclick={doCancelDownload}><Icon name="close" size={16} /> Cancel Download</button>
				{:else if dlState === 'downloaded'}<button onclick={doRemoveDownload}><Icon name="download" size={16} /> Remove Download</button>
				{:else}<button onclick={doDownload}><Icon name="download" size={16} /> Download</button>{/if}
			{/if}
			{#if hasArtist && caps?.go_to_artist}<button onclick={doArtist}><Icon name="users" size={16} /> Go to Artist</button>{/if}
			{#if hasAlbum && caps?.go_to_album}<button onclick={doAlbum}><Icon name="library" size={16} /> Go to Album</button>{/if}
			<button onclick={doInfo}><Icon name="more" size={16} /> Credits &amp; Info</button>
			{#if m.onRemove}<div class="sep"></div><button class="danger" onclick={doRemove}><Icon name="close" size={16} /> Remove from Playlist</button>{/if}
			{#if shareUrl}<div class="sep"></div><button onclick={() => doShare(shareUrl)}><Icon name="next" size={16} /> Share</button>{/if}
		</div>
	</div>
{/if}

<style>
	.track-context { position: fixed; z-index: 500; width: 262px; overflow: hidden; border: 1px solid rgba(29,27,25,.14); border-radius: 16px; background: rgba(251,250,248,.96); box-shadow: 0 20px 52px rgba(31,28,25,.24), 0 3px 10px rgba(31,28,25,.10); backdrop-filter: blur(24px) saturate(1.15); animation: menu-in .14s ease-out; }
	@keyframes menu-in { from { opacity: 0; transform: translateY(5px) scale(.98); } to { opacity: 1; transform: none; } }
	.menu-preview { display: flex; align-items: center; gap: .7rem; padding: .75rem; background: rgba(255,255,255,.52); }
	.menu-preview :global(img), .menu-preview :global(.placeholder) { width: 42px; height: 42px; flex: 0 0 auto; border-radius: 9px; object-fit: cover; }
	.menu-preview span { display: grid; min-width: 0; gap: 2px; }
	.menu-preview strong, .menu-preview small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.menu-preview strong { font-size: .81rem; }
	.menu-preview small { color: var(--muted); font-size: .72rem; }
	.menu-body { display: grid; gap: 1px; padding: .35rem; }
	.menu-body button { display: flex; align-items: center; gap: .6rem; width: 100%; min-height: 34px; padding: .35rem .5rem; border: 0; border-radius: 9px; background: transparent; color: var(--text); font: inherit; font-size: .79rem; text-align: left; cursor: pointer; }
	.menu-body button:hover { background: var(--accent-soft); color: var(--accent-deep); }
	.menu-body button.danger { color: #b23947; }
	.submenu > button { justify-content: space-between; }
	.submenu > button span { display: inline-flex; align-items: center; gap: .6rem; }
	.submenu-list { display: grid; max-height: 160px; margin: 2px 0 2px 1.2rem; overflow: auto; padding-left: .35rem; border-left: 1px solid var(--line); }
	.submenu-list span { padding: .4rem .5rem; color: var(--muted); font-size: .74rem; }
	.sep { height: 1px; margin: .25rem .4rem; background: var(--line); }
	@media (prefers-reduced-motion: reduce) { .track-context { animation: none; } }
</style>
