// SPA mode for the Tauri webview: no SSR, no prerendering. Client-side routing
// is served from the adapter-static `index.html` fallback.
export const ssr = false;
export const prerender = false;