<script lang="ts">
	// <img> with a graceful gradient fallback when art is missing or fails to
	// load (common for `lo:`/`su:` tracks whose `/api/art` has no embedded art).
	import { artUrl } from '$lib/api/client';

	let {
		ref,
		alt = '',
		radius = 6,
		size,
		eager = false
	}: { ref?: string; alt?: string; radius?: number; size?: string; eager?: boolean } = $props();

	let failed = $state(false);
	let previousRef: string | undefined;
	$effect(() => {
		if (ref !== previousRef) {
			previousRef = ref;
			failed = false;
		}
	});
	const url = $derived(failed || !ref ? '' : artUrl(ref, eager));
	const style = $derived(size ? `width:${size};height:${size}` : '');
</script>

{#if url}
	<img src={url} {alt} {style} loading={eager ? 'eager' : 'lazy'} decoding="async" onerror={() => (failed = true)} />
{:else}
	<div class="placeholder" {style} aria-hidden="true"></div>
{/if}

<style>
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.placeholder {
		background: linear-gradient(145deg, #384c63, #785866);
		position: relative;
		isolation: isolate;
	}
	.placeholder::after {
		content: '';
		position: absolute;
		inset: 18%;
		border: 1px solid rgba(255,255,255,.3);
		border-radius: 50%;
	}
</style>
