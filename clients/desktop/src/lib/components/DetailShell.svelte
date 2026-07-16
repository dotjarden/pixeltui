<script lang="ts">
	// Common chrome for detail/list sub-pages. Detail pages own their immersive
	// hero; simple catalog pages render their heading in the page body.
	let {
		title,
		subtitle,
		loading = false,
		showHeading = false,
		immersive = false,
		children
	}: { title: string; subtitle?: string; loading?: boolean; showHeading?: boolean; immersive?: boolean; children?: import('svelte').Snippet } = $props();

</script>

<main class="detail" class:immersive-shell={immersive}>
	{#if showHeading}
		<div class="page-heading">
			<h1>{title}</h1>
			{#if subtitle}<p>{subtitle}</p>{/if}
		</div>
	{/if}
	{#if loading}
		<div class="loading-state" aria-label="Loading">
			<span class="loading-hero"></span>
			<span class="loading-line wide"></span>
			<span class="loading-line"></span>
			<span class="loading-line short"></span>
		</div>
	{/if}
	{@render children?.()}
</main>

<style>
	.detail {
		padding: 0 0 2rem;
		color: var(--text);
		container-type: inline-size;
	}
	.page-heading { margin: 0 0 1.5rem; }
	.page-heading h1 { margin: 0; font-size: clamp(2.1rem, 4vw, 3.8rem); letter-spacing: -.065em; line-height: .98; }
	.page-heading p { margin: .5rem 0 0; color: var(--muted); font-size: .88rem; }
	.loading-state { display: grid; gap: .7rem; padding: .5rem 0 2rem; }
	.loading-hero, .loading-line { display: block; border-radius: 8px; background: linear-gradient(100deg, #e9e6e2 25%, var(--surface-strong) 42%, #e9e6e2 62%); background-size: 220% 100%; animation: loading-shimmer 1.2s ease-in-out infinite; }
	.loading-hero { height: min(34vw, 280px); min-height: 180px; }
	.loading-line { width: 62%; height: 14px; }
	.loading-line.wide { width: 82%; height: 22px; }
	.loading-line.short { width: 38%; }
	@keyframes loading-shimmer { from { background-position: 120% 0; } to { background-position: -80% 0; } }
	@media (prefers-reduced-motion: reduce) { .loading-hero, .loading-line { animation: none; } }
</style>
