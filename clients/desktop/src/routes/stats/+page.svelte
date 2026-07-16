<script lang="ts">
	// Listening stats from the shared play history — mirrors iOS `StatsView`:
	// a range picker (All Time / 30 Days / 7 Days), totals, ranked top artists
	// and top tracks. Top tracks are playable when the server knows the stream
	// id (we reconstruct a Track, like iOS `song(forStatsTrack:)`).
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ArtImg from '$lib/components/ArtImg.svelte';
	import { api } from '$lib/api/client';
	import { playFromList } from '$lib/stores/player';
	import { goArtist } from '$lib/nav';
	import Icon from '$lib/components/Icon.svelte';
	import type { Stats, StatsEntry, Track, Capabilities } from '$lib/api/types';

	const RANGES = [
		{ days: 0, label: 'All Time' },
		{ days: 30, label: '30 Days' },
		{ days: 7, label: '7 Days' }
	];

	let range = $state(0);
	let s = $state<Stats | null>(null);
	let loading = $state(true);

	function sourceFor(id: string, src?: string): string {
		if (src) return src;
		if (id.startsWith('lo:')) return 'local';
		if (id.startsWith('su:')) return 'subsonic';
		if (id.startsWith('yt:')) return 'youtube';
		return 'unknown';
	}

	/** Build a playable Track from a stats row (mirror iOS `song(forStatsTrack:)`). */
	function toTrack(e: StatsEntry): Track | null {
		if (!e.id) return null;
		const caps: Capabilities = {
			start_station: false,
			go_to_artist: !!e.artist && e.artist !== 'Unknown Artist',
			go_to_album: false,
			radio: false,
			download: true,
			lyrics: true
		};
		return {
			id: e.id,
			track: e.name,
			artist: e.artist ?? '',
			album: undefined,
			duration: 0,
			art: e.art,
			source: sourceFor(e.id, e.source),
			capabilities: caps
		};
	}

	async function load(days: number) {
		loading = true;
		try {
			s = await api.stats(days);
		} catch {
			s = null;
		} finally {
			loading = false;
		}
	}

	// Refetch whenever the range changes (and on first mount).
	$effect(() => {
		void load(range);
	});

	function playEntry(e: StatsEntry) {
		const t = toTrack(e);
		if (!t) return;
		playFromList([t], 0);
	}

	function listened(seconds: number): string {
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		return h > 0 ? `${h}h ${m}m` : `${m}m`;
	}

	const rangeLabel = $derived(RANGES.find((r) => r.days === range)?.label ?? '');
</script>

<DetailShell title="Stats" subtitle={loading ? '' : rangeLabel} {loading} showHeading>
	<div class="ranges">
		{#each RANGES as r (r.days)}
			<button class="seg" class:active={range === r.days} onclick={() => (range = r.days)}>
				{r.label}
			</button>
		{/each}
	</div>

	{#if s && s.plays > 0}
		<div class="summary">
			<div class="tile"><span class="n">{s.plays}</span><span class="l">Plays</span></div>
			<div class="tile"><span class="n">{s.unique_tracks}</span><span class="l">Tracks</span></div>
			<div class="tile"><span class="n">{s.unique_artists}</span><span class="l">Artists</span></div>
			<div class="tile"><span class="n">{listened(s.seconds)}</span><span class="l">Listened</span></div>
		</div>

		{#if s.top_artists.length}
			<h3 class="sec">Top Artists</h3>
			<ul class="list">
				{#each s.top_artists as e, i (e.name)}
					<li class="srow">
						<span class="rank">{i + 1}</span>
						<button class="namebtn" onclick={() => goArtist(e.name)}>
							<span class="name">{e.name}</span>
						</button>
						<span class="plays">{e.plays} play{e.plays === 1 ? '' : 's'}</span>
					</li>
				{/each}
			</ul>
		{/if}

		{#if s.top_tracks.length}
			<h3 class="sec">Top Tracks</h3>
			<ul class="list">
				{#each s.top_tracks as e, i (e.name + (e.artist ?? ''))}
					{@const t = toTrack(e)}
					<li class="srow">
						<span class="rank">{i + 1}</span>
						<ArtImg ref={e.art} size="44px" />
						<button class="playname" disabled={!t} onclick={() => playEntry(e)}>
							<span class="name">{e.name}</span>
							{#if e.artist}<span class="sub">{e.artist}</span>{/if}
						</button>
						<span class="plays">{e.plays} play{e.plays === 1 ? '' : 's'}</span>
					</li>
				{/each}
			</ul>
		{/if}
	{:else if !loading}
		<div class="empty">
			<span class="icon"><Icon name="chart" size={28}/></span>
			<p>No plays yet. Play some music and your listening stats will show up here.</p>
		</div>
	{/if}
</DetailShell>

<style>
	.ranges {
		display: flex;
		gap: 0.4rem;
		margin-bottom: 1rem;
	}
	.seg {
		border: 1px solid var(--line);
		background: transparent;
		border-radius: 999px;
		padding: 0.35rem 1rem;
		font: inherit;
		font-size: 0.82rem;
		cursor: pointer;
		color: var(--muted);
	}
	.seg.active {
		background: var(--accent);
		border-color: var(--accent);
		color: #fff;
	}
	.summary {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 0.6rem;
		margin-bottom: 1.2rem;
	}
	.tile {
		display: flex;
		flex-direction: column;
		align-items: center;
		background: var(--surface);
		border-radius: 10px;
		padding: 0.8rem 0.4rem;
	}
	.n {
		font-size: 1.25rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		line-height: 1.1;
	}
	.l {
		font-size: 0.72rem;
		color: var(--muted);
		margin-top: 0.2rem;
	}
	.sec {
		font-size: 0.9rem;
		margin: 1rem 0 0.4rem;
	}
	.list {
		list-style: none;
		padding: 0;
		margin: 0 0 1rem;
	}
	.srow {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		padding: 0.35rem 0;
		border-bottom: 1px solid var(--line);
	}
	.rank {
		font-variant-numeric: tabular-nums;
		color: var(--quiet);
		font-size: 0.85rem;
		font-weight: 600;
		min-width: 1.4rem;
		text-align: right;
	}
	.srow :global(img),
	.srow :global(.placeholder) {
		width: 44px;
		height: 44px;
		border-radius: 7px;
		object-fit: cover;
		flex-shrink: 0;
	}
	.namebtn,
	.playname {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		border: none;
		background: transparent;
		text-align: left;
		cursor: pointer;
		font: inherit;
		padding: 0;
	}
	.playname:disabled {
		cursor: default;
	}
	.name {
		font-size: 0.88rem;
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		font-size: 0.76rem;
		color: #666;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.plays {
		font-variant-numeric: tabular-nums;
		color: #aaa;
		font-size: 0.78rem;
		white-space: nowrap;
	}
	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.6rem;
		color: #888;
		padding: 3rem 1rem;
		text-align: center;
	}
	.empty .icon {
		font-size: 2rem;
	}
	.empty p {
		margin: 0;
		max-width: 36ch;
		line-height: 1.5;
	}
</style>
