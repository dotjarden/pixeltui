<script lang="ts">
	// Track credits & info — mirrors iOS `TrackInfoView`. Two-stage load: the
	// fast payload (Track / Last.fm / History / lyrics flag) from
	// /api/trackinfo renders immediately; the slow yt-dlp YouTube card streams
	// in from /api/trackinfo/youtube so the page never waits on a subprocess.
	// Big numbers lead each section as KPI tiles; tags are chips; wiki and the
	// YouTube description are collapsible.
	import { page } from '$app/stores';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import ImmersiveHeader from '$lib/components/ImmersiveHeader.svelte';
	import TrackMenu from '$lib/components/TrackMenu.svelte';
	import { api } from '$lib/api/client';
	import { playFromList } from '$lib/stores/player';
	import type { Track, TrackInfo, TrackInfoYouTube, Capabilities } from '$lib/api/types';

	let info = $state<TrackInfo | null>(null);
	let yt = $state<TrackInfoYouTube | null>(null);
	let ytLoaded = $state(false);
	let loading = $state(true);

	const id = $derived(decodeURIComponent($page.params.id ?? ''));
	const sp = $derived($page.url.searchParams);
	const title = $derived(sp.get('title') ?? id);
	const artist = $derived(sp.get('artist') ?? '');
	const album = $derived(sp.get('album') ?? undefined);
	const duration = $derived(Number(sp.get('duration') ?? 0));
	const art = $derived(sp.get('art') ?? undefined);
	const source = $derived(sp.get('source') ?? '');
	const isYouTube = $derived(id.startsWith('yt:'));

	const track = $derived.by(() => {
		const caps: Capabilities = {
			start_station: false,
			go_to_artist: !!artist && artist !== 'Unknown Artist',
			go_to_album: !!album && album !== 'Singles',
			radio: false,
			download: true,
			lyrics: true
		};
		return {
			id,
			track: title,
			artist,
			album,
			duration,
			art,
			source,
			capabilities: caps
		};
	});

	const sourceLabel = $derived.by(() => {
		switch (id.split(':')[0]) {
			case 'yt':
				return 'YouTube';
			case 'lo':
				return 'Server Files';
			case 'su':
				return 'Subsonic';
			default:
				return source || 'Unknown';
		}
	});

	let requestSeq = 0;
	async function loadPage() {
		const seq = ++requestSeq;
		info = null;
		yt = null;
		ytLoaded = false;
		loading = true;
		try {
			const result = await api.trackInfo(track);
			if (seq === requestSeq) info = result;
		} catch {
			if (seq === requestSeq) info = null;
		} finally {
			if (seq === requestSeq) loading = false;
		}
		// Slow yt-dlp card — fire lazily so it can never block the page.
		if (isYouTube) {
			try {
				const res = await api.trackInfoYouTube(id);
				if (seq === requestSeq) yt = res.youtube ?? null;
			} catch {
				if (seq === requestSeq) yt = null;
			} finally {
				if (seq === requestSeq) ytLoaded = true;
			}
		}
	}

	$effect(() => {
		const key = `${id}|${title}|${artist}|${album ?? ''}|${duration}|${art ?? ''}`;
		void key;
		void loadPage();
	});

	function play() {
		playFromList([track], 0);
	}

	function fmtDur(sec: number): string {
		if (!sec || sec <= 0) return '';
		const m = Math.floor(sec / 60);
		const r = Math.floor(sec % 60);
		return `${m}:${String(r).padStart(2, '0')}`;
	}

	function fmtNum(n?: number): string {
		if (n === undefined || n === null) return '';
		return n.toLocaleString();
	}

	function fmtDate(t: number): string {
		if (!t) return '';
		return new Date(t * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}

	/// yt-dlp upload_date is "YYYYMMDD".
	function fmtYtDate(s?: string): string {
		if (!s || s.length !== 8) return s ?? '';
		return `${s.slice(0, 4)}-${s.slice(4, 6)}-${s.slice(6, 8)}`;
	}

	function totalListenTime(): string {
		const dur = info?.duration ?? duration;
		if (!dur || !info?.history) return '';
		return fmtDur(dur * info.history.plays);
	}
</script>

<DetailShell title={info?.title ?? title} subtitle={info?.artist ?? artist} {loading} immersive>
	{#if info || !loading}
		<ImmersiveHeader
			art={info?.art ?? art}
			title={info?.title ?? title}
			subtitle={info?.artist ?? artist}
			onPlay={play}
			onShuffle={play}
			showShuffle={false}
		>
			<TrackMenu {track} context={[track]} />
		</ImmersiveHeader>

		{#if info}
			<section class="sec">
				<h3>Track</h3>
				<div class="rows">
					<div class="row"><span class="k">Source</span><span class="v">{sourceLabel}</span></div>
					{#if (info.duration ?? duration) > 0}
						<div class="row">
							<span class="k">Duration</span>
							<span class="v">{fmtDur(info.duration ?? duration)}</span>
						</div>
					{/if}
					{#if info.lyrics !== undefined}
						<div class="row">
							<span class="k">Lyrics</span>
							<span class="v">{info.lyrics ? 'Available' : 'None'}</span>
						</div>
					{/if}
				</div>
			</section>

			{#if info.lastfm && ((info.lastfm.playcount ?? 0) > 0 || (info.lastfm.listeners ?? 0) > 0 || (info.lastfm.tags?.length ?? 0) > 0 || info.lastfm.wiki)}
				<section class="sec">
					<h3>Last.fm</h3>
					{#if (info.lastfm.playcount ?? 0) > 0 || (info.lastfm.listeners ?? 0) > 0}
						<div class="tiles">
							{#if (info.lastfm.playcount ?? 0) > 0}
								<div class="tile"><span class="n">{fmtNum(info.lastfm.playcount)}</span><span class="l">Scrobbles</span></div>
							{/if}
							{#if (info.lastfm.listeners ?? 0) > 0}
								<div class="tile"><span class="n">{fmtNum(info.lastfm.listeners)}</span><span class="l">Listeners</span></div>
							{/if}
						</div>
					{/if}
					{#if info.lastfm.tags?.length}
						<div class="tags">
							{#each info.lastfm.tags as tag (tag)}<span class="tag">{tag}</span>{/each}
						</div>
					{/if}
					{#if info.lastfm.wiki}
						<details class="bio">
							<summary>About this track</summary>
							<p>{info.lastfm.wiki}</p>
						</details>
					{/if}
				</section>
			{/if}

			{#if isYouTube}
				<section class="sec">
					<h3>YouTube</h3>
					{#if !ytLoaded}
						<p class="hint">Loading YouTube details…</p>
					{:else if yt}
						{#if (yt.views ?? 0) > 0 || (yt.duration ?? 0) > 0}
							<div class="tiles">
								{#if (yt.views ?? 0) > 0}
									<div class="tile"><span class="n">{fmtNum(yt.views)}</span><span class="l">Views</span></div>
								{/if}
								{#if (yt.duration ?? 0) > 0}
									<div class="tile"><span class="n">{fmtDur(yt.duration ?? 0)}</span><span class="l">Duration</span></div>
								{/if}
							</div>
						{/if}
						<div class="rows">
							{#if yt.channel}<div class="row"><span class="k">Channel</span><span class="v">{yt.channel}</span></div>{/if}
							{#if yt.upload_date}<div class="row"><span class="k">Uploaded</span><span class="v">{fmtYtDate(yt.upload_date)}</span></div>{/if}
							{#if yt.license}<div class="row"><span class="k">License</span><span class="v">{yt.license}</span></div>{/if}
						</div>
						{#if yt.description}
							<details class="bio">
								<summary>Description</summary>
								<p>{yt.description}</p>
							</details>
						{/if}
					{:else}
						<p class="hint">No YouTube details available.</p>
					{/if}
				</section>
			{/if}

			{#if info.history}
				<section class="sec">
					<h3>Your History</h3>
					<div class="tiles">
						<div class="tile"><span class="n">{fmtNum(info.history.plays)}</span><span class="l">Plays</span></div>
						{#if totalListenTime()}
							<div class="tile"><span class="n">{totalListenTime()}</span><span class="l">Total Time</span></div>
						{/if}
					</div>
					<div class="rows">
						{#if info.history.first_played}
							<div class="row"><span class="k">First Played</span><span class="v">{fmtDate(info.history.first_played)}</span></div>
						{/if}
						{#if info.history.last_played}
							<div class="row"><span class="k">Last Played</span><span class="v">{fmtDate(info.history.last_played)}</span></div>
						{/if}
					</div>
				</section>
			{/if}
		{:else if !loading}
			<div class="empty">
				<p>No info available for this track.</p>
			</div>
		{/if}
	{:else if !loading}
		<p class="empty">No info available.</p>
	{/if}
</DetailShell>

<style>
	.sec {
		margin-bottom: 1.6rem;
	}
	.sec h3 {
		font-size: 0.82rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: #666;
		margin: 0 0 0.6rem;
	}
	.tiles {
		display: flex;
		gap: 0.6rem;
		margin-bottom: 0.8rem;
	}
	.tile {
		display: flex;
		flex-direction: column;
		align-items: center;
		background: #f6f8fc;
		border-radius: 10px;
		padding: 0.7rem 1rem;
		min-width: 5rem;
	}
	.tile .n {
		font-size: 1.15rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}
	.tile .l {
		font-size: 0.7rem;
		color: #888;
		margin-top: 0.15rem;
	}
	.rows {
		display: flex;
		flex-direction: column;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.4rem 0;
		border-bottom: 1px solid #f0f0f0;
		font-size: 0.86rem;
	}
	.row .k {
		color: #777;
	}
	.row .v {
		color: #222;
		text-align: right;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
		margin-bottom: 0.8rem;
	}
	.tag {
		font-size: 0.74rem;
		background: #eef4ff;
		color: #2a6df6;
		border-radius: 999px;
		padding: 0.2rem 0.7rem;
	}
	.bio {
		margin-top: 0.5rem;
	}
	.bio summary {
		cursor: pointer;
		font-size: 0.84rem;
		color: #2a6df6;
	}
	.bio p {
		font-size: 0.82rem;
		color: #444;
		line-height: 1.55;
		margin: 0.5rem 0 0;
		white-space: pre-wrap;
	}
	.hint {
		font-size: 0.84rem;
		color: #888;
	}
	.empty {
		color: #888;
		padding: 2rem 0;
	}
</style>
