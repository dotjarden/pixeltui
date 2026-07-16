<script lang="ts">
	// Host settings editor for `~/.pixeltui/config.json`. The desktop IS the
	// server, so these edits control the embedded Go sidecar. Saving restarts the
	// sidecar so the Go binary re-reads the file.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		config,
		configLoaded,
		loadConfig,
		saveConfig,
		restartSidecar,
		normalizeAddr,
		cloneConfig
	} from '$lib/stores/settings';
	import { rePair } from '$lib/server';
	import { downloadLikedOn, setDownloadLiked } from '$lib/stores/downloads';
	import { setCrossfade, CROSSFADE_PRESETS } from '$lib/stores/player';
	import type { AppConfig } from '$lib/stores/settings';
	import Icon from './Icon.svelte';
	import ServerAccess from './ServerAccess.svelte';

	let saving = $state(false);
	let saved = $state(false);
	let message = $state('');
	let form = $state<AppConfig | null>(null);
	let pairingOpen = $state(false);

	onMount(() => {
		if (!$configLoaded) {
			void loadConfig().then((c) => (form = cloneConfig(c)));
		} else if ($config) {
			form = cloneConfig($config);
		}
	});

	function addLocalDir() {
		if (!form) return;
		form.local_dirs = [...form.local_dirs, ''];
	}
	function removeLocalDir(i: number) {
		if (!form) return;
		form.local_dirs = form.local_dirs.filter((_, j) => j !== i);
	}
	function updateLocalDir(i: number, v: string) {
		if (!form) return;
		form.local_dirs = form.local_dirs.map((d, j) => (j === i ? v : d));
	}

	async function submit() {
		if (!form) return;
		saving = true;
		saved = false;
		message = '';
		try {
			form.server.addr = normalizeAddr(form.server.addr);
			await saveConfig(cloneConfig(form));
			setDownloadLiked($downloadLikedOn);
			saved = true;
			message = 'Settings saved. Sidecar restarted.';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Failed to save settings';
		} finally {
			saving = false;
		}
	}

	function openParty() {
		void goto('/?view=party');
	}

	function applyServerConfig(next: AppConfig) {
		form = cloneConfig(next);
		saved = true;
		message = 'Server access updated. The sidecar restarted.';
	}
</script>

