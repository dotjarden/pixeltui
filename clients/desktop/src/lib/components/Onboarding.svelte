<script lang="ts">
	// First-run onboarding: provision the host (yt-dlp + mpv) via
	// `pixeltui doctor --fix`, collect the minimum host settings, save config,
	// and restart the sidecar. Shown when `config.json` does not exist yet.
	import {
		provisioning,
		provisionOutput,
		provisionDone,
		onboardingComplete,
		runProvision,
		saveConfig,
		normalizeAddr,
		cloneConfig
	} from '$lib/stores/settings';
	import { downloadLikedOn, setDownloadLiked } from '$lib/stores/downloads';
	import type { AppConfig } from '$lib/stores/settings';
	import Icon from './Icon.svelte';

	let step = $state(1);
	let cfg = $state<AppConfig>({
		lastfm_key: '',
		scrobble: { enabled: false, lastfm_secret: '', lastfm_session: '', lastfm_user: '', listenbrainz_token: '' },
		subsonic: { url: '', user: '', pass: '' },
		local_dirs: [],
		download_dir: '',
		theme: '',
		explore: 5,
		autoplay: true,
		seek_step: 10,
		charts: { global: true, country: '' },
		server: { addr: '127.0.0.1:8790', name: 'PixelPal Desktop', public_url: '', tunnel: '' },
		acoustid_api_key: '',
		audio_device: ''
	});
	let saving = $state(false);
	let error = $state('');

	function addDir() {
		cfg.local_dirs = [...cfg.local_dirs, ''];
	}
	function removeDir(i: number) {
		cfg.local_dirs = cfg.local_dirs.filter((_, j) => j !== i);
	}
	function updateDir(i: number, v: string) {
		cfg.local_dirs = cfg.local_dirs.map((d, j) => (j === i ? v : d));
	}

	async function doProvision() {
		error = '';
		try {
			await runProvision();
			step = 2;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Provisioning failed';
		}
	}

	async function finish() {
		saving = true;
		error = '';
		try {
			cfg.server.addr = normalizeAddr(cfg.server.addr);
			await saveConfig(cloneConfig(cfg));
			setDownloadLiked($downloadLikedOn);
			onboardingComplete.set(true);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save settings';
		} finally {
			saving = false;
		}
	}

	function setDownloadChecked(on: boolean) {
		setDownloadLiked(on);
	}
</script>

