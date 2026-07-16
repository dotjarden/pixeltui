<script lang="ts">
	// Full play history, grouped by calendar day (newest first), each listen
	// playable like any catalog song. Mirrors iOS `HistoryView`. Online-only —
	// the Library card hides offline, like Stats.
	import { onMount } from 'svelte';
	import DetailShell from '$lib/components/DetailShell.svelte';
	import TrackRow from '$lib/components/TrackRow.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { api } from '$lib/api/client';
	import type { HistoryEntry } from '$lib/api/types';

	let entries = $state<HistoryEntry[]>([]);
	let loading = $state(true);

	onMount(async () => {
		try {
			entries = await api.history(200, false);
		} catch {
			/* leave empty */
		} finally {
			loading = false;
		}
	});

	// Bucket by local calendar day; server already returns most-recent first so
	// each bucket stays sorted. Newest day first.
	const days = $derived.by(() => {
		const groups = new Map<string, HistoryEntry[]>();
		for (const e of entries) {
			const d = new Date(e.played_at * 1000);
			const key = new Date(d.getFullYear(), d.getMonth(), d.getDate()).toISOString();
			const arr = groups.get(key);
			if (arr) arr.push(e);
			else groups.set(key, [e]);
		}
		return [...groups.entries()].sort((a, b) => (a[0] < b[0] ? 1 : -1));
	});

	function dayTitle(key: string): string {
		const day = new Date(key);
		const today = new Date();
		const startToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
		const t = day.getTime();
		if (t === startToday) return 'Today';
		if (t === startToday - 86_400_000) return 'Yesterday';
		return day.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
	}

	function timeOfDay(t: number): string {
		return new Date(t * 1000).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
	}
</script>

<DetailShell title="History" subtitle={loading ? '' : `${entries.length} plays`} {loading} showHeading>
	{#if entries.length}
		{#each days as [key, items] (key)}
			<h3 class="day">{dayTitle(key)}</h3>
			<ul class="list">
				{#each items as t, i (t.id + '-' + t.played_at)}
					<TrackRow track={t} context={items} index={i} time={timeOfDay(t.played_at)} />
				{/each}
			</ul>
		{/each}
	{:else if !loading}
		<div class="empty">
			<span class="icon"><Icon name="clock" size={28} /></span>
			<p>No plays yet. Play some music and every listen will show up here.</p>
		</div>
	{/if}
</DetailShell>

<style>
	.day {
		font-size: 0.82rem;
		font-weight: 600;
		color: #666;
		margin: 1rem 0 0.3rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
	.day:first-child {
		margin-top: 0;
	}
	.list {
		list-style: none;
		padding: 0;
		margin: 0;
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
