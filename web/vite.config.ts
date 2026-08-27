import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { VitePWA } from 'vite-plugin-pwa';

// Vite builds the SPA into ../internal/webui/dist so the Go binary can
// embed it via `//go:embed all:dist` (see internal/webui/embed.go).
// During `npm run dev`, /api/* is proxied to the Go backend on :8080.
//
// PWA (UC-6): the manifest + service worker make the UI installable as
// a standalone-window app. The server side is untouched — the SW only
// precaches the app SHELL (entry js/css/html/icons, ~1.5MB), NOT the
// ~600 lazy Shiki chunks or the 5.6MB vendored Excalidraw build; those
// cache themselves on first use via runtimeCaching. /api and /mcp are
// never touched by the SW.
export default defineConfig({
  plugins: [
    svelte(),
    VitePWA({
      // autoUpdate: a new build skip-waits + claims clients immediately, and
      // App.svelte force-reloads onto it (controllerchange). No "Reload?"
      // prompt — a rebuild can never strand the window on a stale/broken
      // bundle (ends the recurring clear-cache dance after a server upgrade).
      registerType: 'autoUpdate',
      includeAssets: ['icons/*.png'],
      manifest: {
        name: 'CodeDistill',
        short_name: 'CodeDistill',
        description: 'Scratchpad → auto-classified todos, bugs, and knowledge',
        display: 'standalone',
        start_url: '/',
        // Window chrome + splash match the dark theme tokens
        // (--p-0d0d0d / --p-99ccff in app.css).
        background_color: '#0d0d0d',
        theme_color: '#0d0d0d',
        icons: [
          { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: '/icons/icon-512-maskable.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // Shell only — entry bundle, styles, html, icons.
        globPatterns: ['index.html', 'assets/index-*.{js,css}', 'icons/*.png', '*.svg'],
        navigateFallback: 'index.html',
        // /excalidraw/ is a SEPARATE vendored app loaded in an iframe by
        // the sketch editor — it must NOT fall back to the main app shell
        // (that hijacked the iframe into showing a second CodeDistill
        // canvas, CodeDestill_imports bug #3).
        navigateFallbackDenylist: [/^\/api\//, /^\/mcp/, /^\/excalidraw\//],
        runtimeCaching: [
          {
            // Lazy chunks (Shiki languages/themes, code-split views):
            // hashed filenames are immutable, so cache-first is safe.
            urlPattern: /\/assets\/.*\.(js|css)$/,
            handler: 'CacheFirst',
            options: {
              cacheName: 'lazy-assets',
              expiration: { maxEntries: 200, maxAgeSeconds: 60 * 60 * 24 * 90 },
            },
          },
        ],
      },
    }),
  ],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
    },
    // Allow importing the Glassbox guide from ../docs (HelpPanel uses ?raw).
    fs: { allow: ['..'] },
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
});
