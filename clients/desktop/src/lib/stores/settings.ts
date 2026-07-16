// Settings store: read/write `~/.pixeltui/config.json` via Rust commands.
// The desktop IS the host, so these settings drive the embedded Go server.
// Mirrors the Rust `config::Config` struct (from `src-tauri/src/config.rs`).

import { get, writable } from 'svelte/store';
import { invoke } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';
import { downloadLikedOn, setDownloadLiked } from '$lib/stores/downloads';
import { setAutoplay, setCrossfade } from '$lib/stores/player';

export interface SubsonicConfig {
	url: string;
	user: string;
	pass: string;
}

export interface ChartsConfig {
	global: boolean;
	country: string;
}

export interface ScrobbleConfig {
	enabled: boolean;
	lastfm_secret: string;
	lastfm_session: string;
	lastfm_user: string;
	listenbrainz_token: string;
}

export interface ServerConfig {
	addr: string;
	name: string;
	public_url: string;
	tunnel: string;
}

export interface AppConfig {
	lastfm_key: string;
	scrobble: ScrobbleConfig;
	subsonic: SubsonicConfig;
	local_dirs: string[];
	download_dir: string;
	theme: string;
	explore: number;
	autoplay: boolean;
	seek_step: number;
	charts: ChartsConfig;
	server: ServerConfig;
	acoustid_api_key: string;
	audio_device: string;
}

/** Config is JSON-only. Svelte 5 state values are proxies, which cannot be
 * passed to `structuredClone`; serialize to a plain DTO before persisting or
 * handing a draft between components. */
export function cloneConfig(cfg: AppConfig): AppConfig {
	return JSON.parse(JSON.stringify(cfg)) as AppConfig;
}

export const config = writable<AppConfig | null>(null);
export const configLoaded = writable(false);
export const onboardingComplete = writable(false);
export const provisioning = writable(false);
export const provisionOutput = writable<string[]>([]);
export const provisionDone = writable(false);

const DEFAULTS: AppConfig = {
	lastfm_key: '',
	scrobble: {
		enabled: false,
		lastfm_secret: '',
		lastfm_session: '',
		lastfm_user: '',
		listenbrainz_token: ''
	},
	subsonic: { url: '', user: '', pass: '' },
	local_dirs: [],
	download_dir: '',
	theme: '',
	explore: 5,
	autoplay: true,
	seek_step: 10,
	charts: { global: true, country: '' },
	server: { addr: '127.0.0.1:8790', name: '', public_url: '', tunnel: '' },
	acoustid_api_key: '',
	audio_device: ''
};

let provisionUnlisten: UnlistenFn[] = [];

/** Load config from Rust. */
export async function loadConfig(): Promise<AppConfig> {
	const cfg = await invoke<AppConfig>('get_config');
	// Merge defaults so missing fields don't become null/undefined.
	const merged = { ...DEFAULTS, ...cfg };
	merged.scrobble = { ...DEFAULTS.scrobble, ...cfg.scrobble };
	merged.subsonic = { ...DEFAULTS.subsonic, ...cfg.subsonic };
	merged.charts = { ...DEFAULTS.charts, ...cfg.charts };
	merged.server = { ...DEFAULTS.server, ...cfg.server };
	if (!merged.local_dirs) merged.local_dirs = [];
	config.set(merged);
	configLoaded.set(true);
	// Sync local playback settings from the host config.
	setAutoplay(merged.autoplay);
	setCrossfade(0); // crossfade is local-only in the desktop player; reset to 0
	setDownloadLiked(false); // will be toggled below if needed
	if (merged.download_dir) {
		// download-liked flag is stored separately in localStorage for now.
		const flag = localStorage.getItem('pixeltui.downloadLiked.v1');
		setDownloadLiked(flag === '1');
	}
	return merged;
}

/** Save config to disk and restart the sidecar so the Go server picks it up. */
export async function saveConfig(cfg: AppConfig): Promise<void> {
	await invoke('set_config', { cfg });
	config.set(cfg);
	setAutoplay(cfg.autoplay);
}

/** Restart sidecar without changing config. */
export async function restartSidecar(): Promise<void> {
	await invoke('restart_sidecar');
}

/** Whether the app has been configured before (config.json exists). */
export async function hasConfig(): Promise<boolean> {
	return invoke<boolean>('config_exists');
}

/** Run `pixeltui doctor --fix` and stream output to `provisionOutput`. */
export async function runProvision(): Promise<string> {
	provisioning.set(true);
	provisionOutput.set([]);
	provisionDone.set(false);

	const push = (line: string) =>
		provisionOutput.update((l) => [...l.slice(-300), line.trimEnd()]);

	provisionUnlisten.forEach((u) => u());
	provisionUnlisten = [];
	provisionUnlisten.push(await listen<string>('provision://stdout', (e) => push(String(e.payload))));
	provisionUnlisten.push(await listen<string>('provision://stderr', (e) => push(String(e.payload))));
	provisionUnlisten.push(
		await listen<string>('provision://done', () => {
			provisionDone.set(true);
			provisionUnlisten.forEach((u) => u());
			provisionUnlisten = [];
		})
	);

	try {
		const out = await invoke<string>('provision');
		return out;
	} catch (e) {
		push(String(e));
		throw e;
	} finally {
		provisioning.set(false);
	}
}

/** Sanitize the server bind address. */
export function normalizeAddr(addr: string): string {
	const a = addr.trim();
	if (!a) return '127.0.0.1:8790';
	// Accept bare port like ":8790" or "127.0.0.1:8790".
	if (/^:\d+$/.test(a)) return `127.0.0.1${a}`;
	if (/^\d+$/.test(a)) return `127.0.0.1:${a}`;
	return a;
}
