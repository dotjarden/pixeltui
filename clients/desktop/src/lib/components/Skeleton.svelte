<script lang="ts">
	// Shimmer placeholder for loading rails. `count` renders N cards.
	let { count = 6, width = 130, height = 130 }: { count?: number; width?: number; height?: number } = $props();
</script>

{#each Array(count) as _, i}
	<div class="sk" style="width:{width}px;height:{height + 36}px" aria-hidden="true">
		<div class="art" style="height:{height}px"></div>
		<div class="line"></div>
		<div class="line short"></div>
	</div>
{/each}

<style>
	.sk {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
		flex-shrink: 0;
	}
	.art {
		border-radius: 8px;
		background: linear-gradient(90deg, #eee 25%, #f5f5f5 37%, #eee 63%);
		background-size: 400% 100%;
		animation: shimmer 1.4s ease infinite;
	}
	.line {
		height: 9px;
		border-radius: 4px;
		background: #eee;
	}
	.line.short {
		width: 60%;
	}
	@keyframes shimmer {
		0% {
			background-position: 100% 0;
		}
		100% {
			background-position: 0 0;
		}
	}
</style>