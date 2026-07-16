<script lang="ts">
	// Overflow affordance for the viewport-level universal track menu.
	import { openTrackContextMenu, openTrackContextMenuFromAnchor } from '$lib/stores/contextMenu';
	import type { Track } from '$lib/api/types';
	import Icon from './Icon.svelte';

	let { track, context, label = 'More', onRemove }: { track: Track; context?: Track[]; label?: string; onRemove?: () => void } = $props();
</script>

<button
	class="trigger track-menu-trigger"
	title={label}
	aria-label={label}
	aria-haspopup="menu"
	onclick={(event) => { event.stopPropagation(); openTrackContextMenuFromAnchor(event.currentTarget, track, context, onRemove); }}
	oncontextmenu={(event) => openTrackContextMenu(event, track, context, onRemove)}
><Icon name="more" size={17} /></button>

<style>
	.trigger { display: grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: 8px; background: transparent; color: var(--muted); cursor: pointer; }
	.trigger:hover { background: var(--accent-soft); color: var(--accent-deep); }
</style>
