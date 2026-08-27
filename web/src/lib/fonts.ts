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

// User-selectable fonts. Two independent surfaces:
//   - scratchpad items  → --item-font-family / --item-font-size
//   - code (file tree + code viewer) → --code-font-family / --code-font-size
// The chosen values are written as CSS custom properties on
// document.documentElement; the surfaces read them (with a sensible default
// fallback in app.css). Fonts are system fonts — nothing is bundled — so an
// uninstalled choice falls back gracefully to the surface's default stack.

import { loadUserString, saveUserString, loadUserNum, saveUserNum } from './userSettings';

const K = {
  itemFamily: 'ui.item_font_family',
  itemSize: 'ui.item_font_size',
  codeFamily: 'ui.code_font_family',
  codeSize: 'ui.code_font_size',
} as const;

export const DEFAULT_ITEM_SIZE = 12;
export const DEFAULT_CODE_SIZE = 12;

export type FontState = {
  itemFamily: string; // '' = use the surface default
  itemSize: number;
  codeFamily: string; // '' = use the default monospace stack
  codeSize: number;
};

let state: FontState = {
  itemFamily: '',
  itemSize: DEFAULT_ITEM_SIZE,
  codeFamily: '',
  codeSize: DEFAULT_CODE_SIZE,
};

function apply(): void {
  const s = document.documentElement.style;
  if (state.itemFamily) s.setProperty('--item-font-family', state.itemFamily);
  else s.removeProperty('--item-font-family');
  s.setProperty('--item-font-size', `${state.itemSize}px`);

  if (state.codeFamily) s.setProperty('--code-font-family', state.codeFamily);
  else s.removeProperty('--code-font-family');
  s.setProperty('--code-font-size', `${state.codeSize}px`);
}

export function currentFonts(): FontState {
  return { ...state };
}

export function setItemFont(family: string, size: number): void {
  state = { ...state, itemFamily: family, itemSize: size };
  apply();
  saveUserString(K.itemFamily, family);
  saveUserNum(K.itemSize, size);
}

export function setCodeFont(family: string, size: number): void {
  state = { ...state, codeFamily: family, codeSize: size };
  apply();
  saveUserString(K.codeFamily, family);
  saveUserNum(K.codeSize, size);
}

// initFonts loads the stored preferences and applies them. Call once at App boot
// (like initTheme).
export async function initFonts(): Promise<FontState> {
  state = {
    itemFamily: await loadUserString(K.itemFamily, ''),
    itemSize: await loadUserNum(K.itemSize, DEFAULT_ITEM_SIZE),
    codeFamily: await loadUserString(K.codeFamily, ''),
    codeSize: await loadUserNum(K.codeSize, DEFAULT_CODE_SIZE),
  };
  apply();
  return { ...state };
}
