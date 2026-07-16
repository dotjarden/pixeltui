// Server-Sent Events on the embedded pixeltui server's `/events` stream.
//
// Unlike iOS (which hand-parses bytes from URLSession), the Tauri webview has a
// native `EventSource` that handles `event:`/`data:` framing and auto-reconnects
// for us. Auth is `?token=` (EventSource can't set headers). The loopback URL
// never moves, so tunnel endpoint changes don't require restarting this stream
// (unlike iOS, which may connect to a remote advertised URL).

import { get } from 'svelte/store';
import { writable } from 'svelte/store';
import { baseUrl, token } from '$lib/server';

export type SseEventType = 'hello' | 'library' | 'endpoints';
export type SseHandler = (data: string) => void;

/** Connection state of the `/events` stream, for UI status indicators. */
export const sseConnected = writable(false);

/**
 * Long-lived EventSource on `/events?token=`. Add handlers via {@link on},
 * then {@link start}. Native EventSource auto-reconnects on transient errors;
 * use {@link stop}/{@link restart} for background gating and token/base changes.
 */
export class EventStream {
	private es: EventSource | null = null;
	private handlers = new Map<SseEventType, Set<SseHandler>>();
	private stopped = true;

	on(type: SseEventType, h: SseHandler): () => void {
		let set = this.handlers.get(type);
		if (!set) {
			set = new Set();
			this.handlers.set(type, set);
		}
		set.add(h);
		return () => {
			set?.delete(h);
		};
	}

	start() {
		if (!this.stopped) return;
		this.stopped = false;
		this.open();
	}

	private open() {
		const base = get(baseUrl);
		const t = get(token);
		if (!base || !t) return;
		const es = new EventSource(`${base}/events?token=${encodeURIComponent(t)}`);
		this.es = es;
		for (const type of ['hello', 'library', 'endpoints'] as SseEventType[]) {
			es.addEventListener(type, (e) => this.dispatch(type, (e as MessageEvent).data));
		}
		es.onopen = () => sseConnected.set(true);
		es.onerror = () => {
			sseConnected.set(false);
			// native EventSource auto-reconnects; no manual retry needed.
		};
	}

	private dispatch(type: SseEventType, raw: string) {
		// Server sends JSON-quoted strings ("liked", "changed", "<name>");
		// fall back to the raw string if it isn't JSON.
		let data = raw;
		try {
			data = JSON.parse(raw);
		} catch {
			/* plain string */
		}
		this.handlers.get(type)?.forEach((h) => h(data as string));
	}

	stop() {
		this.stopped = true;
		this.es?.close();
		this.es = null;
		sseConnected.set(false);
	}

	/** Re-open with the current base/token (after re-pair or base change). */
	restart() {
		this.es?.close();
		this.es = null;
		if (!this.stopped) this.open();
	}
}

export const events = new EventStream();