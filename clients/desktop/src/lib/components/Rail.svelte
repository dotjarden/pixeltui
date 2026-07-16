<script lang="ts">
	let {
		title,
		action,
		edge = false,
		children
	}: {
		title: string;
		action?: { label: string; onclick: () => void };
		edge?: boolean;
		children?: import('svelte').Snippet;
	} = $props();
</script>

<section class="rail" class:edge>
	<header>
		<h3>{title}</h3>
		{#if action}
			<button onclick={action.onclick}>{action.label}</button>
		{/if}
	</header>
	<div class="scroll">
		{@render children?.()}
	</div>
</section>

<style>
	.rail {
		margin-bottom: 1.2rem;
	}
	header {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		margin-bottom: 0.5rem;
	}
	h3 {
		font-size: 0.95rem;
		margin: 0;
	}
	header button {
		border: none;
		background: transparent;
		color: #2a6df6;
		font-size: 0.78rem;
		cursor: pointer;
		padding: 0;
	}
	.scroll {
		display: flex;
		gap: 0.7rem;
		overflow-x: auto;
		padding-bottom: 0.3rem;
		scrollbar-width: thin;
	}
	.scroll::-webkit-scrollbar {
		height: 6px;
	}
	.scroll::-webkit-scrollbar-thumb {
		background: #ddd;
		border-radius: 3px;
	}
	/* An edge rail owns the viewport width while preserving a deliberate
	 * content inset for its label and first item. This keeps the last item
	 * travelling to the application edge instead of a centered max-width. */
	.rail.edge {
		width: 100vw;
		margin-left: calc((100% - 100vw) / 2);
		margin-right: calc((100% - 100vw) / 2);
	}
	.rail.edge header { padding-inline: clamp(20px, 5vw, 76px); }
	.rail.edge .scroll {
		gap: 14px;
		padding: 0 clamp(20px, 5vw, 76px) 8px;
		scroll-padding-inline: clamp(20px, 5vw, 76px);
	}
</style>
