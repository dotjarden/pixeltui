<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Home from '$lib/components/Home.svelte';
	import Library from '$lib/components/Library.svelte';
	import Search from '$lib/components/Search.svelte';
	import Party from '$lib/components/Party.svelte';
	import Settings from '$lib/components/Settings.svelte';

	type Tab = 'home' | 'library' | 'search' | 'party' | 'settings';
	const tab = $derived((page.url.searchParams.get('view') as Tab) || 'home');
	onMount(() => {
		const shortcut = async (event: KeyboardEvent) => {
			if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
				event.preventDefault(); await goto('/?view=search'); await tick();
				(document.querySelector('.top-search input') as HTMLInputElement | null)?.focus();
			}
		};
		window.addEventListener('keydown', shortcut); return () => window.removeEventListener('keydown', shortcut);
	});
</script>

{#if tab === 'home'}<Home />
{:else if tab === 'library'}<Library />
{:else if tab === 'search'}<Search />
{:else if tab === 'party'}<Party />
{:else}<Settings />{/if}
