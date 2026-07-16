<script lang="ts">
	// Party remote UI — mirrors iOS `PartyView`. The host device is the speaker;
	// this client stays silent, rendering the room snapshot and offering
	// transport/enqueue controls. Join by code; leave to exit.
	import ArtImg from './ArtImg.svelte';
	import TrackMenu from './TrackMenu.svelte';
	import {
		room,
		joined,
		joining,
		joinError,
		displayPosition,
		join,
		leave,
		next,
		togglePause,
		enqueue
	} from '$lib/stores/party';
	import type { Track } from '$lib/api/types';
	import { openTrackContextMenu } from '$lib/stores/contextMenu';
	import Icon from './Icon.svelte';

	let code = $state('');

	const snap = $derived($room);
	const track = $derived(snap?.track);

	function openContextMenu(event: MouseEvent, item: Track) {
		openTrackContextMenu(event, item, snap?.queue);
	}
	const pos = $derived($displayPosition);
	const dur = $derived(track?.duration ?? 0);

	function doJoin() {
		if (code.trim().length) void join(code);
	}
	function doLeave() {
		void leave();
		code = '';
	}
	function fmt(s: number): string {
		const m = Math.floor(s / 60);
		const r = Math.floor(s % 60);
		return `${m}:${String(r).padStart(2, '0')}`;
	}
</script>

