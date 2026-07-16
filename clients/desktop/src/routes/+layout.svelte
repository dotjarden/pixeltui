<script lang="ts">
	import '../app.css';
	// Global layout: boot the sidecar, open SSE + library wiring once ready, and
	// mount the persistent chrome (MiniPlayer + QueueSheet) once for every route.
	// First-run onboarding overlays everything until the host is provisioned and
	// a config.json is saved.
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { boot, status } from '$lib/server';
	import { events } from '$lib/sse/events';
	import { connectLibraryEvents, refresh, refreshRecents } from '$lib/stores/library';
	import { initDownloads } from '$lib/stores/downloads';
	import { restoreQueue } from '$lib/stores/player';
	import { initUpdater } from '$lib/updater';
	import { queueOpen, lyricsOpen } from '$lib/stores/ui';
	import { hasConfig, loadConfig, onboardingComplete } from '$lib/stores/settings';
	import {
		registerMediaShortcuts,
		unregisterMediaShortcuts,
		initMediaSession
	} from '$lib/media/integration';
	import MiniPlayer from '$lib/components/MiniPlayer.svelte';
	import QueueSheet from '$lib/components/QueueSheet.svelte';
	import LyricsSheet from '$lib/components/LyricsSheet.svelte';
	import TrackContextMenu from '$lib/components/TrackContextMenu.svelte';
	import Onboarding from '$lib/components/Onboarding.svelte';
	import DesktopChrome from '$lib/components/DesktopChrome.svelte';

	let { children } = $props();

	let showOnboarding = $state(false);
	let initialized = $state(false);
	let configChecked = $state(false);
	let cleanupMediaSession: (() => void) | null = null;

	function preventBrowserContextMenu(event: MouseEvent) {
		event.preventDefault();
	}

	onMount(() => {
		boot();
		let latestStatus = 'starting';

		function initializeIfReady() {
			if (latestStatus !== 'ready' || !configChecked || initialized || !($onboardingComplete || !showOnboarding)) return;
			initialized = true;
			events.start();
			connectLibraryEvents();
			void refresh().catch(() => {});
			void refreshRecents().catch(() => {});
			void loadConfig();
			void initDownloads();
			restoreQueue();
			void registerMediaShortcuts();
			cleanupMediaSession = initMediaSession();
			void initUpdater();
		}

		// First-run check: if there's no config.json, show the onboarding wizard.
		hasConfig()
			.then((exists) => {
				if (!exists) showOnboarding = true;
				configChecked = true;
				initializeIfReady();
			})
			.catch(() => { configChecked = true; initializeIfReady(); });

		// Keep the status subscription alive across sidecar restarts; gate init
		// on not being in the middle of onboarding.
		const unsub = status.subscribe((s) => {
			latestStatus = s;
			initializeIfReady();
		});

		// When onboarding finishes, the sidecar will restart; hide the wizard and
		// let the status subscription trigger init on the next ready event.
		const unsubOb = onboardingComplete.subscribe((done) => {
			if (done) {
				showOnboarding = false;
				initializeIfReady();
			}
		});

		return () => {
			unsub();
			unsubOb();
			events.stop();
			void unregisterMediaShortcuts();
			cleanupMediaSession?.();
		};
	});
</script>

<svelte:window oncontextmenu={preventBrowserContextMenu} />

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<div class="shell" class:dim={showOnboarding}><DesktopChrome>{@render children()}</DesktopChrome></div>

<MiniPlayer />
<QueueSheet open={$queueOpen} onClose={() => queueOpen.set(false)} />
<LyricsSheet open={$lyricsOpen} onClose={() => lyricsOpen.set(false)} />
<TrackContextMenu />

{#if showOnboarding}
	<Onboarding />
{/if}

<style>
	.shell {
		padding-bottom: 120px; /* clear the fixed MiniPlayer */
	}
	.dim {
		filter: blur(2px);
	}
</style>
