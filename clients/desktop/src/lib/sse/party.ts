// Party SSE stream on `/api/party/events?token=&code=`. The server pushes
// `PartySnapshot` updates whenever the party state changes (transport, queue,
// members). Auth is `?token=` (EventSource can't set headers), same as the main
// `/events` stream. Native EventSource auto-reconnects on transient errors.
//
// The snapshot event name is whatever the server emits; this consumer is
// deliberately lenient — it listens on the default `message` channel and any
// named event, parses the payload as JSON, and forwards anything that looks like
// a snapshot (has a `code` field). Stale snapshots (rev <= last applied) are
// dropped by the store, not here.

import { get } from 'svelte/store';
import { writable } from 'svelte/store';
import { baseUrl, token } from '$lib/server';
import type { PartySnapshot } from '$lib/api/types';

export const partySseConnected = writable(false);

export type SnapshotHandler = (snap: PartySnapshot) => void;

export class PartyStream {
	private es: EventSource | null = null;
	private code = '';
	private handler: SnapshotHandler | null = null;
	private stopped = true;

	onSnapshot(h: SnapshotHandler) {
		this.handler = h;
	}

	start(code: string) {
		this.code = code;
		if (!this.stopped) return;
		this.stopped = false;
		this.open();
	}

	private open() {
		const base = get(baseUrl);
		const t = get(token);
		if (!base || !t || !this.code) return;
		const es = new EventSource(
			`${base}/api/party/events?token=${encodeURIComponent(t)}&code=${encodeURIComponent(this.code)}`
		);
		this.es = es;
		// Default message channel + any named event the server may use.
		es.addEventListener('message', (e) => this.handle((e as MessageEvent).data));
		es.addEventListener('snapshot', (e) => this.handle((e as MessageEvent).data));
		es.addEventListener('party', (e) => this.handle((e as MessageEvent).data));
		es.onopen = () => partySseConnected.set(true);
		es.onerror = () => {
			partySseConnected.set(false);
			// native EventSource auto-reconnects
		};
	}

	private handle(raw: string) {
		if (!this.handler) return;
		try {
			const obj = JSON.parse(raw);
			if (obj && typeof obj === 'object' && 'code' in obj) {
				this.handler(obj as PartySnapshot);
			}
		} catch {
			/* not JSON — ignore */
		}
	}

	stop() {
		this.stopped = true;
		this.es?.close();
		this.es = null;
		partySseConnected.set(false);
	}

	restart() {
		this.es?.close();
		this.es = null;
		if (!this.stopped) this.open();
	}
}

export const partyStream = new PartyStream();