<div class="ob">
	<div class="card">
		<div class="icon" aria-hidden="true"><Icon name="library" size={28} /></div>
		<h1>Welcome to PixelPal Desktop</h1>
		<p class="lede">This device will be the music host. Let's set it up.</p>

		{#if step === 1}
			<p class="step-title">Step 1: Install playback tools</p>
			<p class="desc">
				PixelPal needs yt-dlp and mpv on this computer. We'll run the built-in
				installer now.
			</p>
			{#if $provisionOutput.length}
				<pre class="log">{$provisionOutput.slice(-50).join('\n')}</pre>
			{/if}
			{#if error}<p class="err">{error}</p>{/if}
			<button
				class="primary"
				onclick={doProvision}
				disabled={$provisioning}
			>
				{$provisioning ? 'Installing…' : $provisionDone ? 'Continue' : 'Install yt-dlp + mpv'}
			</button>
			{#if $provisionDone}
				<button class="ghost" onclick={() => (step = 2)}>Continue to settings →</button>
			{/if}
		{:else}
			<p class="step-title">Step 2: Host settings</p>

			<label>
				<span>Server name</span>
				<input bind:value={cfg.server.name} />
			</label>
			<label>
				<span>Bind address</span>
				<input bind:value={cfg.server.addr} />
			</label>
			<label>
				<span>Last.fm API key (optional)</span>
				<input bind:value={cfg.lastfm_key} />
			</label>

			<div class="dirs">
				<span>Local music folders (optional)</span>
				{#each cfg.local_dirs as d, i (i)}
					<div class="dir-row">
						<input value={d} oninput={(e) => updateDir(i, e.currentTarget.value)} />
						<button onclick={() => removeDir(i)} aria-label="Remove folder"><Icon name="close" size={14} /></button>
					</div>
				{/each}
				<button class="add" onclick={addDir}>+ Add folder</button>
			</div>

			<label>
				<span>Download directory (optional)</span>
				<input bind:value={cfg.download_dir} placeholder="~/.pixeltui/downloads" />
			</label>

			<label class="inline">
				<input type="checkbox" checked={$downloadLikedOn} onchange={(e) => setDownloadChecked(e.currentTarget.checked)} />
				<span>Download liked songs</span>
			</label>

			{#if error}<p class="err">{error}</p>{/if}
			<button class="primary" onclick={finish} disabled={saving}>
				{saving ? 'Saving…' : 'Finish setup'}
			</button>
		{/if}
	</div>
</div>

<style>
	.ob {
		position: fixed;
		inset: 0;
		z-index: 100;
		background: #f4f6fb;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1.5rem;
	}
	.card {
		background: #fff;
		border-radius: 16px;
		padding: 2rem;
		max-width: 480px;
		width: 100%;
		box-shadow: 0 16px 50px rgba(0, 0, 0, 0.1);
	}
	.icon {
		font-size: 2.8rem;
		text-align: center;
		margin-bottom: 0.5rem;
	}
	h1 {
		margin: 0;
		text-align: center;
		font-size: 1.4rem;
	}
	.lede {
		text-align: center;
		color: #666;
		margin: 0.4rem 0 1.2rem;
	}
	.step-title {
		font-weight: 600;
		margin: 0 0 0.3rem;
	}
	.desc {
		color: #666;
		font-size: 0.86rem;
		line-height: 1.5;
		margin: 0 0 1rem;
	}
	.log {
		background: #111;
		color: #eaeaea;
		font-size: 11px;
		padding: 0.6rem;
		border-radius: 6px;
		max-height: 24vh;
		overflow: auto;
		white-space: pre-wrap;
		margin: 0 0 1rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		margin-bottom: 0.7rem;
		font-size: 0.84rem;
		color: #444;
	}
	label span {
		font-weight: 500;
	}
	label.inline {
		flex-direction: row;
		align-items: center;
		gap: 0.5rem;
	}
	input {
		font: inherit;
		padding: 0.45rem 0.6rem;
		border: 1px solid #d5d5d5;
		border-radius: 6px;
	}
	input[type='checkbox'] {
		width: 16px;
		height: 16px;
	}
	.dirs {
		font-size: 0.84rem;
		color: #444;
		margin-bottom: 0.8rem;
	}
	.dirs > span {
		font-weight: 500;
	}
	.dir-row {
		display: flex;
		gap: 0.4rem;
		margin-top: 0.4rem;
	}
	.dir-row input {
		flex: 1;
	}
	.dir-row button {
		border: none;
		background: transparent;
		color: #999;
		cursor: pointer;
		font-size: 0.85rem;
	}
	.add {
		border: 1px dashed #bbb;
		background: transparent;
		border-radius: 6px;
		padding: 0.35rem 0.8rem;
		font: inherit;
		font-size: 0.8rem;
		cursor: pointer;
		color: #555;
		margin-top: 0.4rem;
	}
	button {
		border: none;
		border-radius: 8px;
		padding: 0.55rem 1.2rem;
		font: inherit;
		font-weight: 600;
		cursor: pointer;
		margin-top: 0.5rem;
	}
	.primary {
		background: #2a6df6;
		color: #fff;
		width: 100%;
	}
	.primary:hover {
		background: #1a5ae0;
	}
	.primary:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.ghost {
		background: transparent;
		color: #2a6df6;
		width: 100%;
	}
	.err {
		color: #c33;
		font-size: 0.84rem;
		margin: 0.4rem 0;
	}
</style>