{#if form}
	{#if pairingOpen}
		<ServerAccess config={form} onClose={() => (pairingOpen = false)} onApplied={applyServerConfig} />
	{:else}
	<div class="settings">
		<h2>Settings</h2>

		<section>
			<h3>Server</h3>
			<button type="button" class="server-access" onclick={() => (pairingOpen = true)}>
				<span class="server-access-icon"><Icon name="device" size={17} /></span>
				<span><strong>Pair a device</strong><small>Show the server QR, manage devices, and choose remote access.</small></span>
				<Icon name="next" size={16} />
			</button>
			<label>
				<span>Bind address</span>
				<input bind:value={form.server.addr} placeholder="127.0.0.1:8790" />
			</label>
			<label>
				<span>Advertised name</span>
				<input bind:value={form.server.name} placeholder="PixelPal Desktop" />
			</label>
			<label>
				<span>Tunnel</span>
				<select bind:value={form.server.tunnel}>
					<option value="">LAN only</option>
					<option value="cloudflare">Cloudflare</option>
					<option value="ngrok">ngrok</option>
					<option value="tailscale">Tailscale</option>
				</select>
			</label>
			<label>
				<span>Public URL</span>
				<input bind:value={form.server.public_url} placeholder="https://…" />
			</label>
		</section>

		<section>
			<h3>Sources</h3>
			<p class="dir-label">Local directories</p>
			{#each form.local_dirs as dir, i (i)}
				<div class="dir-row">
					<input value={dir} oninput={(e) => updateLocalDir(i, e.currentTarget.value)} />
						<button type="button" onclick={() => removeLocalDir(i)} aria-label="Remove folder"><Icon name="close" size={14} /></button>
				</div>
			{/each}
			<button type="button" class="add" onclick={addLocalDir}><Icon name="plus" size={14}/> Add directory</button>

			<details class="subsec">
				<summary>Subsonic</summary>
				<label>
					<span>URL</span>
					<input bind:value={form.subsonic.url} placeholder="https://…" />
				</label>
				<label>
					<span>Username</span>
					<input bind:value={form.subsonic.user} />
				</label>
				<label>
					<span>Password</span>
					<input type="password" bind:value={form.subsonic.pass} />
				</label>
			</details>
		</section>

		<section>
			<h3>Scrobbling</h3>
			<label class="inline">
				<input type="checkbox" bind:checked={form.scrobble.enabled} />
				<span>Enable scrobbling</span>
			</label>
			<label>
				<span>Last.fm API key</span>
				<input bind:value={form.lastfm_key} />
			</label>
			<details class="subsec">
				<summary>Advanced credentials</summary>
				<label>
					<span>Last.fm secret</span>
					<input type="password" bind:value={form.scrobble.lastfm_secret} />
				</label>
				<label>
					<span>Last.fm session</span>
					<input type="password" bind:value={form.scrobble.lastfm_session} />
				</label>
				<label>
					<span>Last.fm username</span>
					<input bind:value={form.scrobble.lastfm_user} />
				</label>
				<label>
					<span>ListenBrainz token</span>
					<input type="password" bind:value={form.scrobble.listenbrainz_token} />
				</label>
			</details>
		</section>

		<section>
			<h3>Playback</h3>
			<label class="inline">
				<input type="checkbox" bind:checked={form.autoplay} />
				<span>Autoplay top-up</span>
			</label>
			<label>
				<span>Crossfade</span>
				<select value={0} onchange={(e) => setCrossfade(Number(e.currentTarget.value))}>
					{#each CROSSFADE_PRESETS as s (s)}
						<option value={s}>{s === 0 ? 'Off' : `${s}s`}</option>
					{/each}
				</select>
			</label>
			<label>
				<span>Seek step (seconds)</span>
				<input type="number" bind:value={form.seek_step} min="1" max="60" />
			</label>
			<label>
				<span>Explore depth (0–10)</span>
				<input type="number" bind:value={form.explore} min="0" max="10" />
			</label>
		</section>

		<section>
			<h3>Storage</h3>
			<label>
				<span>Download directory</span>
				<input bind:value={form.download_dir} placeholder="~/.pixeltui/downloads" />
			</label>
			<label class="inline">
				<input type="checkbox" checked={$downloadLikedOn} onchange={(e) => setDownloadLiked(e.currentTarget.checked)} />
				<span>Download liked songs</span>
			</label>
		</section>

		<section>
			<h3>Appearance</h3>
			<label>
				<span>Theme</span>
				<input bind:value={form.theme} placeholder="default" />
			</label>
		</section>

		<section class="experimental">
			<div class="section-heading">
				<div>
					<h3>Experimental</h3>
					<p>Features still being shaped for desktop.</p>
				</div>
				<span class="badge">Beta</span>
			</div>
			<button type="button" class="party-entry" onclick={openParty}>
				<span class="party-icon"><Icon name="users" size={17} /></span>
				<span><strong>Listening party</strong><small>Control a shared room without leaving your music.</small></span>
				<Icon name="next" size={16} />
			</button>
		</section>

		{#if message}
			<p class="message" class:error={!saved}>{message}</p>
		{/if}

		<div class="actions">
			<button class="primary" onclick={submit} disabled={saving}>
				{saving ? 'Saving…' : 'Save & Restart'}
			</button>
			<button type="button" onclick={() => restartSidecar()} disabled={saving}>
				Restart sidecar
			</button>
			<button type="button" onclick={rePair}>Re-pair</button>
		</div>
		</div>
	{/if}
{:else}
	<p class="loading">Loading settings…</p>
{/if}

<style>
	.settings {
		max-width: 620px;
		padding-bottom: 2rem;
	}
	h2 {
		margin: 0 0 1rem;
		font-size: 1.3rem;
	}
	section {
		background: #f9fafc;
		border-radius: 10px;
		padding: 1rem;
		margin-bottom: 1rem;
	}
	section h3 {
		margin: 0 0 0.8rem;
		font-size: 0.86rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: #666;
	}
	.server-access { display: flex; align-items: center; gap: .7rem; width: 100%; margin: 0 0 1rem; padding: .7rem; border: 1px solid #e5e0db; border-radius: 10px; background: rgba(255,255,255,.6); text-align: left; cursor: pointer; }
	.server-access:hover { background: #fff; border-color: #d8d0c8; }
	.server-access > span:nth-child(2) { display: flex; flex: 1; flex-direction: column; gap: .15rem; }
	.server-access strong { font-size: .84rem; }.server-access small { color: #777; font-size: .72rem; }.server-access-icon { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 8px; background: #edf2f7; color: #546a83; }
	.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
	.section-heading p { margin: -.45rem 0 .8rem; font-size: .78rem; color: #777; }
	.badge { color: #a35a2d; background: #fff0df; border-radius: 999px; padding: .18rem .48rem; font-size: .68rem; font-weight: 700; }
	.party-entry { display: flex; align-items: center; gap: .7rem; width: 100%; padding: .7rem; border: 1px solid #e5e0db; border-radius: 10px; background: rgba(255,255,255,.6); text-align: left; cursor: pointer; }
	.party-entry:hover { background: #fff; border-color: #d8d0c8; }
	.party-entry > span:nth-child(2) { display: flex; flex-direction: column; gap: .15rem; flex: 1; }
	.party-entry strong { font-size: .84rem; }
	.party-entry small { color: #777; font-size: .72rem; }
	.party-icon { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 8px; background: #f6e9e2; color: #a35a2d; }
	label {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin-bottom: 0.7rem;
		font-size: 0.84rem;
		color: #444;
	}
	label.inline {
		flex-direction: row;
		align-items: center;
		gap: 0.5rem;
	}
	label span:first-child {
		font-weight: 500;
	}
	input,
	select {
		font: inherit;
		padding: 0.45rem 0.6rem;
		border: 1px solid #d5d5d5;
		border-radius: 6px;
		background: #fff;
	}
	input[type='checkbox'] {
		width: 16px;
		height: 16px;
	}
	.dir-row {
		display: flex;
		gap: 0.4rem;
		margin-bottom: 0.4rem;
	}
	.dir-row input {
		flex: 1;
	}
	.dir-label {
		font-size: 0.84rem;
		font-weight: 500;
		color: #444;
		margin: 0 0 0.25rem;
	}
	.dir-row button {
		border: none;
		background: transparent;
		color: #999;
		cursor: pointer;
		font-size: 0.85rem;
	}
	.add {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		border: 1px dashed #bbb;
		background: transparent;
		border-radius: 6px;
		padding: 0.35rem 0.8rem;
		font: inherit;
		font-size: 0.8rem;
		cursor: pointer;
		color: #555;
	}
	.subsec {
		margin-top: 0.8rem;
		padding-top: 0.6rem;
		border-top: 1px solid #eee;
	}
	.subsec summary {
		cursor: pointer;
		font-size: 0.82rem;
		color: #2a6df6;
	}
	.actions {
		display: flex;
		gap: 0.6rem;
		margin-top: 1rem;
	}
	.actions button {
		border: 1px solid #d0d0d0;
		background: #fff;
		border-radius: 6px;
		padding: 0.45rem 1rem;
		font: inherit;
		font-size: 0.84rem;
		cursor: pointer;
	}
	.actions button.primary {
		background: #2a6df6;
		border-color: #2a6df6;
		color: #fff;
		font-weight: 600;
	}
	.actions button:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.message {
		font-size: 0.84rem;
		margin: 0.6rem 0;
	}
	.message.error {
		color: #c33;
	}
	.loading {
		color: #888;
	}
</style>
