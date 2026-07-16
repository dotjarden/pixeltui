<script lang="ts">
	import { upNext, current, autoplayIDs, jumpTo, removeFromQueue } from '$lib/stores/player';
	import ArtImg from './ArtImg.svelte';
	import Icon from './Icon.svelte';

	let { open, onClose } = $props();
	let panel = $state<HTMLElement | undefined>();

	// Split up-next into user-queued (above the divider) and autoplay (∞ below).
	const userQ = $derived($upNext.filter((t) => !$autoplayIDs.has(t.id)));
	const autoQ = $derived($upNext.filter((t) => $autoplayIDs.has(t.id)));

	function offsetOf(trackId: string): number {
		return $upNext.findIndex((t) => t.id === trackId);
	}
	function handleWindowClick(event: MouseEvent) {
		const target = event.target;
		if (target instanceof Element && target.closest('[data-queue-toggle]')) return;
		if (panel && target instanceof Node && panel.contains(target)) return;
		if (open) onClose();
	}
</script>

<svelte:window onclick={handleWindowClick} />
{#if open}
	<aside class="sheet" bind:this={panel} aria-label="Up next queue">
		<header>
			<h3>Up Next</h3>
			<button onclick={onClose} aria-label="Close"><Icon name="close" /></button>
		</header>
		<div class="list">
			{#if $current}
				<div class="row now">
					<ArtImg ref={$current.art} alt="" eager />
					<div><span class="title">{$current.track}</span><span class="artist">{$current.artist}</span></div>
					<span class="playing">playing</span>
				</div>
			{/if}

			{#each userQ as t (t.id)}
				<div class="row">
					<button class="jump" onclick={() => jumpTo(offsetOf(t.id))}>
						<ArtImg ref={t.art} alt="" />
						<div><span class="title">{t.track}</span><span class="artist">{t.artist}</span></div>
					</button>
					<button class="rm" onclick={() => removeFromQueue(offsetOf(t.id))} title="remove" aria-label="Remove"><Icon name="close" size={14} /></button>
				</div>
			{/each}

			{#if autoQ.length}
				<div class="divider">∞ autoplay</div>
				{#each autoQ as t (t.id)}
					<div class="row auto">
						<button class="jump" onclick={() => jumpTo(offsetOf(t.id))}>
							<ArtImg ref={t.art} alt="" />
							<div><span class="title">{t.track}</span><span class="artist">{t.artist}</span></div>
						</button>
						<button class="rm" onclick={() => removeFromQueue(offsetOf(t.id))} title="remove" aria-label="Remove"><Icon name="close" size={14} /></button>
					</div>
				{/each}
			{/if}

			{#if $upNext.length === 0}
				<p class="empty">Queue is empty.</p>
			{/if}
		</div>
	</aside>
{/if}

<style>
	.sheet {
		position: fixed;
		right: 24px;
		bottom: 102px;
		width: min(380px, calc(100vw - 32px));
		max-height: min(68vh, 560px);
		background: var(--surface-strong);
		border: 1px solid var(--line);
		border-radius: 14px;
		z-index: 35;
		display: flex;
		flex-direction: column;
		box-shadow: 0 18px 45px rgba(31, 28, 25, 0.18);
		overflow: hidden;
		animation: queue-in .16s ease-out;
	}
	@keyframes queue-in { from { opacity: 0; transform: translateY(8px) scale(.98); } to { opacity: 1; transform: translateY(0) scale(1); } }
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.8rem 1rem;
		border-bottom: 1px solid var(--line);
	}
	h3 {
		margin: 0;
		font-size: 0.95rem;
	}
	header button {
		border: none;
		background: transparent;
		font-size: 0.95rem;
		cursor: pointer;
	}
	.list {
		overflow: auto;
		padding: 0.4rem;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.3rem;
		padding: 0.2rem;
		border-radius: 7px;
	}
	.row:hover {
		background: var(--surface);
	}
	.now :global(img), .now :global(.placeholder) {
		width: 36px;
		height: 36px;
		border-radius: 6px;
		object-fit: cover;
		flex-shrink: 0;
	}
	.jump {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		flex: 1;
		min-width: 0;
		text-align: left;
		border: none;
		background: transparent;
		padding: 0.2rem;
		border-radius: 6px;
		cursor: pointer;
	}
	.jump :global(img), .jump :global(.placeholder) {
		width: 36px;
		height: 36px;
		border-radius: 4px;
		object-fit: cover;
		background: #e8e5e0;
		flex-shrink: 0;
	}
	.jump > div {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
	}
	.title {
		font-size: 0.84rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.artist {
		font-size: 0.72rem;
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.now {
		background: var(--accent-soft);
		cursor: default;
	}
	.playing {
		font-size: 0.68rem;
		color: var(--accent-deep);
	}
	.rm {
		border: none;
		background: transparent;
		font-size: 0.72rem;
		color: var(--quiet);
		padding: 0.2rem 0.4rem;
		cursor: pointer;
		border-radius: 5px;
	}
	.rm:hover {
		color: var(--accent-deep);
	}
	.divider {
		margin: 0.5rem 0.35rem 0.2rem;
		font-size: 0.68rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--accent-deep);
	}
	.auto .title {
		color: var(--text);
	}
	.empty {
		color: var(--muted);
		padding: 1rem;
		text-align: center;
	}
</style>
