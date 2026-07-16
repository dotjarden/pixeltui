<script lang="ts">
	import ArtImg from './ArtImg.svelte';
	import Icon from './Icon.svelte';

	let {
		art,
		title,
		subtitle,
		description,
		onPlay,
		onShuffle,
		playDisabled = false,
		showShuffle = true,
		children
	}: {
		art?: string;
		title: string;
		subtitle?: string;
		description?: string;
		onPlay: () => void;
		onShuffle: () => void;
		playDisabled?: boolean;
		showShuffle?: boolean;
		children?: import('svelte').Snippet;
	} = $props();
</script>

<header class="media-header">
	<div class="media-atmosphere"><ArtImg ref={art} eager alt="" /></div>
	<div class="media-wash"></div>
	<div class="details">
		<h1>{title}</h1>
		{#if subtitle}<p class="subtitle">{subtitle}</p>{/if}
		{#if description}<p class="description">{description}</p>{/if}
		<div class="media-actions">
			<button class="play" onclick={onPlay} disabled={playDisabled} aria-label={`Play ${title}`} title="Play"><Icon name="play" size={18} /></button>
			{#if showShuffle}<button class="shuffle" onclick={onShuffle} disabled={playDisabled}><Icon name="shuffle" size={15} /> Shuffle</button>{/if}
			{@render children?.()}
		</div>
	</div>
	<div class="cover"><ArtImg ref={art} eager alt={title} /></div>
</header>

<style>
	.media-header { position: relative; isolation: isolate; display: flex; min-height: clamp(330px, 43vh, 470px); width: calc(100vw - 32px); margin: 0 0 clamp(38px, 5vw, 68px) calc((100% - 100vw) / 2 + 16px); overflow: hidden; border-radius: 28px; background: #292623; box-shadow: 0 24px 64px rgba(47,42,37,.18), 0 5px 16px rgba(47,42,37,.08); color: #fff; }
	.media-atmosphere, .media-wash { position: absolute; inset: 0; }
	.media-atmosphere { opacity: .7; transform: scale(1.08); filter: saturate(1.12) blur(12px); }
	.media-atmosphere :global(img), .media-atmosphere :global(.placeholder) { width: 100%; height: 100%; object-fit: cover; }
	.media-wash { z-index: 1; background: linear-gradient(90deg, rgba(17,15,14,.88) 0%, rgba(17,15,14,.53) 50%, rgba(17,15,14,.16) 100%), linear-gradient(0deg, rgba(17,15,14,.52), transparent 60%); }
	.details { position: relative; z-index: 2; align-self: flex-end; min-width: 0; max-width: min(680px, 58%); padding: clamp(28px, 5vw, 70px); }
	.details h1 { margin: 0; color: #fff !important; font-size: clamp(2.8rem, 5.8vw, 6.6rem); font-weight: 770; line-height: .9; letter-spacing: -.075em; text-wrap: balance; }
	.subtitle { margin: .75rem 0 0; color: rgba(255,255,255,.76); font-size: .94rem; line-height: 1.4; }
	.description { display: -webkit-box; margin: .8rem 0 0; overflow: hidden; color: rgba(255,255,255,.7); font-size: .84rem; line-height: 1.5; -webkit-box-orient: vertical; -webkit-line-clamp: 2; line-clamp: 2; }
	.cover { position: absolute; z-index: 2; right: clamp(24px, 5vw, 72px); bottom: clamp(24px, 5vw, 64px); width: clamp(148px, 21vw, 284px); aspect-ratio: 1; overflow: hidden; border-radius: 18px; background: var(--surface-strong); box-shadow: 0 22px 48px rgba(0,0,0,.29), inset 0 1px 0 rgba(255,255,255,.32); }
	.cover :global(img), .cover :global(.placeholder) { width: 100%; height: 100%; object-fit: cover; }
	.media-actions { display: flex; align-items: center; gap: .55rem; flex-wrap: wrap; margin-top: 1.2rem; }
	.play, .shuffle {
		display: inline-grid;
		place-items: center;
		border: 0;
		font: inherit;
		font-size: .82rem;
		font-weight: 700;
		cursor: pointer;
	}
	.play {
		width: 40px;
		height: 40px;
		border-radius: 50%;
		background: var(--accent);
		color: #fff;
		box-shadow: 0 7px 15px rgba(201,71,87,.22);
	}
	.shuffle { grid-auto-flow: column; gap: 7px; min-height: 36px; padding: 0 .8rem; border-radius: 999px; background: rgba(20,18,17,.35); border: 1px solid rgba(255,255,255,.18); color: #fff; backdrop-filter: blur(12px); }
	.play:hover { background: var(--accent-deep); }
	.shuffle:hover { background: rgba(255,255,255,.18); }
	.play:disabled, .shuffle:disabled { opacity: .45; cursor: default; }
	.media-actions :global(.ed-btn) { min-height: 36px; padding: 0 .8rem; border: 1px solid rgba(255,255,255,.18); border-radius: 999px; background: rgba(20,18,17,.35); color: #fff; font: inherit; font-size: .82rem; font-weight: 650; cursor: pointer; backdrop-filter: blur(12px); }
	.media-actions :global(.ed-btn:hover) { background: rgba(255,255,255,.18); }
	.media-actions :global(.ed-btn.danger) { color: #ffd3d8; }
	@container (max-width: 720px) {
		.media-header { min-height: 440px; width: calc(100vw - 24px); margin-left: calc((100% - 100vw) / 2 + 12px); border-radius: 22px; }
		.details { max-width: 84%; padding: 28px; }
		.cover { right: 24px; bottom: 24px; width: 132px; border-radius: 14px; }
		.details h1 { font-size: clamp(2.5rem, 10vw, 4.8rem); }
		.description { display: none; }
		.media-actions { margin-top: .8rem; }
	}
</style>
