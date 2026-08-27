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

// Small user-scope UI preferences shared between App and the Settings modal.
// Runes store (same pattern as activity.svelte.ts) so a toggle in Settings
// takes effect in the live App without a reload; persisted per-user via
// user_settings.

import { loadUserBool, saveUserBool, loadUserString, saveUserString } from './userSettings';

const ACTIVITY_WAVE_KEY = 'ui.activity_wave';
const ITEM_NUMBER_PREFIX_KEY = 'ui.item_number_prefix';

// How a work item's number shows as a title prefix in the canvas views (UC-65):
//   off     — no prefix
//   number  — #95
//   bracket — [95]
//   typed   — BUG-95 / TODO-12 / UC-60 (kind + number)
export type ItemNumberPrefix = 'off' | 'number' | 'bracket' | 'typed';
export const ITEM_NUMBER_PREFIXES: ItemNumberPrefix[] = ['off', 'number', 'bracket', 'typed'];

export const uiPrefs = $state({
  // The sliding "wave" across the top of the window while background work
  // (indexing, dedup scans, …) runs. Defaults on; users who find it
  // distracting — the per-button busy states carry the feedback now — can
  // turn it off in Settings → Appearance.
  activityWave: true,
  // Item-number prefix on titles in the canvas/list/calendar views (UC-65).
  // Defaults to the compact "#95" — shows the number without eating space.
  itemNumberPrefix: 'number' as ItemNumberPrefix,
});

export async function initUiPrefs(): Promise<void> {
  uiPrefs.activityWave = await loadUserBool(ACTIVITY_WAVE_KEY, true);
  const p = await loadUserString(ITEM_NUMBER_PREFIX_KEY, 'number');
  uiPrefs.itemNumberPrefix = (ITEM_NUMBER_PREFIXES as string[]).includes(p)
    ? (p as ItemNumberPrefix)
    : 'number';
}

export function setActivityWave(on: boolean): void {
  uiPrefs.activityWave = on;
  saveUserBool(ACTIVITY_WAVE_KEY, on);
}

export function setItemNumberPrefix(mode: ItemNumberPrefix): void {
  uiPrefs.itemNumberPrefix = mode;
  saveUserString(ITEM_NUMBER_PREFIX_KEY, mode);
}
