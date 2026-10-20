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

// The reactive, persisted wrapper around layout.ts.
//
// Deliberately thin. Every function here is "apply a pure operation, then save"
// — the reasoning lives in layout.ts where tests can reach it. Anything
// resembling a rule that got written in this file is in the wrong file.

import {
  defaultLayout, normalise, fromLegacy, paneSize as sizeOf,
  openPane as _open, closePane as _close, togglePane as _toggle,
  activatePane as _activate, movePane as _move, setPaneSize as _setSize,
  slotOf, isOpen as _isOpen, visibleSlots as _visible,
  type Layout, type SlotID,
} from './layout';
import {
  loadUserBool, loadUserNum, loadUserObject, saveUserObject,
} from './userSettings';

const LAYOUT_KEY = 'ui.layout';

// A holder rather than an exported `let`: a module-level `let` cannot be
// reassigned from outside, and the operations in layout.ts return NEW layouts
// rather than mutating. Assigning a property of a $state object is what makes
// that reactive.
const state = $state({ current: defaultLayout() });

/** The live layout. Read it in markup; never assign into it directly — the
 *  functions below keep storage in step. */
export function layout(): Layout {
  return state.current;
}

let loaded = false;

/** initLayout loads the stored layout, or builds one from the pre-panes
 *  settings so nobody's workspace resets on first run.
 *
 *  Fire-and-forget like the other preference loaders, which means readers must
 *  cope with the default being visible for one frame. That is a real hazard and
 *  not a theoretical one: a scratchpad-view preference read ONCE at component
 *  init captured the pre-load default and never updated. Read `layout()` inside
 *  a derived, not into a local at init.
 */
export async function initLayout(): Promise<void> {
  try {
    const raw = await loadUserObject<Record<string, unknown>>(LAYOUT_KEY, {});
    if (raw && Object.keys(raw).length > 0) {
      state.current = normalise(raw);
      loaded = true;
      return;
    }
    // Nothing stored: migrate. Read the old keys directly rather than through
    // the panels that used to own them, because those panels are about to stop
    // owning anything.
    const [drawerOpen, drawerWidth, codeWidth] = await Promise.all([
      loadUserBool('ui.drawer.open', false),
      loadUserNum('ui.drawer.width', 0),
      loadUserNum('ui.codecanvas.width', 0),
    ]);
    state.current = fromLegacy({
      drawerOpen,
      drawerWidth: drawerWidth || undefined,
      codeCanvasWidth: codeWidth || undefined,
    });
    // Saved immediately, so the migration happens once rather than on every
    // start until the user changes something.
    persist();
  } catch {
    state.current = defaultLayout();
  } finally {
    loaded = true;
  }
}

function persist(): void {
  // Guarded: a mutation arriving before the load would otherwise save the
  // DEFAULT over the user's stored layout — the classic read-modify-write race
  // against one's own async initialiser.
  if (!loaded) return;
  saveUserObject(LAYOUT_KEY, state.current);
}

// ── Operations. Apply, then save. ───────────────────────────────────────────

export function openPane(id: string): void {
  state.current = _open(state.current, id);
  persist();
}

export function closePane(id: string): void {
  state.current = _close(state.current, id);
  persist();
}

export function togglePane(id: string): void {
  state.current = _toggle(state.current, id);
  persist();
}

export function activatePane(id: string): void {
  state.current = _activate(state.current, id);
  persist();
}

export function movePane(id: string, to: SlotID, index?: number): void {
  state.current = _move(state.current, id, to, index);
  persist();
}

/** resetLayout is the escape hatch from an arrangement you cannot undo.
 *
 *  Not quite defaultLayout(): that opens NOTHING, which is right for a fresh
 *  install (the canvas alone is the honest starting state) but reads as broken
 *  when you asked to put things back. The tree is opened because drawer-open was
 *  the long-standing default before panes existed — DRAWER_KEY defaulted true.
 */
export function resetLayout(): void {
  state.current = _open(defaultLayout(), 'tree');
  persist();
}

// ── Reads ───────────────────────────────────────────────────────────────────

export function isPaneOpen(id: string): boolean {
  return _isOpen(state.current, id);
}

export function paneSlot(id: string): SlotID | null {
  return slotOf(state.current, id);
}

export function paneSize(id: string): number {
  return sizeOf(state.current, id);
}

export function visibleSlots(): SlotID[] {
  return _visible(state.current);
}

export function activePane(slot: SlotID): string | null {
  return state.current.slots[slot].active;
}

export function slotPanes(slot: SlotID): string[] {
  return state.current.slots[slot].panes;
}

// ── Dragging ────────────────────────────────────────────────────────────────
//
// Shared rather than per-frame because a drag STARTS in one PaneFrame and ENDS
// in another — and because the slots that are currently empty do not render at
// all, so App needs to know a drag is in flight to offer them as targets. That
// is the one case drag and drop can do that the Move menu cannot avoid: you
// cannot drop into a slot that is not on screen.

const drag = $state({ pane: null as string | null });

/** The pane being dragged, or null. */
export function draggingPane(): string | null {
  return drag.pane;
}

export function beginDrag(id: string): void {
  drag.pane = id;
}

export function endDrag(): void {
  drag.pane = null;
}

// ── Resizing ────────────────────────────────────────────────────────────────

let sizeTimer: ReturnType<typeof setTimeout> | undefined;

/** setPaneSize applies immediately and saves on a delay.
 *
 *  A drag fires this on every pointer move. Applying at once keeps the drag
 *  smooth; debouncing the save keeps a single resize from becoming two hundred
 *  writes to the settings API.
 */
export function setPaneSize(id: string, px: number): void {
  state.current = _setSize(state.current, id, px);
  clearTimeout(sizeTimer);
  sizeTimer = setTimeout(persist, 400);
}

/** flushPaneSize saves a pending size now — on pointer-up, so letting go
 *  commits rather than waiting out the debounce. */
export function flushPaneSize(): void {
  clearTimeout(sizeTimer);
  persist();
}
