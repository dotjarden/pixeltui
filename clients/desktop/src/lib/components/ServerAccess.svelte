<script lang="ts">
	// Host-side pairing and sharing control surface. This uses the same live
	// single-use invitation the server prints for `pixeltui serve`, so a phone
	// scanning here follows the exact iOS pairing path.
	import { onMount } from 'svelte';
	import { listen, type UnlistenFn } from '@tauri-apps/api/event';
	import { api, ApiError, mediaUrl } from '$lib/api/client';
	import { cloneConfig, restartSidecar, saveConfig } from '$lib/stores/settings';
	import { status } from '$lib/server';
	import type { Device, PairingInfo } from '$lib/api/types';
	import type { AppConfig } from '$lib/stores/settings';
	import Icon from './Icon.svelte';

	let { config, onClose, onApplied }: {
		config: AppConfig;
		onClose: () => void;
		onApplied: (config: AppConfig) => void;
	} = $props();

	function blankConfig(): AppConfig {
		return {
			lastfm_key: '', scrobble: { enabled: false, lastfm_secret: '', lastfm_session: '', lastfm_user: '', listenbrainz_token: '' },
			subsonic: { url: '', user: '', pass: '' }, local_dirs: [], download_dir: '', theme: '', explore: 5,
			autoplay: true, seek_step: 10, charts: { global: true, country: '' },
			server: { addr: '127.0.0.1:8790', name: '', public_url: '', tunnel: '' }, acoustid_api_key: '', audio_device: ''
		};
	}
	let draft = $state<AppConfig>(blankConfig());
	let pairing = $state<PairingInfo | null>(null);
	let devices = $state<Device[]>([]);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state('');
	let serverUpdateNeeded = $state(false);
	let restarting = $state(false);
	let attemptedServerUpgrade = $state(false);
	let copied = $state<'url' | 'code' | null>(null);
	let qrVersion = $state(0);
	const online = $derived($status === 'ready');
	const qrSrc = $derived(mediaUrl(`/api/pairing/qr?v=${qrVersion}`));
	const tunnelLabel = $derived(
		draft.server.tunnel === 'tailscale' ? 'Tailscale private access' :
		draft.server.tunnel === 'cloudflare' ? 'Cloudflare quick tunnel' :
		draft.server.tunnel === 'ngrok' ? 'ngrok public tunnel' :
		draft.server.public_url ? 'Custom public address' : 'Local network only'
	);
	$effect(() => {
		draft = cloneConfig(config);
	});

	async function refresh() {
		loading = true;
		error = '';
		try {
			const [nextPairing, nextDevices] = await Promise.all([api.pairing(), api.devices()]);
			pairing = nextPairing;
			devices = nextDevices;
			qrVersion += 1;
			serverUpdateNeeded = false;
		} catch (e) {
			serverUpdateNeeded = e instanceof ApiError && e.status === 404;
			error = serverUpdateNeeded
				? 'This running server needs a quick restart to enable its pairing screen.'
				: e instanceof Error ? e.message : 'Could not reach the server.';
			if (serverUpdateNeeded && !attemptedServerUpgrade) {
				attemptedServerUpgrade = true;
				void restartAndRetry();
			}
		} finally {
			loading = false;
		}
	}

	async function restartAndRetry() {
		restarting = true;
		error = '';
		try {
			await restartSidecar();
			await new Promise((resolve) => setTimeout(resolve, 1200));
			await refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not restart the server.';
		} finally {
			restarting = false;
		}
	}

	async function applySharing() {
		saving = true;
		error = '';
		try {
			await saveConfig(cloneConfig(draft));
			onApplied(cloneConfig(draft));
			// The sidecar restarts to bring a tunnel up or down. Its ready event
			// follows shortly after; refresh is also available immediately below.
			setTimeout(() => void refresh(), 900);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not update server access.';
		} finally {
			saving = false;
		}
	}

	async function copy(value: string, kind: 'url' | 'code') {
		try {
			await navigator.clipboard.writeText(value);
			copied = kind;
			setTimeout(() => { if (copied === kind) copied = null; }, 1600);
		} catch {
			error = 'Could not copy to the clipboard.';
		}
	}

	function copyPairing(kind: 'url' | 'code') {
		if (!pairing) return;
		void copy(kind === 'url' ? pairing.url : pairing.code, kind);
	}

	async function revoke(device: Device) {
		if (!confirm(`Remove ${device.name}'s access to this server?`)) return;
		try {
			await api.revoke(device.id);
			await refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not remove this device.';
		}
	}

	onMount(() => {
		void refresh();
		let unlisten: UnlistenFn | undefined;
		void listen<string>('sidecar://stdout', (event) => {
			const line = String(event.payload);
			if (line.includes('Code:') || line.includes('tunnel up:') || line.includes('Tunnel re-established')) void refresh();
		}).then((stop) => (unlisten = stop));
		return () => unlisten?.();
	});
