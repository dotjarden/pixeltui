// Boot/connection store: drives the embedded-server lifecycle UI in Phase 0.
// Later phases split into stores/server.ts + lib/sse + lib/offline, mirroring
// iOS ServerStore.swift.

import { writable, get } from 'svelte/store';
import { invoke } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';

export type BootStatus = 'starting' | 'health' | 'pairing' | 'ready' | 'error';

export const status = writable<BootStatus>('starting');
export const token = writable<string | null>(null);
export const baseUrl = writable<string>('');
export const health = writable<unknown>(null);
export const logs = writable<string[]>([]);

const unlisteners: UnlistenFn[] = [];

function pushLog(kind: string, line: string) {
	logs.update((l) => [...l.slice(-300), `[${kind}] ${line.trimEnd()}`]);
}

export async function boot() {
	baseUrl.set(await invoke<string>('get_server_base'));

	unlisteners.push(
		await listen('sidecar://stdout', (e) => pushLog('out', String(e.payload)))
	);
	unlisteners.push(
		await listen('sidecar://stderr', (e) => pushLog('err', String(e.payload)))
	);
	unlisteners.push(await listen('sidecar://terminated', () => pushLog('evt', 'sidecar terminated')));
	unlisteners.push(await listen('app://health', () => status.set('health')));
	unlisteners.push(await listen('app://status', (e) => {
		if (e.payload === 'pairing') status.set('pairing');
	}));
	unlisteners.push(
		await listen('app://error', (e) => {
			status.set('error');
			pushLog('err', String(e.payload));
		})
	);
	unlisteners.push(
		await listen('app://ready', (e) => {
			token.set(String(e.payload));
			status.set('ready');
		})
	);

	// Subsequent launches may reuse a token already in Rust state.
	try {
		const t = await invoke<string | null>('get_token');
		if (t) {
			token.set(t);
			status.set('ready');
		}
	} catch {
		/* ignore */
	}
}

export async function fetchHealth() {
	const base = get(baseUrl);
	const t = get(token);
	if (!base) return;
	const res = await fetch(`${base}/health`, t ? { headers: { Authorization: `Bearer ${t}` } } : undefined);
	health.set(await res.json());
}

export async function rePair() {
	status.set('starting');
	await invoke('re_pair');
}