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

import { beforeEach, describe, expect, it, vi } from 'vitest';

// The settings API is stubbed: these tests are about the wrapper's behaviour —
// does it persist, does it refuse to persist before loading, does it debounce —
// not about HTTP.
const saved: { key: string; value: unknown }[] = [];
vi.mock('./userSettings', () => ({
  LOCAL_USER: 'local',
  loadUserObject: vi.fn(async () => ({})),
  loadUserBool: vi.fn(async (_k: string, d: boolean) => d),
  loadUserNum: vi.fn(async (_k: string, d: number) => d),
  saveUserObject: (key: string, value: object) => { saved.push({ key, value }); },
}));

import * as store from './layoutStore.svelte';
import { paneDef } from './layout';

describe('the reactive wrapper', () => {
  beforeEach(() => { saved.length = 0; });

  it('refuses to persist before the stored layout has loaded', () => {
    // THE race this guard exists for: a mutation arriving before init resolves
    // would save the DEFAULT over whatever the user had stored.
    store.openPane('jobs');
    expect(saved).toHaveLength(0);
  });

  it('persists once loading has happened', async () => {
    await store.initLayout();
    saved.length = 0;
    store.openPane('jobs');
    expect(saved.map((s) => s.key)).toEqual(['ui.layout']);
    expect(store.isPaneOpen('jobs')).toBe(true);
  });

  it('reassigns rather than mutating, so Svelte sees the change', async () => {
    await store.initLayout();
    const before = store.visibleSlots().length;
    store.openPane('tree');
    // If the operations mutated in place, the holder's property would never be
    // reassigned and nothing would re-render.
    expect(store.visibleSlots().length).toBeGreaterThan(before);
  });

  it('moves a pane between slots', async () => {
    await store.initLayout();
    store.openPane('jobs');
    store.movePane('jobs', 'bottom');
    expect(store.paneSlot('jobs')).toBe('bottom');
    expect(store.activePane('bottom')).toBe('jobs');
  });

  it('debounces size saves, and flush commits immediately', async () => {
    vi.useFakeTimers();
    await store.initLayout();
    store.openPane('jobs');
    saved.length = 0;

    // A drag fires this on every pointer move.
    for (let px = 400; px < 420; px++) store.setPaneSize('jobs', px);
    expect(saved).toHaveLength(0);          // nothing written mid-drag
    expect(store.paneSize('jobs')).toBe(419); // but applied, so the drag is smooth

    store.flushPaneSize();                  // pointer-up
    expect(saved).toHaveLength(1);
    vi.useRealTimers();
  });

  it('reset clears a wrecked arrangement but leaves the tree open', async () => {
    await store.initLayout();
    store.openPane('jobs');
    store.movePane('jobs', 'bottom');
    store.openPane('inbox');
    store.setPaneSize('inbox', 900);

    store.resetLayout();

    // Not defaultLayout(), which opens nothing: a bare canvas after asking to
    // put things back reads as a failed reset. The tree is what drawer-open
    // defaulted to before panes existed.
    expect(store.isPaneOpen('tree')).toBe(true);
    expect(store.isPaneOpen('jobs')).toBe(false);
    expect(store.isPaneOpen('inbox')).toBe(false);
    expect(store.visibleSlots()).toEqual(['left']);
    // Sizes go back too, or a pane dragged to 900px stays unusable after a reset.
    expect(store.paneSize('inbox')).toBe(paneDef('inbox')!.size);
  });
});
