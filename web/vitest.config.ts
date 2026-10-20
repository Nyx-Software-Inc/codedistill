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

// Test config, separate from vite.config.ts on purpose.
//
// The build config carries a dev-only middleware that reads the API token out of
// the user's home directory, and the PWA plugin. Neither belongs in a test run:
// a test suite that touches ~/.config or generates a service worker is a test
// suite with reasons to fail that have nothing to do with the code.
//
// Why this exists at all: a preference that would not apply took three attempts
// to fix — an async race, then a rune written with a type argument that compiled
// to an ordinary function call. Both type-checked clean. `npm run check` proves
// shapes agree; it never executes anything. The only thing that caught either
// was a human opening a browser.

import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte({ hot: false })],
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
    // Runes work in .svelte.ts modules under this condition; without it a
    // $state in a plain module is a compile error rather than reactive state.
    alias: [{ find: /^svelte$/, replacement: 'svelte' }],
  },
  resolve: {
    conditions: ['browser'],
  },
});
