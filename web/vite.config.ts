// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================
import { defineConfig, type Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { VitePWA } from 'vite-plugin-pwa';
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

// Dev-only. The Go server's app-shell handler is what stamps the cd_auth
// cookie the SPA authenticates writes with; under `vite dev` Vite serves the
// shell instead, so the app loads unauthenticated and every write 401s —
// Settings → Server access can't even list the token. Read the same file the
// server reads (main.go apiTokenFilePath) and stamp it ourselves. `apply:
// 'serve'` keeps this out of every build; the token never leaves this machine.
function devAuthCookie(): Plugin {
  return {
    name: 'codedistill-dev-auth-cookie',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        // Only the shell needs it; /api is proxied and forwards the cookie.
        if (req.url?.startsWith('/api')) return next();
        try {
          const tok = readFileSync(join(homedir(), '.config', 'codedistill', 'api-token'), 'utf8').trim();
          if (tok) res.setHeader('Set-Cookie', `cd_auth=${tok}; Path=/; SameSite=Lax`);
        } catch {
          // No token file (or -no-auth): leave the request unauthenticated.
        }
        next();
      });
    },
  };
}

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
    devAuthCookie(),
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
