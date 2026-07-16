<script lang="ts">
	import { goto } from '$app/navigation';
	import { tick } from 'svelte';
	import { page } from '$app/state';
	import { current } from '$lib/stores/player';
	import { searchQuery } from '$lib/stores/ui';
	import Icon from './Icon.svelte';

	let { children }: { children: import('svelte').Snippet } = $props();
	const path = $derived(page.url.pathname);
	const view = $derived(page.url.searchParams.get('view') || 'home');
	const query = $derived(page.url.searchParams.get('q') || '');
	const root = $derived(path === '/');
	let searchOpen = $state(false);
	let searchDraft = $state('');
	let lastView = $state('');
	$effect(() => {
		if (view === lastView) return;
		lastView = view;
		if (view === 'search') {
			searchOpen = true;
			const initialQuery = query || $searchQuery;
			searchDraft = initialQuery;
			searchQuery.set(initialQuery);
		} else {
			searchOpen = false;
			searchDraft = '';
			searchQuery.set('');
		}
	});
	const title = $derived(
		root ? (view === 'home' ? 'Home' : view === 'library' ? 'Library' : view === 'search' ? 'Search' : view === 'party' ? 'Listening party' : 'Settings') :
		path.startsWith('/artists') ? 'Artists' : path.startsWith('/albums') ? 'Albums' : path.startsWith('/playlists') ? 'Playlists' : path.startsWith('/artist') ? 'Artist' : path.startsWith('/album') ? 'Album' : path.startsWith('/playlist') ? 'Playlist' : path.startsWith('/trackinfo') ? 'Credits & info' : path.startsWith('/downloads') ? 'Downloads' : path.startsWith('/history') ? 'Listening history' : path.startsWith('/stats') ? 'Your listening' : 'Collection'
	);
	const nav = [
		{ id: 'home', label: 'Home', icon: 'home' },
		{ id: 'library', label: 'Library', icon: 'library' }
	];
	function open(id: string) { goto(`/?view=${id}`); }
	function back() {
		if (typeof history !== 'undefined' && history.length > 1) history.back();
		else goto('/?view=library');
	}
	async function openSearch() {
		searchOpen = true;
		searchQuery.set(searchDraft);
		await goto('/?view=search');
		await tick();
		(document.querySelector('.top-search input') as HTMLInputElement | null)?.focus();
	}
	function updateSearch(value: string) {
		searchDraft = value;
		searchQuery.set(value);
	}
</script>

<div class="app-frame">
	<header class="top-nav" aria-label="Primary navigation">
		{#if !root}
			<button class="back-nav" onclick={back} aria-label="Back"><Icon name="back" size={16}/><span>Back</span></button>
		{/if}
		<nav class="nav-links">
			{#each nav as item}
				<button class:active={root && view === item.id} onclick={() => open(item.id)} aria-label={item.label}><span class="nav-icon"><Icon name={item.icon} size={16}/></span><span class="nav-text">{item.label}</span></button>
			{/each}
		</nav>
		<div class="search-slot" class:open={searchOpen}>
			<button class="search-trigger" onclick={openSearch} aria-label="Search"><Icon name="search" size={16} /></button>
			<div class="top-search">
				<input bind:value={searchDraft} oninput={(event) => updateSearch(event.currentTarget.value)} placeholder="Search your music" aria-label="Search your music" />
				<button class="search-close" onclick={() => { searchOpen = false; searchDraft = ''; searchQuery.set(''); open('home'); }} aria-label="Close search"><Icon name="close" size={14} /></button>
			</div>
		</div>
		<button class:active={root && view === 'settings'} class="settings-link" onclick={() => open('settings')} aria-label="Settings"><Icon name="settings" size={16}/><span class="nav-text">Settings</span></button>
	</header>
	<main class="workspace" class:detail-workspace={!root}><header class="workspace-head" class:subpage={!root || view === 'home'}><div>{#if !root}<p class="eyebrow">Your music</p>{/if}<h1>{title}</h1></div>{#if $current}<div class="playing-note"><span></span> Playing now</div>{/if}</header><section class="page-content">{@render children()}</section></main>
</div>
