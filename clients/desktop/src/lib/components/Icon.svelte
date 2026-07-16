<script lang="ts">
	let { name, size = 18, stroke = 1.9 }: { name: string; size?: number; stroke?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width={stroke} stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
	{#if name === 'home'}<path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z"/><path d="M9 21v-7h6v7"/>
	{:else if name === 'search'}<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4.5 4.5"/>
	{:else if name === 'offline'}<path d="M3 3l18 18"/><path d="M6.3 6.3A11 11 0 0 1 20 9.8M3.9 12.2a11 11 0 0 1 5.4-3.5M7.8 16.1a5.7 5.7 0 0 1 3.5-1.2M12 20h.01"/>
	{:else if name === 'device'}<rect x="5" y="3" width="14" height="18" rx="2.5"/><path d="M10 18h4"/>
	{:else if name === 'copy'}<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/>
	{:else if name === 'check'}<path d="m5 12 4.5 4.5L19 7"/>
	{:else if name === 'refresh'}<path d="M20 11a8 8 0 1 0 2 5"/><path d="M20 4v7h-7"/>
	{:else if name === 'link'}<path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.15 1.15"/><path d="M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.15-1.15"/>
	{:else if name === 'library'}<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/>
	{:else if name === 'download'}<path d="M12 3v12M7 10l5 5 5-5"/><path d="M5 20h14"/>
	{:else if name === 'chart'}<path d="M5 20V10M12 20V4M19 20v-7"/>
	{:else if name === 'users'}<path d="M16 20v-1a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v1"/><circle cx="9.5" cy="7" r="3"/><path d="M17 11a3 3 0 1 0-1.2-5.75M21 20v-1a4 4 0 0 0-2.5-3.7"/>
	{:else if name === 'settings'}<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.3 2.3-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.03 1.56v.1h-3.25v-.1a1.7 1.7 0 0 0-1.03-1.56 1.7 1.7 0 0 0-1.88.34l-.06.06-2.3-2.3.06-.06A1.7 1.7 0 0 0 6.4 15a1.7 1.7 0 0 0-1.56-1.03h-.1v-3.25h.1A1.7 1.7 0 0 0 6.4 9.7a1.7 1.7 0 0 0-.34-1.88L6 7.76l2.3-2.3.06.06a1.7 1.7 0 0 0 1.88.34 1.7 1.7 0 0 0 1.03-1.56v-.1h3.25v.1a1.7 1.7 0 0 0 1.03 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.3 2.3-.06.06a1.7 1.7 0 0 0-.34 1.88 1.7 1.7 0 0 0 1.56 1.03h.1v3.25h-.1A1.7 1.7 0 0 0 19.4 15Z"/>
	{:else if name === 'queue'}<path d="M4 6h11M4 12h11M4 18h7"/><path d="m18 15 3 3-3 3"/>
	{:else if name === 'more'}<circle cx="5" cy="12" r="1.3" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.3" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.3" fill="currentColor" stroke="none"/>
	{:else if name === 'play'}<path d="m8 5 11 7-11 7z" fill="currentColor" stroke="none"/>
	{:else if name === 'pause'}<path d="M8 5v14M16 5v14"/>
	{:else if name === 'previous'}<path d="M6 5v14M18 6l-8 6 8 6z" fill="currentColor"/>
	{:else if name === 'next'}<path d="M18 5v14M6 6l8 6-8 6z" fill="currentColor"/>
	{:else if name === 'shuffle'}<path d="m16 3 4 4-4 4M4 7h3c5 0 5 10 10 10h3M4 17h3c1.6 0 2.7-1 3.7-2.2M14 9.2C15 8 15.8 7 17 7h3"/>
	{:else if name === 'repeat'}<path d="M17 2l3 3-3 3"/><path d="M4 11V9a4 4 0 0 1 4-4h12M7 22l-3-3 3-3"/><path d="M20 13v2a4 4 0 0 1-4 4H4"/>
	{:else if name === 'volume'}<path d="M5 10v4h3l4 3V7l-4 3z"/><path d="M16 9a4 4 0 0 1 0 6M18.5 6.5a7.5 7.5 0 0 1 0 11"/>
	{:else if name === 'lyrics'}<path d="M9 18V5l10-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="16" cy="16" r="3"/>
	{:else if name === 'timer'}<circle cx="12" cy="13" r="8"/><path d="M12 9v4l3 2M9 2h6"/>
	{:else if name === 'clock'}<circle cx="12" cy="12" r="8.5"/><path d="M12 7v5l3 2"/>
	{:else if name === 'user'}<circle cx="12" cy="8" r="3.5"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/>
	{:else if name === 'autoplay'}<path d="M4 6h10M4 11h7M4 16h5"/><path d="m16 14 4 3-4 3z" fill="currentColor" stroke="none"/>
	{:else if name === 'crossfade'}<path d="M4 7h3c5 0 5 10 10 10h3M4 17h3c5 0 5-10 10-10h3"/>
	{:else if name === 'heart'}<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>
	{:else if name === 'close'}<path d="M6 6l12 12M18 6 6 18"/>
	{:else if name === 'back'}<path d="m15 5-7 7 7 7"/>
	{:else if name === 'plus'}<path d="M12 5v14M5 12h14"/>
	{:else}<circle cx="12" cy="12" r="7"/>{/if}
</svg>
