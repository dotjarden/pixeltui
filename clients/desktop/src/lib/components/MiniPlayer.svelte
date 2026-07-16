<script lang="ts">
	import {
		current,
		isPlaying,
		isPreparing,
		position,
		duration,
		volume,
		shuffleOn,
		repeatMode,
		autoplayOn,
		crossfadeSeconds,
		sleepTimerActive,
		togglePlayPause,
		next,
		previous,
		seek,
		toggleShuffle,
		cycleRepeat,
		setAutoplay,
		setCrossfade,
		setVolume,
		setSleepTimer,
		setSleepTimerEndOfTrack,
		cancelSleepTimer,
		CROSSFADE_PRESETS,
		upNext
	} from '$lib/stores/player';
	import { lyricsOpen, queueOpen } from '$lib/stores/ui';
	import { likedIds, toggleLike } from '$lib/stores/library';
	import { openTrackContextMenu } from '$lib/stores/contextMenu';
	import Icon from './Icon.svelte';
	import ArtImg from './ArtImg.svelte';

	let dragging = $state(false);
	let showSleep = $state(false);
	let showMore = $state(false);

	function fmt(s: number): string {
		if (!Number.isFinite(s) || s < 0) s = 0;
		const m = Math.floor(s / 60);
		const r = Math.floor(s % 60);
		return `${m}:${String(r).padStart(2, '0')}`;
	}

	function onInput(e: Event) {
		const v = Number((e.target as HTMLInputElement).value);
		seek(v);
	}

	function pickSleep(v: string) {
		showSleep = false;
		if (v === 'off') cancelSleepTimer();
		else if (v === 'eot') setSleepTimerEndOfTrack();
		else setSleepTimer(Number(v));
	}

	function closeMenus() {
		showMore = false;
		showSleep = false;
	}

	function onWindowClick(event: MouseEvent) {
		const target = event.target;
		if (target instanceof Element && target.closest('[data-mini-popover]')) return;
		closeMenus();
	}

	function toggleMore(event: MouseEvent) {
		event.stopPropagation();
		showMore = !showMore;
		showSleep = false;
	}

	function chooseCrossfade(seconds: number) {
		setCrossfade(seconds);
		showMore = false;
	}

	const repeatLabel = $derived(
		$repeatMode === 'off' ? 'repeat-off' : $repeatMode === 'all' ? 'repeat-all' : 'repeat-one'
	);
</script>

<svelte:window onclick={onWindowClick} />

