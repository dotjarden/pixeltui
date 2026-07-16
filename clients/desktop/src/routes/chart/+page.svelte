<script lang="ts">
	// Ranked chart for a country code (ZZ = global). Loads /api/charts?country=
	// and renders a numbered track list. Full chart UI is Phase 7; this is the
	// functional list.
	import { page } from '$app/stores';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import TrackNumberRow from '$lib/components/TrackNumberRow.svelte';
	import TrackListHeader from '$lib/components/TrackListHeader.svelte';
	import ArtImg from '$lib/components/ArtImg.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { goCollection } from '$lib/nav';
	import { playFromList } from '$lib/stores/player';
	import { api } from '$lib/api/client';
	import type { Track } from '$lib/api/types';

	let tracks = $state<Track[]>([]);
	let loading = $state(true);

	const country = $derived($page.url.searchParams.get('country') ?? 'ZZ');
	const title = $derived($page.url.searchParams.get('title') ?? 'Top Charts');
	const countryLabel = $derived(({ ZZ: 'Global', US: 'United States', GB: 'United Kingdom', DE: 'Germany', JP: 'Japan', BR: 'Brazil' } as Record<string, string>)[country] ?? country);
	const topTen = $derived(tracks.slice(0, 10));
	const chartArt = $derived(tracks[0]?.art);

	let requestSeq = 0;
	async function loadPage() {
		const seq = ++requestSeq;
		loading = true;
		tracks = [];
		try {
			const c = await api.charts(country);
			if (seq === requestSeq) tracks = c.tracks;
		} catch {
			/* leave empty */
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	$effect(() => {
		const key = country;
		void key;
		void loadPage();
	});
</script>

<DetailShell {title} {loading} immersive>
	{#if tracks.length}
		<section class="chart-stage">
			<ArtImg ref={chartArt} eager alt="" />
			<div class="stage-shade"></div>
			<div class="stage-content"><p>{countryLabel}</p><h1>{title}</h1><span>{tracks.length} songs shaping the chart</span><button onclick={() => playFromList(tracks, 0)}><Icon name="play" size={16} /> Play chart</button></div>
		</section>
		<section class="chart-songs">
			<div class="section-head"><p>This week</p><h2>Top 10</h2></div>
			<ul class="list track-list numbered">
				<TrackListHeader numbered />
				{#each topTen as t, i (t.id)}
					<TrackNumberRow track={t} context={tracks} number={i + 1} table />
				{/each}
			</ul>
			{#if tracks.length > 10}<button class="all-songs" onclick={() => goCollection({ title: `${countryLabel} Top Charts`, subtitle: 'Top charts', tracks })}><span>View all {tracks.length} songs</span><Icon name="next" size={16} /></button>{/if}
		</section>
	{:else if !loading}
		<p class="empty">No chart data.</p>
	{/if}
</DetailShell>

<style>
	.chart-stage { position: relative; isolation: isolate; height: clamp(400px, 52vh, 610px); width: calc(100vw - 32px); margin: 0 0 clamp(42px, 5vw, 72px) calc((100% - 100vw) / 2 + 16px); overflow: hidden; border-radius: 28px; background: #252321; box-shadow: 0 24px 64px rgba(47,42,37,.19), 0 5px 16px rgba(47,42,37,.08); color: #fff; }
	.chart-stage :global(img), .chart-stage :global(.placeholder) { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; transform: scale(1.03); filter: saturate(1.12) contrast(1.04); }
	.stage-shade { position: absolute; z-index: 1; inset: 0; background: linear-gradient(90deg, rgba(12,11,10,.76), rgba(12,11,10,.14) 75%), linear-gradient(0deg, rgba(12,11,10,.72), transparent 60%); }
	.stage-content { position: absolute; z-index: 2; bottom: clamp(30px, 5vw, 72px); left: clamp(30px, 5vw, 76px); display: grid; justify-items: start; gap: .4rem; }
	.stage-content p { margin: 0; color: rgba(255,255,255,.7) !important; font-size: .68rem; font-weight: 800; letter-spacing: .14em; text-transform: uppercase; }
	.stage-content h1 { margin: 0; color: #fff !important; font-size: clamp(3.5rem, 8vw, 8.8rem); font-weight: 790; letter-spacing: -.085em; line-height: .82; }
	.stage-content > span { color: rgba(255,255,255,.74); font-size: .9rem; }
	.stage-content button { display: inline-flex; align-items: center; gap: 7px; min-height: 42px; margin-top: .8rem; padding: 0 1rem; border: 0; border-radius: 999px; background: #fff; color: #24211f; font: inherit; font-size: .82rem; font-weight: 720; cursor: pointer; }
	.stage-content button:hover { background: var(--accent-soft); }
	.chart-songs { max-width: 1120px; margin-bottom: 4rem; }
	.section-head { margin-bottom: .85rem; }
	.section-head p { margin: 0 0 .25rem; color: var(--accent-deep); font-size: .68rem; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
	.section-head h2 { margin: 0; font-size: clamp(1.45rem, 2.2vw, 2rem); letter-spacing: -.055em; }
	.list { list-style: none; padding: 0; margin: 0; border: 1px solid var(--line); border-radius: 18px; overflow: hidden; background: rgba(255,255,255,.52); box-shadow: 0 8px 24px rgba(47,42,37,.05); }
	.all-songs { display: flex; align-items: center; justify-content: space-between; width: 100%; min-height: 52px; margin-top: 10px; padding: 0 .9rem 0 1rem; border: 1px solid var(--line); border-radius: 14px; background: rgba(255,255,255,.6); color: var(--accent-deep); font: inherit; font-size: .84rem; font-weight: 720; cursor: pointer; }
	.all-songs:hover { background: var(--surface-strong); border-color: var(--line-strong); }
	.empty {
		color: #888;
	}
	@container (max-width: 700px) { .chart-stage { height: 510px; width: calc(100vw - 24px); margin-left: calc((100% - 100vw) / 2 + 12px); border-radius: 22px; } .stage-content { bottom: 30px; left: 28px; } .stage-content h1 { font-size: clamp(3.4rem, 13vw, 6rem); } }
</style>
