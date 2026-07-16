<script lang="ts">
	// Generic "song collection" page (Liked, Recently Played, Made For You,
	// Daily Mix, Songs search see-all, source-genre). Tracks are staged in the
	// `collection` store by `goCollection()` before navigation; this page just
	// renders them.
	import DetailShell from '$lib/components/DetailShell.svelte';
	import TrackRow from '$lib/components/TrackRow.svelte';
	import TrackListHeader from '$lib/components/TrackListHeader.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { collection } from '$lib/stores/collection';
	import { playFromList, playShuffle } from '$lib/stores/player';

	let c = $derived($collection);
</script>

<DetailShell title={c?.title ?? 'Collection'} immersive>
	{#if c && c.tracks.length}
		{@const collectionData = c}
		<section class="collection-stage">
			<p>Collection</p>
			<h1>{collectionData.title}</h1>
			{#if collectionData.subtitle}<span>{collectionData.subtitle}</span>{/if}
			<div class="actions">
				<button class="primary" onclick={() => playFromList(collectionData.tracks, 0)}><Icon name="play" size={14} /> Play</button>
				<button onclick={() => playShuffle(collectionData.tracks)}><Icon name="shuffle" size={14} /> Shuffle</button>
				<span class="count">{collectionData.tracks.length} songs</span>
			</div>
		</section>
		<ul class="list track-list">
			<TrackListHeader />
			{#each collectionData.tracks as t, i (t.id)}
				<TrackRow track={t} context={collectionData.tracks} index={i} table />
			{/each}
		</ul>
	{:else}
		<p class="empty">Nothing here.</p>
	{/if}
</DetailShell>

<style>
	.collection-stage { width: calc(100vw - 32px); margin: 0 0 clamp(38px, 5vw, 64px) calc((100% - 100vw) / 2 + 16px); padding: clamp(30px, 5vw, 66px); border-radius: 28px; background: linear-gradient(135deg, #393532, #242220 67%, #4b2f34); box-shadow: 0 24px 64px rgba(47,42,37,.17); color: #fff; }
	.collection-stage p { margin: 0 0 .45rem; color: rgba(255,255,255,.66) !important; font-size: .68rem; font-weight: 800; letter-spacing: .14em; text-transform: uppercase; }
	.collection-stage h1 { margin: 0; color: #fff !important; font-size: clamp(3rem, 6vw, 6.6rem); font-weight: 770; letter-spacing: -.075em; line-height: .88; }
	.collection-stage > span { display: block; margin-top: .75rem; color: rgba(255,255,255,.72); font-size: .92rem; }
	.actions {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		margin-top: 1.25rem;
	}
	.actions button {
		display: inline-flex;
		align-items: center;
		gap: 7px;
		border: 1px solid rgba(255,255,255,.2);
		background: rgba(20,18,17,.35);
		border-radius: 999px;
		padding: 0.35rem 1rem;
		font: inherit;
		font-size: 0.82rem;
		cursor: pointer;
	}
	.actions .primary { border-color: #fff; background: #fff; color: #24211f; }
	.actions button:hover {
		background: rgba(255,255,255,.18);
		color: #fff;
	}
	.count {
		color: rgba(255,255,255,.7);
		font-size: 0.8rem;
	}
	.list {
		list-style: none;
		padding: 0;
		margin: 0;
		border: 1px solid var(--line);
		border-radius: 18px;
		overflow: hidden;
		background: rgba(255,255,255,.52);
		box-shadow: 0 8px 24px rgba(47,42,37,.05);
	}
	.empty {
		color: #888;
	}
	@container (max-width: 700px) { .collection-stage { width: calc(100vw - 24px); margin-left: calc((100% - 100vw) / 2 + 12px); border-radius: 22px; padding: 30px; } }
</style>