{#if $current}
	<footer class="mini">
		<div class="player-grid">
			<div class="now" role="group" aria-label="Now playing" oncontextmenu={(event) => openTrackContextMenu(event, $current)}>
				<ArtImg ref={$current.art} alt="" eager />
				<div class="meta">
					<span class="title">{$current.track}</span>
					<span class="artist">{$current.artist}</span>
				</div>
				<button class="track-action" class:active={$likedIds.has($current.id)} onclick={() => void toggleLike($current)} title={$likedIds.has($current.id) ? 'Remove from liked songs' : 'Add to liked songs'} aria-label={$likedIds.has($current.id) ? 'Remove from liked songs' : 'Add to liked songs'}><Icon name="heart" size={16} /></button>
			</div>

			<div class="transport">
				<div class="controls">
				<button class="ghost" class:active={$shuffleOn} onclick={toggleShuffle} title="shuffle" aria-label="Shuffle"><Icon name="shuffle" /></button>
				<button onclick={previous} title="previous" aria-label="Previous"><Icon name="previous" /></button>
				<button class="play" onclick={togglePlayPause} title="play/pause" aria-label={$isPlaying ? 'Pause' : 'Play'}>
					{#if $isPreparing}…{:else if $isPlaying}<Icon name="pause" />{:else}<Icon name="play" />{/if}
				</button>
				<button onclick={next} title="next" aria-label="Next"><Icon name="next" /></button>
				<button class="ghost" class:active={$repeatMode !== 'off'} onclick={cycleRepeat} title={repeatLabel}>
					<Icon name="repeat" />
				</button>
				</div>
				<div class="progress">
					<span class="t">{fmt($position)}</span>
					<input
						type="range"
						min="0"
						max={$duration || 0}
						step="0.1"
						value={$position}
						oninput={onInput}
						onpointerdown={() => (dragging = true)}
						onpointerup={() => (dragging = false)}
						aria-label="Seek"
					/>
					<span class="t">{fmt($duration)}</span>
				</div>
			</div>
			<div class="tools">
				<label class="vol" title="volume">
					<span><Icon name="volume" size={16}/></span>
					<input type="range" min="0" max="1" step="0.01" value={$volume} oninput={(e) => setVolume(Number(e.currentTarget.value))} />
				</label>
				<button class="utility" class:active={$lyricsOpen} onclick={() => lyricsOpen.update((open) => !open)} title="Lyrics" aria-label="Lyrics"><Icon name="lyrics" /></button>
				<button class="utility queue-control" class:active={$queueOpen} data-queue-toggle onclick={() => queueOpen.update((open) => !open)} title="Queue" aria-label="Queue"><Icon name="queue" />{#if $upNext.length}<span class="queue-badge">{$upNext.length}</span>{/if}</button>
				<div class="more-wrap">
					<button class="utility more-toggle" class:active={showMore} onclick={toggleMore} title="More player controls" aria-label="More player controls"><Icon name="more" /></button>
					{#if showMore}
						<div class="more-menu" data-mini-popover role="menu">
							<button class="menu-item" class:active={$autoplayOn} onclick={() => setAutoplay(!$autoplayOn)} role="menuitem"><Icon name="autoplay" size={16}/><span>Autoplay</span><span class="state">{$autoplayOn ? 'On' : 'Off'}</span></button>
							<div class="menu-section">
								<span class="menu-label"><Icon name="crossfade" size={15}/> Crossfade</span>
								<div class="crossfade-options">
									{#each CROSSFADE_PRESETS as s}
										<button class:active={$crossfadeSeconds === s} onclick={() => chooseCrossfade(s)}>{s === 0 ? 'Off' : `${s}s`}</button>
									{/each}
								</div>
							</div>
							<button class="menu-item" class:active={$sleepTimerActive} onclick={(event) => { event.stopPropagation(); showSleep = !showSleep; }} role="menuitem"><Icon name="timer" size={16}/><span>Sleep timer</span><span class="state">{$sleepTimerActive ? 'On' : 'Off'}</span></button>
							{#if showSleep}
								<div class="sleep-options">
									<button onclick={() => pickSleep('off')}>Off</button>
									<button onclick={() => pickSleep('5')}>5 min</button>
									<button onclick={() => pickSleep('15')}>15 min</button>
									<button onclick={() => pickSleep('30')}>30 min</button>
									<button onclick={() => pickSleep('eot')}>End of track</button>
								</div>
							{/if}
						</div>
					{/if}
				</div>
			</div>
		</div>
	</footer>
{/if}

<style>
	.mini {
		position: fixed;
		left: 18px;
		right: 18px;
		bottom: 18px;
		z-index: 30;
		border: 1px solid rgba(255,255,255,.1);
		border-radius: 18px;
		padding: .55rem 1rem;
		background: rgba(20, 28, 38, 0.92);
		backdrop-filter: blur(24px) saturate(1.2);
		box-shadow: 0 20px 55px rgba(0, 0, 0, 0.35), inset 0 1px rgba(255,255,255,.04);
	}
	.player-grid {
		display: grid;
		grid-template-columns: minmax(190px, 1fr) minmax(260px, 460px) minmax(190px, 1fr);
		align-items: center;
		gap: 1rem;
	}
	.progress {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: .67rem;
		color: var(--quiet);
		font-variant-numeric: tabular-nums;
	}
	.progress input {
		flex: 1;
		min-width: 0;
		accent-color: var(--accent);
		cursor: pointer;
	}
	.t {
		min-width: 2.4rem;
		text-align: center;
	}
	.now {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		min-width: 0;
		width: 100%;
	}
	.now :global(img),
	.now :global(.placeholder) {
		width: 48px;
		height: 48px;
		border-radius: 11px;
		object-fit: cover;
		background: #e8e5e0;
	}
	.meta {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.title {
		font-size: .86rem;
		font-weight: 680;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.artist {
		font-size: 0.74rem;
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.track-action,
	.utility {
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		padding: 0;
		border: 0;
		border-radius: 10px;
		background: transparent;
		color: var(--muted);
		cursor: pointer;
		transition: background .16s ease, color .16s ease, transform .16s ease;
	}
	.track-action { margin-left: auto; }
	.track-action:hover, .utility:hover { background: #efedea; color: var(--text); }
	.track-action.active { color: var(--accent); }
	.utility.active { background: var(--accent-soft); color: var(--accent-deep); }
	.transport { display: grid; gap: .25rem; min-width: 0; }
	.controls {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: .2rem;
	}
	.controls button {
		border: 0;
		background: transparent;
		color: var(--text);
		font-size: 0.95rem;
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		padding: 0;
		border-radius: 10px;
		cursor: pointer;
	}
	.play {
		width: 40px;
		height: 40px;
		border-radius: 50% !important;
		background: var(--accent) !important;
		color: #fff !important;
		font-size: 1.05rem;
	}
	.tools {
		display: flex;
		align-items: center;
		gap: .2rem;
		min-width: 0;
		justify-content: flex-end;
		position: relative;
	}
	.vol {
		display: flex;
		align-items: center;
		gap: .35rem;
		font-size: 0.8rem;
	}
	.vol input {
		width: min(8vw, 96px);
		accent-color: var(--accent);
	}
	.ghost {
		border: none;
		background: transparent;
		color: var(--muted);
	}
	.ghost.active {
		color: var(--accent-deep);
	}
	.queue-control {
		position: relative;
	}
	.queue-badge {
		position: absolute;
		top: -2px;
		right: 0;
		min-width: 13px;
		height: 13px;
		padding: 0 3px;
		border-radius: 999px;
		background: var(--accent);
		color: #fff;
		font-size: 9px;
		font-weight: 800;
		line-height: 13px;
		text-align: center;
	}
	.more-wrap {
		position: relative;
	}
	.more-menu {
		position: absolute;
		bottom: 44px;
		right: 0;
		width: 214px;
		background: var(--surface-strong);
		border: 1px solid var(--line);
		border-radius: 12px;
		box-shadow: 0 12px 28px rgba(0, 0, 0, 0.28);
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 0.35rem;
		z-index: 10;
	}
	.menu-item {
		display: flex;
		align-items: center;
		gap: 0.55rem;
		width: 100%;
		border: none;
		background: transparent;
		padding: 0.45rem 0.5rem;
		border-radius: 7px;
		color: var(--text);
		font: inherit;
		font-size: 0.78rem;
		text-align: left;
		cursor: pointer;
	}
	.menu-item:hover,
	.menu-item.active {
		background: var(--accent-soft);
		color: var(--accent-deep);
	}
	.menu-item .state {
		margin-left: auto;
		color: var(--muted);
		font-size: 0.7rem;
	}
	.menu-section {
		padding: 0.45rem 0.5rem 0.55rem;
		border-top: 1px solid var(--line);
		border-bottom: 1px solid var(--line);
	}
	.menu-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 0.4rem;
		color: var(--muted);
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: .08em;
	}
	.crossfade-options,
	.sleep-options {
		display: flex;
		gap: 0.25rem;
		flex-wrap: wrap;
	}
	.crossfade-options button,
	.sleep-options button {
		border: 1px solid var(--line-strong);
		border-radius: 6px;
		background: transparent;
		color: var(--muted);
		padding: 0.25rem 0.45rem;
		font: inherit;
		font-size: 0.7rem;
		cursor: pointer;
	}
	.crossfade-options button:hover,
	.crossfade-options button.active,
	.sleep-options button:hover {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--accent-deep);
	}
	@media (max-width: 980px) {
		.player-grid { grid-template-columns: minmax(150px, 1fr) minmax(220px, 360px) minmax(132px, 1fr); gap: .55rem; }
		.controls .ghost { display: none; }
	}
	@media (max-width: 700px) {
		.player-grid { grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr); }
		.now { gap: .45rem; }
		.now :global(img), .now :global(.placeholder) { width: 40px; height: 40px; }
		.track-action, .vol, .more-wrap { display: none; }
		.tools { justify-content: flex-end; }
		.progress .t { display: none; }
	}
	@media (prefers-reduced-motion: reduce) { .track-action, .utility { transition: none; } }
</style>
