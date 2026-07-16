import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Tauri loads the app as a SPA from local files (no Node server), so we use
// adapter-static in SPA/fallback mode and disable SSR/prerender globally.
export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({
				fallback: 'index.html',
				strict: false
			})
		})
	],
	clearScreen: false,
	server: {
		port: 5173,
		strictPort: true
	}
});