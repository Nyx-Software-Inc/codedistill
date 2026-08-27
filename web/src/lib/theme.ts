/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

// Theme mode handling (UC-7). The stored preference is dark | light |
// system; the RESOLVED theme (dark | light) is written to
// document.documentElement's data-theme attribute, which app.css's
// palette tables key off. "system" tracks prefers-color-scheme live.

import { loadUserString, saveUserString } from './userSettings';

export type ThemeMode = 'dark' | 'light' | 'system';
export const THEME_KEY = 'ui.theme';

let mode: ThemeMode = 'dark';
let mql: MediaQueryList | null = null;

function resolved(m: ThemeMode): 'dark' | 'light' {
  if (m === 'system') {
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  return m;
}

function apply() {
  document.documentElement.dataset.theme = resolved(mode);
}

export function currentThemeMode(): ThemeMode {
  return mode;
}

export function setThemeMode(m: ThemeMode, persist = true): void {
  mode = m;
  apply();
  if (persist) saveUserString(THEME_KEY, m);
}

// initTheme loads the stored preference and starts tracking the OS
// scheme for "system". Call once at App boot.
export async function initTheme(): Promise<ThemeMode> {
  mql = window.matchMedia('(prefers-color-scheme: light)');
  mql.addEventListener('change', () => {
    if (mode === 'system') apply();
  });
  const stored = await loadUserString(THEME_KEY, 'dark');
  mode = stored === 'light' || stored === 'system' ? stored : 'dark';
  apply();
  return mode;
}
