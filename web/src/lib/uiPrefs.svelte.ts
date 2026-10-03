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
const SORT_PROJECTS_KEY = 'ui.sort_projects_alphabetically';
const DEFAULT_CANVAS_VIEW_KEY = 'ui.default_canvas_view';

// How a work item's number shows as a title prefix in the canvas views (UC-65):
//   off     — no prefix
//   number  — #95
//   bracket — [95]
//   typed   — BUG-95 / TODO-12 / UC-60 (kind + number)
export type ItemNumberPrefix = 'off' | 'number' | 'bracket' | 'typed';
export const ITEM_NUMBER_PREFIXES: ItemNumberPrefix[] = ['off', 'number', 'bracket', 'typed'];

/** Which view a scratchpad opens in. 'canvas' is the default and the one the
 *  product is really about; the others are alternative readings of the same
 *  items. */
export type CanvasView = 'canvas' | 'list' | 'kanban' | 'calendar';
export const CANVAS_VIEWS: CanvasView[] = ['canvas', 'list', 'kanban', 'calendar'];

export const uiPrefs = $state({
  // The sliding "wave" across the top of the window while background work
  // (indexing, dedup scans, …) runs. Defaults on; users who find it
  // distracting — the per-button busy states carry the feedback now — can
  // turn it off in Settings → Appearance.
  activityWave: true,
  // Item-number prefix on titles in the canvas/list/calendar views (UC-65).
  // Defaults to the compact "#95" — shows the number without eating space.
  itemNumberPrefix: 'number' as ItemNumberPrefix,
  // Project dropdowns read in creation order by default — how they always have,
  // and it keeps recent work near where you left it. Alphabetical is opt-in,
  // for when there are enough projects that "which one was that" beats "what
  // did I touch last".
  sortProjectsAlphabetically: false,
  // Which view a scratchpad opens in. 'canvas' because that is what the product
  // is for; the others are alternative readings of the same items.
  defaultCanvasView: 'canvas' as CanvasView,
});

export async function initUiPrefs(): Promise<void> {
  uiPrefs.activityWave = await loadUserBool(ACTIVITY_WAVE_KEY, true);
  uiPrefs.sortProjectsAlphabetically = await loadUserBool(SORT_PROJECTS_KEY, false);
  const v = await loadUserString(DEFAULT_CANVAS_VIEW_KEY, 'canvas');
  // An unrecognised stored value falls back rather than leaving the canvas in
  // a view that does not exist — a setting written by a newer build, or by hand.
  uiPrefs.defaultCanvasView = (CANVAS_VIEWS as string[]).includes(v)
    ? (v as CanvasView)
    : 'canvas';
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

export function setSortProjectsAlphabetically(on: boolean): void {
  uiPrefs.sortProjectsAlphabetically = on;
  saveUserBool(SORT_PROJECTS_KEY, on);
}

export function setDefaultCanvasView(v: CanvasView): void {
  uiPrefs.defaultCanvasView = v;
  saveUserString(DEFAULT_CANVAS_VIEW_KEY, v);
}

/** Orders projects for a dropdown, honouring the preference.
 *
 *  One function rather than a .sort() at each of the six call sites, so the
 *  lists cannot disagree about their order — which is the actual complaint:
 *  a dropdown sorted one way and another sorted differently is worse than
 *  either choice.
 *
 *  Copies before sorting: Array.prototype.sort mutates, and these arrays are
 *  the live project list several components render from.
 */
export function orderProjects<T extends { name: string }>(projects: T[]): T[] {
  if (!uiPrefs.sortProjectsAlphabetically) {
    return projects;
  }
  return [...projects].sort((a, b) =>
    a.name.localeCompare(b.name, undefined, { sensitivity: 'base', numeric: true }));
}