</script>

<div class="access-page">
	<header class="access-nav">
		<button class="back" onclick={onClose}><Icon name="back" size={17} /> Settings</button>
		<div class:online class="host-state"><span></span>{online ? 'Server online' : 'Connecting…'}</div>
	</header>

	<section class="pairing-hero">
		<div class="qr-side" class:loading>
			{#if loading}
				<div class="qr-placeholder" aria-label="Loading pairing code"><Icon name="refresh" size={22} /></div>
			{:else if pairing}
				<img src={qrSrc} alt="QR code for pairing a device with {pairing.url}" />
			{:else}
				<div class="qr-placeholder"><Icon name="link" size={22} /></div>
			{/if}
		</div>
		<div class="pair-copy">
			<p class="eyebrow">SERVER ACCESS</p>
			<h2>Connect a device.</h2>
			<p class="lede">Open PixelPal on your phone and scan this code. It carries the server address and a one-time pairing code, just like <code>pixeltui serve</code>.</p>

			{#if pairing}
				<div class="invite-values">
					<div class="invite-value">
						<span>Server address</span>
						<code title={pairing.url}>{pairing.url}</code>
						<button onclick={() => copyPairing('url')} aria-label="Copy server address"><Icon name={copied === 'url' ? 'check' : 'copy'} size={15} /></button>
					</div>
					<div class="invite-value code-value">
						<span>Pairing code</span>
						<strong>{pairing.code}</strong>
						<button onclick={() => copyPairing('code')} aria-label="Copy pairing code"><Icon name={copied === 'code' ? 'check' : 'copy'} size={15} /></button>
					</div>
				</div>
			{/if}
		</div>
	</section>

	<section class="sharing-card">
		<div class="sharing-heading">
			<div>
				<p class="eyebrow">REACHABILITY</p>
				<h3>How devices reach this server</h3>
				<p>{tunnelLabel}. A pairing remains valid when a quick tunnel changes address.</p>
			</div>
			<button class="refresh" onclick={() => void refresh()} disabled={loading} aria-label="Refresh pairing information"><Icon name="refresh" size={16} /></button>
		</div>

		<div class="access-options">
			<label class:chosen={!draft.server.tunnel && !draft.server.public_url}>
				<input type="radio" name="tunnel" checked={!draft.server.tunnel && !draft.server.public_url} onchange={() => { draft.server.tunnel = ''; draft.server.public_url = ''; }} />
				<span><strong>Local network</strong><small>Devices on the same Wi-Fi can connect.</small></span>
			</label>
			<label class:chosen={draft.server.tunnel === 'tailscale'}>
				<input type="radio" name="tunnel" checked={draft.server.tunnel === 'tailscale'} onchange={() => { draft.server.tunnel = 'tailscale'; draft.server.public_url = ''; }} />
				<span><strong>Tailscale</strong><small>Private access across your tailnet.</small></span><em>Recommended</em>
			</label>
			<label class:chosen={draft.server.tunnel === 'cloudflare'}>
				<input type="radio" name="tunnel" checked={draft.server.tunnel === 'cloudflare'} onchange={() => { draft.server.tunnel = 'cloudflare'; draft.server.public_url = ''; }} />
				<span><strong>Cloudflare quick tunnel</strong><small>Public link, no account required.</small></span>
			</label>
			<label class:chosen={draft.server.tunnel === 'ngrok'}>
				<input type="radio" name="tunnel" checked={draft.server.tunnel === 'ngrok'} onchange={() => { draft.server.tunnel = 'ngrok'; draft.server.public_url = ''; }} />
				<span><strong>ngrok</strong><small>Public link using your ngrok setup.</small></span>
			</label>
			<label class="custom-url" class:chosen={!!draft.server.public_url}>
				<input type="radio" name="tunnel" checked={!!draft.server.public_url} onchange={() => { draft.server.tunnel = ''; }} />
				<span><strong>Use my own public URL</strong><small>For a tunnel or domain you already manage.</small></span>
				<input bind:value={draft.server.public_url} placeholder="https://music.example.com" onfocus={() => { draft.server.tunnel = ''; }} />
			</label>
		</div>
		<div class="sharing-actions"><button class="apply" onclick={applySharing} disabled={saving}>{saving ? 'Updating server…' : 'Apply server access'}</button></div>
	</section>

	<section class="devices-card">
		<div class="devices-heading"><div><p class="eyebrow">PAIRED DEVICES</p><h3>{devices.length ? `${devices.length} device${devices.length === 1 ? '' : 's'} connected` : 'No devices paired yet'}</h3></div></div>
		{#if devices.length}
			<div class="device-list">
				{#each devices as device (device.id)}
					<div class="device-row">
						<span class="device-icon"><Icon name="device" size={17} /></span>
						<span><strong>{device.name}</strong><small>{device.current ? 'This desktop' : `Last seen ${new Date(device.last_seen).toLocaleDateString()}`}</small></span>
						{#if !device.current}<button class="revoke" onclick={() => void revoke(device)}>Remove</button>{/if}
					</div>
				{/each}
			</div>
		{:else}
			<p class="device-empty">The next phone that scans this code will appear here.</p>
		{/if}
	</section>

	{#if error}
		<div class="access-error"><span>{error}</span>{#if serverUpdateNeeded}<button onclick={() => void restartAndRetry()} disabled={restarting}>{restarting ? 'Restarting…' : 'Restart server'}</button>{/if}</div>
	{/if}
</div>

<style>
	.access-page { max-width: 1000px; padding: 4px 0 3rem; color: var(--text); }
	.access-nav { display: flex; justify-content: space-between; align-items: center; margin-bottom: clamp(28px, 4vw, 48px); }
	.back, .refresh { display: inline-flex; align-items: center; gap: .35rem; border: 0; background: transparent; color: var(--muted); font: inherit; cursor: pointer; padding: .35rem 0; }
	.back:hover { color: var(--text); }
	.host-state { display: inline-flex; align-items: center; gap: .42rem; color: var(--muted); font-size: .78rem; font-weight: 650; }
	.host-state span { width: 7px; height: 7px; border-radius: 50%; background: #a9a49c; }
	.host-state.online { color: #37755d; }.host-state.online span { background: #51a67c; box-shadow: 0 0 0 4px rgba(81,166,124,.12); }
	.pairing-hero { display: grid; grid-template-columns: minmax(210px, 278px) minmax(0, 1fr); align-items: center; gap: clamp(30px, 6vw, 76px); margin-bottom: clamp(42px, 6vw, 72px); }
	.qr-side { display: grid; place-items: center; aspect-ratio: 1; padding: 18px; border-radius: 28px; background: #fff; box-shadow: 0 20px 46px rgba(42,37,31,.13), 0 2px 8px rgba(42,37,31,.08); }
	.qr-side img { display: block; width: 100%; height: 100%; image-rendering: pixelated; }
	.qr-placeholder { display: grid; place-items: center; width: 100%; height: 100%; border-radius: 17px; background: var(--surface-strong); color: var(--muted); animation: pulse 1.2s ease-in-out infinite alternate; }
	@keyframes pulse { to { opacity: .55; } }
	.eyebrow { margin: 0 0 .4rem; color: var(--accent-deep); font-size: .67rem; font-weight: 800; letter-spacing: .13em; }
	.pair-copy h2 { margin: 0; font-size: clamp(2.15rem, 4vw, 3.8rem); letter-spacing: -.07em; line-height: .94; font-weight: 780; }
	.lede { max-width: 540px; margin: 1rem 0 1.45rem; color: var(--muted); font-size: 1rem; line-height: 1.55; }.lede code { font: inherit; color: var(--text); }
	.invite-values { display: grid; gap: 8px; max-width: 540px; }.invite-value { display: grid; grid-template-columns: 102px minmax(0,1fr) 34px; align-items: center; gap: 10px; min-height: 44px; padding: 0 8px 0 12px; border: 1px solid var(--line); border-radius: 12px; background: rgba(255,255,255,.56); }.invite-value span { color: var(--muted); font-size: .72rem; font-weight: 680; }.invite-value code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); font-size: .78rem; }.invite-value strong { letter-spacing: .16em; font-size: .92rem; }.invite-value button { display: grid; place-items: center; width: 30px; height: 30px; border: 0; border-radius: 8px; background: transparent; color: var(--muted); cursor: pointer; }.invite-value button:hover { background: var(--surface-strong); color: var(--text); }
	.sharing-card, .devices-card { border: 1px solid var(--line); border-radius: 22px; background: rgba(255,255,255,.57); padding: clamp(20px, 3vw, 30px); box-shadow: 0 10px 30px rgba(47,42,37,.045); }.sharing-card { margin-bottom: 16px; }.sharing-heading, .devices-heading { display: flex; justify-content: space-between; gap: 20px; }.sharing-heading h3, .devices-heading h3 { margin: 0; font-size: 1.05rem; letter-spacing: -.035em; }.sharing-heading p:not(.eyebrow) { margin: .45rem 0 1.25rem; color: var(--muted); font-size: .84rem; }.refresh { display: grid; place-items: center; width: 32px; height: 32px; padding: 0; border-radius: 50%; }.refresh:hover { background: var(--surface-strong); color: var(--text); }
	.access-options { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 10px; }.access-options label { position: relative; display: flex; align-items: center; gap: 10px; min-height: 76px; padding: 13px 14px; border: 1px solid var(--line); border-radius: 14px; cursor: pointer; transition: border-color .16s ease, background .16s ease, box-shadow .16s ease; }.access-options label:hover { border-color: color-mix(in srgb, var(--accent) 45%, var(--line)); }.access-options label.chosen { border-color: color-mix(in srgb, var(--accent) 60%, var(--line)); background: color-mix(in srgb, var(--accent) 7%, white); box-shadow: 0 3px 10px color-mix(in srgb, var(--accent) 10%, transparent); }.access-options input[type="radio"] { accent-color: var(--accent); flex: 0 0 auto; }.access-options label > span { display: grid; gap: 3px; min-width: 0; }.access-options strong { font-size: .83rem; }.access-options small { color: var(--muted); font-size: .71rem; line-height: 1.32; }.access-options em { position: absolute; right: 10px; top: 10px; color: var(--accent-deep); font-size: .61rem; font-style: normal; font-weight: 760; }.access-options .custom-url { grid-column: 1 / -1; align-items: center; }.custom-url > input:last-child { flex: 1; min-width: 0; padding: .55rem .65rem; border: 1px solid var(--line); border-radius: 9px; background: rgba(255,255,255,.75); font: inherit; font-size: .78rem; color: var(--text); }.sharing-actions { display: flex; justify-content: flex-end; margin-top: 16px; }.apply { border: 0; border-radius: 999px; padding: .68rem 1.05rem; background: var(--accent); color: #fff; font: inherit; font-size: .82rem; font-weight: 700; cursor: pointer; box-shadow: 0 7px 16px color-mix(in srgb, var(--accent) 22%, transparent); }.apply:disabled,.refresh:disabled { opacity: .55; cursor: default; }
	.device-list { margin-top: 15px; }.device-row { display: flex; align-items: center; gap: 10px; min-height: 54px; padding: 8px 0; border-top: 1px solid var(--line); }.device-row > span:nth-child(2) { display: grid; gap: 2px; min-width: 0; flex: 1; }.device-row strong { font-size: .83rem; }.device-row small { color: var(--muted); font-size: .72rem; }.device-icon { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 10px; background: var(--surface-strong); color: var(--accent-deep); }.revoke { border: 0; border-radius: 8px; padding: .38rem .55rem; background: transparent; color: #9c4b43; font: inherit; font-size: .74rem; cursor: pointer; }.revoke:hover { background: #fff0ed; }.device-empty { margin: 15px 0 0; color: var(--muted); font-size: .83rem; }.access-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 14px 0 0; color: #a9473e; font-size: .82rem; }.access-error button { flex: 0 0 auto; border: 1px solid #e1b7b1; border-radius: 999px; padding: .38rem .7rem; background: #fff7f5; color: #994138; font: inherit; font-size: .74rem; font-weight: 700; cursor: pointer; }.access-error button:disabled { opacity: .6; cursor: default; }
	@media (max-width: 680px) { .pairing-hero { grid-template-columns: 166px minmax(0,1fr); gap: 22px; }.qr-side { border-radius: 21px; padding: 12px; }.pair-copy h2 { font-size: 2.25rem; }.invite-value { grid-template-columns: 1fr 32px; }.invite-value span { grid-column: 1 / -1; padding-top: 8px; }.access-options { grid-template-columns: 1fr; } }
	@media (max-width: 480px) { .pairing-hero { grid-template-columns: 1fr; }.qr-side { width: min(240px, 70vw); justify-self: center; }.access-nav { margin-bottom: 26px; } }
</style>
