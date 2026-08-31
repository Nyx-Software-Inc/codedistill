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
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { cp } from 'node:fs/promises';
import path from 'node:path';

// Builds the React+Excalidraw wrapper into web/public/excalidraw/.
// The main Svelte SPA's vite serves that directory as static
// assets, and SketchEditor.svelte loads it in an iframe.
//
// Built artifacts are committed (vendored) so users / contributors
// don't need to run `npm install` in this sub-project just to use
// the app. Rebuild only when bumping @excalidraw/excalidraw.
// copyExcalidrawAssets vendors the runtime assets (fonts, locales,
// vendor JS chunk) Excalidraw expects to fetch at runtime. Without
// this, the editor falls back to a CDN URL (https://esm.sh/...)
// and the app no longer works offline. Pairs with the
// window.EXCALIDRAW_ASSET_PATH set in src/main.tsx.
const copyExcalidrawAssets = {
  name: 'copy-excalidraw-assets',
  closeBundle: async () => {
    const src = path.resolve(
      __dirname,
      'node_modules/@excalidraw/excalidraw/dist/excalidraw-assets',
    );
    const dest = path.resolve(__dirname, '../public/excalidraw/excalidraw-assets');
    await cp(src, dest, { recursive: true });
  },
};

export default defineConfig({
  plugins: [react(), copyExcalidrawAssets],
  base: '/excalidraw/',
  define: {
    // Excalidraw's package main uses process.env.* to pick between
    // dev/prod and React/Preact bundles at require() time. Vite
    // needs these baked in at build time or the require() fails.
    'process.env.IS_PREACT': '"false"',
    'process.env.NODE_ENV': '"production"',
  },
  build: {
    outDir: '../public/excalidraw',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Stable asset names so the index.html reference doesn't
        // hash-change with every rebuild — keeps git diffs clean
        // and lets the host iframe URL stay stable.
        entryFileNames: 'assets/index.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
});
