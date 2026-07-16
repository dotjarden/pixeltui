<script lang="ts">
	let { numbered = false }: { numbered?: boolean } = $props();
</script>

<li class="list-head" aria-hidden="true">
	{#if numbered}<span class="number"></span>{/if}
	<span class="art"></span>
	<span>Song</span>
	<span>Artist</span>
	<span class="album">Album</span>
	<span class="length">Length</span>
	<span class="menu"></span>
</li>

<style>
	.list-head {
		display: grid;
		grid-template-columns: var(--track-grid);
		align-items: center;
		column-gap: var(--track-gap);
		padding: .42rem var(--track-inline) .45rem;
		border-bottom: 1px solid var(--line-strong);
		color: var(--quiet);
		font-size: .68rem;
		font-weight: 750;
		letter-spacing: .08em;
		line-height: 1;
		text-transform: uppercase;
	}
	.list-head > span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.number, .art, .menu { display: block; }
	.length { text-align: right; }
	@media (max-width: 760px) {
		.list-head { display: none; }
	}
</style>
