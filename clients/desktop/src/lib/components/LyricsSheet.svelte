<script lang="ts">
	import { cachedLyrics, current, duration, position, isPlaying, prefetchLyrics, seek, togglePlayPause } from '$lib/stores/player';
	import type { Lyrics } from '$lib/api/types';
	import Icon from './Icon.svelte';
	import ArtImg from './ArtImg.svelte';

	let { open, onClose }: { open: boolean; onClose: () => void } = $props();
	let lyrics = $state<Lyrics | null>(null);
	let loading = $state(false);
	let loadSeq = 0;
	let lineEls: HTMLElement[] = [];
	let lyricsBody = $state<HTMLElement>();

	$effect(() => {
		const track = $current;
		if (!track || !open) {
			lyrics = null;
			loading = false;
			return;
		}
		const seq = ++loadSeq;
		const cached = cachedLyrics(track);
		if (cached !== undefined) {
			lyrics = cached;
			loading = false;
			return;
		}
		loading = true;
		lyrics = null;
		void prefetchLyrics(track).then((result) => {
			if (seq !== loadSeq) return;
			lyrics = result;
			loading = false;
		});
	});

	const syncedLines = $derived.by(() => {
		const lines = lyrics?.synced ?? [];
		if (!lines.length) return [];
		const maximum = Math.max(...lines.map((line) => line.t));
		const scale = maximum > Math.max(1000, ($duration || 1) * 10) ? 0.001 : 1;
		return lines.map((line) => ({ ...line, t: line.t * scale }));
	});
	const hasSyncedText = $derived(syncedLines.some((line) => line.text.trim().length > 0));
	const activeIndex = $derived.by(() => {
		if (!hasSyncedText) return -1;
		let index = -1;
		for (let i = 0; i < syncedLines.length; i++) {
			if (syncedLines[i].t <= $position) index = i;
			else break;
		}
		return index;
	});
	const plainLines = $derived(lyrics?.plain ? lyrics.plain.split('\n') : []);

	$effect(() => {
		const line = activeIndex >= 0 ? lineEls[activeIndex] : undefined;
		const body = lyricsBody;
		if (!line || !body) return;
		body.scrollTo({ top: Math.max(0, line.offsetTop - body.clientHeight / 2 + line.offsetHeight / 2), behavior: $isPlaying ? 'smooth' : 'auto' });
	});

	function onKeydown(event: KeyboardEvent) {
		if (open && event.key === 'Escape') onClose();
	}
	function stamp(seconds: number): string {
		return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
	}
</script>

<svelte:window onkeydown={onKeydown} />
{#if open}
	<aside class="lyrics-popover" aria-label="Lyrics">
		<header class="lyrics-head">
			{#if $current}
				<ArtImg ref={$current.art} alt="" eager />
				<div class="meta"><span class="track">{$current.track}</span><span class="artist">{$current.artist}</span></div>
			{:else}
				<div class="meta"><span class="track">Lyrics</span></div>
			{/if}
			<div class="controls">
				{#if $current}<button onclick={togglePlayPause} title={$isPlaying ? 'Pause' : 'Play'} aria-label={$isPlaying ? 'Pause' : 'Play'}><Icon name={$isPlaying ? 'pause' : 'play'} size={17} /></button>{/if}
				<button onclick={onClose} title="Close lyrics" aria-label="Close lyrics"><Icon name="close" size={17} /></button>
			</div>
		</header>

		<div class="lyrics-body" bind:this={lyricsBody}>
			{#if !$current}
				<p class="empty">Play a track to view lyrics.</p>
			{:else if loading}
				<p class="empty">Loading lyrics…</p>
			{:else if lyrics && hasSyncedText}
				<div class="synced">
					{#each syncedLines as line, i (line.t + '-' + i)}
						<button class="line" class:active={i === activeIndex} class:passed={i < activeIndex} bind:this={lineEls[i]} onclick={() => seek(line.t)} title={`Jump to ${stamp(line.t)}`}>{line.text}</button>
					{/each}
				</div>
			{:else if plainLines.length}
				<div class="plain">{#each plainLines as line}<p>{line}</p>{/each}</div>
			{:else}
				<p class="empty">Lyrics are not available for this track.</p>
			{/if}
		</div>
	</aside>
{/if}

<style>
	.lyrics-popover {
		position: fixed;
		right: 24px;
		bottom: 102px;
		z-index: 36;
		display: flex;
		width: min(430px, calc(100vw - 32px));
		max-height: min(66vh, 590px);
		flex-direction: column;
		overflow: hidden;
		border: 1px solid rgba(255,255,255,.12);
		border-radius: 14px;
		background: #242329;
		color: #f8f7f5;
		box-shadow: 0 20px 50px rgba(22,20,25,.32);
		animation: lyrics-in .16s ease-out;
	}
	@keyframes lyrics-in { from { opacity: 0; transform: translateY(8px) scale(.985); } to { opacity: 1; transform: translateY(0) scale(1); } }
	.lyrics-head { display: flex; align-items: center; gap: .7rem; min-height: 64px; padding: .7rem .85rem; border-bottom: 1px solid rgba(255,255,255,.1); }
	.lyrics-head :global(img), .lyrics-head :global(.placeholder) { width: 42px; height: 42px; flex: 0 0 auto; border-radius: 8px; object-fit: cover; }
	.meta { display: grid; min-width: 0; flex: 1; gap: 2px; }
	.track, .artist { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.track { font-size: .86rem; font-weight: 700; }
	.artist { color: rgba(255,255,255,.62); font-size: .75rem; }
	.controls { display: flex; gap: .35rem; }
	.controls button { display: grid; width: 32px; height: 32px; place-items: center; padding: 0; border: 0; border-radius: 8px; background: rgba(255,255,255,.1); color: #fff; cursor: pointer; }
	.controls button:hover { background: rgba(255,255,255,.18); }
	.lyrics-body { min-height: 200px; overflow-y: auto; padding: 1.25rem 1.35rem 1.8rem; scrollbar-color: rgba(255,255,255,.2) transparent; }
	.synced { display: grid; gap: .8rem; padding-block: 20%; text-align: center; }
	.line { width: 100%; margin: 0; padding: 0; border: 0; background: transparent; color: rgba(255,255,255,.38); font: inherit; font-size: 1.16rem; font-weight: 650; line-height: 1.35; text-align: center; cursor: pointer; transition: color .2s ease, transform .2s ease; }
	.line.passed { color: rgba(255,255,255,.6); }
	.line.active { color: #fff; transform: scale(1.035); }
	.line:hover { color: rgba(255,255,255,.84); }
	.plain { color: rgba(255,255,255,.83); font-size: .95rem; line-height: 1.65; text-align: center; white-space: pre-wrap; }
	.plain p { margin: .25rem 0; }
	.empty { margin: 0; padding: 2.5rem .5rem; color: rgba(255,255,255,.58); text-align: center; font-size: .88rem; }
	@media (max-width: 820px) { .lyrics-popover { right: 10px; bottom: 98px; } }
	@media (prefers-reduced-motion: reduce) { .lyrics-popover, .line { animation: none; transition: none; } }
</style>