{#if !$joined}
	<div class="join">
		<div class="icon" aria-hidden="true"><Icon name="users" size={28} /></div>
		<h2>Party remote</h2>
		<p class="lede">The host device is the speaker — this client stays silent. Enter the room code shown on the host to join and control playback together.</p>
		<form
			onsubmit={(e) => {
				e.preventDefault();
				doJoin();
			}}
		>
			<input
				bind:value={code}
				placeholder="Room code"
				spellcheck="false"
				autocomplete="off"
				class="code"
				disabled={$joining}
			/>
			<button type="submit" class="joinbtn" disabled={$joining || code.trim().length === 0}>
				{$joining ? 'Joining…' : 'Join'}
			</button>
		</form>
		{#if $joinError}<p class="err">{$joinError}</p>{/if}
	</div>
{:else if snap}
	<div class="room">
		<div class="np">
			{#if track}
				<ArtImg ref={track.art} size="120px" />
				<div class="npmeta">
					<span class="t">{track.track}</span>
					<span class="a">{track.artist}</span>
					<span class="roomcode">Room {snap.code}</span>
					{#if dur > 0}
						<div class="bar"><div class="fill" style="width:{Math.min(100, (pos / dur) * 100)}%"></div></div>
						<span class="time">{fmt(pos)} / {fmt(dur)}</span>
					{/if}
				</div>
			{:else}
				<div class="emptynp">
					<span class="icon" aria-hidden="true"><Icon name="play" size={22} /></span>
					<p>Nothing playing yet. Add a song to get the party started.</p>
				</div>
			{/if}
		</div>

		<div class="transport">
				<button class="tr" onclick={togglePause} disabled={!track} aria-label={snap.paused ? 'Resume' : 'Pause'}>
					<Icon name={snap.paused ? 'play' : 'pause'} />
				</button>
				<button class="tr" onclick={() => next()} disabled={!track} aria-label="Next"><Icon name="next" /></button>
		</div>

		<section class="sec">
			<h3>Up Next</h3>
			{#if snap.queue.length}
				<ul class="qlist">
					{#each snap.queue as t, i (t.id + '-' + i)}
						<li class="qrow" oncontextmenu={(event) => openContextMenu(event, t)}>
							<span class="num">{i + 1}</span>
							<ArtImg ref={t.art} size="36px" />
							<div class="qi">
								<span class="qt">{t.track}</span>
								<span class="qa">{t.artist}</span>
							</div>
							<TrackMenu track={t} label="More" />
						</li>
					{/each}
				</ul>
			{:else}
				<p class="muted">Queue is empty. Use the More menu on any song and choose “Add to Party”.</p>
			{/if}
		</section>

		<section class="sec">
			<h3>In the room</h3>
			{#if snap.members.length}
				<ul class="mlist">
					{#each snap.members as m (m.id)}
						<li class="mrow"><span class="person"><Icon name="user" size={16}/></span><span>{m.name}</span></li>
					{/each}
				</ul>
			{:else}
				<p class="muted">Just you so far — the host isn't listed.</p>
			{/if}
		</section>

		<button class="leave" onclick={doLeave}>Leave Party</button>
	</div>
{/if}

<style>
	.join {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.6rem;
		padding: 3rem 1.5rem;
		text-align: center;
		max-width: 460px;
		margin: 0 auto;
	}
	.join .icon {
		font-size: 2.6rem;
	}
	.join h2 {
		margin: 0;
		font-size: 1.4rem;
	}
	.lede {
		color: #666;
		font-size: 0.88rem;
		line-height: 1.55;
		margin: 0 0 0.6rem;
	}
	.join form {
		display: flex;
		gap: 0.5rem;
		width: 100%;
		max-width: 340px;
	}
	.code {
		flex: 1;
		font: inherit;
		font-size: 1rem;
		letter-spacing: 0.12em;
		text-transform: uppercase;
		text-align: center;
		padding: 0.6rem 0.8rem;
		border: 1px solid #d0d0d0;
		border-radius: 8px;
	}
	.code:focus {
		outline: none;
		border-color: #2a6df6;
	}
	.joinbtn {
		border: none;
		background: #2a6df6;
		color: #fff;
		border-radius: 8px;
		padding: 0.6rem 1.2rem;
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
	.joinbtn:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.err {
		color: #c33;
		font-size: 0.84rem;
		margin: 0.3rem 0 0;
	}

	.np {
		display: flex;
		gap: 1rem;
		align-items: center;
		background: #f6f8fc;
		border-radius: 14px;
		padding: 1rem;
		margin-bottom: 1rem;
	}
	.np :global(img),
	.np :global(.placeholder) {
		border-radius: 8px;
		flex-shrink: 0;
	}
	.npmeta {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
		min-width: 0;
		flex: 1;
	}
	.t {
		font-size: 1.05rem;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.a {
		font-size: 0.86rem;
		color: #555;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.roomcode {
		font-size: 0.74rem;
		color: #888;
		letter-spacing: 0.04em;
		margin-top: 0.15rem;
	}
	.bar {
		height: 4px;
		background: #d8e0ee;
		border-radius: 2px;
		overflow: hidden;
		margin-top: 0.45rem;
	}
	.fill {
		height: 100%;
		background: #2a6df6;
		transition: width 0.9s linear;
	}
	.time {
		font-size: 0.7rem;
		color: #888;
		font-variant-numeric: tabular-nums;
		margin-top: 0.2rem;
	}
	.emptynp {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.4rem;
		color: #888;
		padding: 1.4rem;
		text-align: center;
	}
	.emptynp .icon {
		font-size: 1.8rem;
	}
	.emptynp p {
		margin: 0;
		font-size: 0.86rem;
	}

	.transport {
		display: flex;
		gap: 0.8rem;
		justify-content: center;
		margin-bottom: 1.4rem;
	}
	.tr {
		width: 56px;
		height: 56px;
		border-radius: 50%;
		border: none;
		background: #2a6df6;
		color: #fff;
		font-size: 1.2rem;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.tr:hover {
		background: #1a5ae0;
	}
	.tr:disabled {
		opacity: 0.35;
		cursor: default;
	}

	.sec {
		margin-bottom: 1.4rem;
	}
	.sec h3 {
		font-size: 0.8rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: #666;
		margin: 0 0 0.5rem;
	}
	.qlist,
	.mlist {
		list-style: none;
		padding: 0;
		margin: 0;
	}
	.qrow {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		padding: 0.3rem 0;
		border-bottom: 1px solid #f0f0f0;
	}
	.num {
		font-size: 0.78rem;
		color: #999;
		font-variant-numeric: tabular-nums;
		min-width: 1.2rem;
		text-align: right;
	}
	.qrow :global(img),
	.qrow :global(.placeholder) {
		border-radius: 4px;
		flex-shrink: 0;
	}
	.qi {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
	}
	.qt {
		font-size: 0.86rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.qa {
		font-size: 0.74rem;
		color: #777;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.mrow {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.3rem 0;
		font-size: 0.86rem;
	}
	.person {
		font-size: 0.8rem;
		opacity: 0.6;
	}
	.muted {
		color: #888;
		font-size: 0.84rem;
		line-height: 1.5;
	}
	.leave {
		border: 1px solid #e0caca;
		background: #fff5f5;
		color: #c33;
		border-radius: 999px;
		padding: 0.5rem 1.4rem;
		font: inherit;
		font-size: 0.85rem;
		cursor: pointer;
	}
	.leave:hover {
		background: #ffeaea;
	}
</style>
