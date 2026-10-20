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

// Pane layout: where the windows are.
//
// PURE. No runes, no persistence, no DOM — every function here takes a layout
// and returns a new one. The reactive wrapper and storage live in
// layout.svelte.ts, so the parts that can be subtly wrong are the parts a test
// can hold still.
//
// That split is deliberate. A preference that would not apply took three
// attempts to fix in this codebase, and the committed explanation for the fix
// turned out to be false — both because nothing executed the code. A layout
// engine has more room for that kind of error than a preference does.
//
// See docs/design/panes.md for the decisions this implements.

/** The three edges. No top slot: it competes with the header, and is where
 *  VS Code deliberately puts nothing. */
export type SlotID = 'left' | 'right' | 'bottom';
export const SLOTS: SlotID[] = ['left', 'right', 'bottom'];

/** What a pane declares about itself.
 *
 *  A pane states a PREFERRED slot and its sizing needs; it never decides where
 *  it is. That stays layout's business, so adding a pane is adding a row here
 *  rather than touching the shell. */
export interface PaneDef {
  id: string;
  title: string;
  /** Where it goes when first opened, and where "reset" puts it back. */
  slot: SlotID;
  /** Smallest useful size in px. A slot narrower than this is useless rather
   *  than merely cramped, so resize clamps to it. */
  min: number;
  /** Size when nothing has been stored yet. */
  size: number;
  /** True when this pane cannot be closed. Nothing is, today — the canvas is
   *  not a pane at all — but a command surface might be. */
  pinned?: boolean;
}

/** The panes that exist. Adding one is a row; the shell needs no change.
 *
 *  Sizes come from what users already had: the drawer and code canvas carry
 *  their previously persisted widths as defaults, so nobody's layout jumps on
 *  first run. */
export const PANES: readonly PaneDef[] = [
  { id: 'tree', title: 'Project', slot: 'left', min: 180, size: 241 },
  { id: 'code', title: 'Code', slot: 'left', min: 280, size: 410 },
  { id: 'dock', title: 'Items', slot: 'right', min: 220, size: 280 },
  { id: 'jobs', title: 'Jobs', slot: 'right', min: 300, size: 420 },
  // The pane this whole design exists for: a classification queue rendered as a
  // modal is a queue nobody sees. Wider minimum than the others because its
  // sign-off rows carry a risk band, a subject, flags and two buttons.
  { id: 'inbox', title: 'Needs review', slot: 'right', min: 320, size: 460 },
];

export function paneDef(id: string): PaneDef | undefined {
  return PANES.find((p) => p.id === id);
}

/** One edge: an ordered list of panes and which one is showing.
 *
 *  A list rather than a tree, because a slot shows ONE pane and tabs the rest.
 *  That keeps a drop unambiguous — onto a slot means "add as a tab", with no
 *  "or split it" to disambiguate, which is the part that makes real docking
 *  systems expensive. */
export interface Slot {
  panes: string[];
  active: string | null;
}

export interface Layout {
  slots: Record<SlotID, Slot>;
  /** Size per PANE, not per slot. The drawer wants 241px and the code canvas
   *  410px; sharing one size per slot would resize jarringly on every tab
   *  switch. */
  sizes: Record<string, number>;
}

export function defaultLayout(): Layout {
  const slots = { left: emptySlot(), right: emptySlot(), bottom: emptySlot() };
  const sizes: Record<string, number> = {};
  for (const p of PANES) sizes[p.id] = p.size;
  // Nothing open by default. The canvas alone is the honest starting state, and
  // it matches what a new install shows today.
  return { slots, sizes };
}

function emptySlot(): Slot {
  return { panes: [], active: null };
}

function clone(l: Layout): Layout {
  return {
    slots: {
      left: { panes: [...l.slots.left.panes], active: l.slots.left.active },
      right: { panes: [...l.slots.right.panes], active: l.slots.right.active },
      bottom: { panes: [...l.slots.bottom.panes], active: l.slots.bottom.active },
    },
    sizes: { ...l.sizes },
  };
}

export function slotOf(l: Layout, id: string): SlotID | null {
  for (const s of SLOTS) {
    if (l.slots[s].panes.includes(id)) return s;
  }
  return null;
}

export function isOpen(l: Layout, id: string): boolean {
  return slotOf(l, id) !== null;
}

/** openPane puts a pane in its preferred slot and shows it.
 *
 *  Already open: it is activated rather than added again, so clicking a
 *  toolbar button for a pane tabbed away brings it forward instead of
 *  duplicating it. */
export function openPane(l: Layout, id: string): Layout {
  const def = paneDef(id);
  if (!def) return l;
  const existing = slotOf(l, id);
  if (existing) return activatePane(l, id);

  const next = clone(l);
  next.slots[def.slot].panes.push(id);
  next.slots[def.slot].active = id;
  if (next.sizes[id] === undefined) next.sizes[id] = def.size;
  return next;
}

/** closePane removes a pane from wherever it is.
 *
 *  If it was the one showing, the slot falls back to its neighbour rather than
 *  going blank with tabs still in it — a slot with panes and no active pane is
 *  a state nothing should be able to reach. */
export function closePane(l: Layout, id: string): Layout {
  const slot = slotOf(l, id);
  if (!slot) return l;
  if (paneDef(id)?.pinned) return l;

  const next = clone(l);
  const s = next.slots[slot];
  const was = s.panes.indexOf(id);
  s.panes.splice(was, 1);
  if (s.active === id) {
    // Prefer the pane that took its place, else the one before it, else empty.
    s.active = s.panes[was] ?? s.panes[was - 1] ?? null;
  }
  return next;
}

export function togglePane(l: Layout, id: string): Layout {
  return isOpen(l, id) ? closePane(l, id) : openPane(l, id);
}

/** activatePane brings a pane to the front of its slot. No-op if it is closed —
 *  activating something that is not there would otherwise set an active pane a
 *  slot does not contain. */
export function activatePane(l: Layout, id: string): Layout {
  const slot = slotOf(l, id);
  if (!slot) return l;
  const next = clone(l);
  next.slots[slot].active = id;
  return next;
}

/** movePane sends a pane to a slot, optionally at a position.
 *
 *  This is the operation drag and drop will eventually call. It exists and is
 *  tested first on purpose: dragging is then an input method over a mutation
 *  that is already correct, rather than a new mutation written under pointer
 *  events.
 *
 *  Moving within the same slot REORDERS. Removing before inserting means the
 *  index is interpreted against the list without the pane in it, which is what
 *  a drag between tab positions means.
 */
export function movePane(l: Layout, id: string, to: SlotID, index?: number): Layout {
  if (!paneDef(id)) return l;
  if (!SLOTS.includes(to)) return l;

  const from = slotOf(l, id);
  const next = clone(l);

  if (from) {
    const s = next.slots[from];
    const was = s.panes.indexOf(id);
    s.panes.splice(was, 1);
    if (s.active === id) s.active = s.panes[was] ?? s.panes[was - 1] ?? null;
  }

  const dest = next.slots[to];
  const at = index === undefined ? dest.panes.length : clampIndex(index, dest.panes.length);
  dest.panes.splice(at, 0, id);
  // A moved pane is the one you want to see — you just asked for it to be here.
  dest.active = id;
  return next;
}

function clampIndex(i: number, len: number): number {
  if (!Number.isFinite(i) || i < 0) return 0;
  return Math.min(Math.floor(i), len);
}

/** insertionIndex works out where a drop lands in a tab strip.
 *
 *  Pure on purpose: it is the only part of drag and drop that can be wrong in a
 *  way a human would not notice immediately (off by one, or always appending),
 *  and it is the part that cannot be exercised by clicking. The caller passes
 *  the tabs' bounding boxes along the drag axis and the pointer position.
 *
 *  A drop lands BEFORE the first tab whose midpoint the pointer has not yet
 *  passed — the usual rule, and the one that makes dragging a tab a small
 *  distance to the right swap it with its neighbour rather than doing nothing.
 */
export function insertionIndex(
  tabs: readonly { start: number; end: number }[],
  pos: number,
): number {
  for (let i = 0; i < tabs.length; i++) {
    const mid = (tabs[i].start + tabs[i].end) / 2;
    if (pos < mid) return i;
  }
  return tabs.length;
}

/** setPaneSize clamps to the pane's declared minimum. A resize that leaves a
 *  pane too narrow to use is a resize that should have stopped. */
export function setPaneSize(l: Layout, id: string, px: number): Layout {
  const def = paneDef(id);
  if (!def) return l;
  const next = clone(l);
  next.sizes[id] = Math.max(def.min, Math.round(px));
  return next;
}

export function paneSize(l: Layout, id: string): number {
  const def = paneDef(id);
  if (!def) return 0;
  const stored = l.sizes[id];
  return typeof stored === 'number' && stored >= def.min ? stored : def.size;
}

/** The slots that should render, in grid order. A slot with no panes is not a
 *  thin empty column — it is absent. */
export function visibleSlots(l: Layout): SlotID[] {
  return SLOTS.filter((s) => l.slots[s].panes.length > 0 && l.slots[s].active !== null);
}

/** normalise turns anything that came out of storage into a usable layout.
 *
 *  Stored layouts outlive the code that wrote them: a pane can be removed in a
 *  later release, a key can be hand-edited, a half-written value can be read
 *  back. Every rule below is a state that must not reach the renderer.
 */
export function normalise(raw: unknown): Layout {
  const base = defaultLayout();
  if (!raw || typeof raw !== 'object') return base;

  const input = raw as Partial<Layout>;
  const seen = new Set<string>();
  const out = defaultLayout();

  for (const s of SLOTS) {
    const slot = input.slots?.[s];
    const panes: string[] = [];
    if (slot && Array.isArray(slot.panes)) {
      for (const id of slot.panes) {
        // Unknown pane: a layout written by a newer build, or by hand. Dropped
        // rather than rendered as a blank column.
        if (typeof id !== 'string' || !paneDef(id)) continue;
        // The same pane in two slots, or twice in one, would render twice and
        // make "close" ambiguous.
        if (seen.has(id)) continue;
        seen.add(id);
        panes.push(id);
      }
    }
    out.slots[s].panes = panes;
    // An active pane the slot does not contain shows nothing while tabs sit
    // above it. Fall back to the first rather than trusting the stored value.
    const active = typeof slot?.active === 'string' && panes.includes(slot.active)
      ? slot.active
      : (panes[0] ?? null);
    out.slots[s].active = active;
  }

  if (input.sizes && typeof input.sizes === 'object') {
    for (const [id, v] of Object.entries(input.sizes)) {
      const def = paneDef(id);
      if (!def || typeof v !== 'number' || !Number.isFinite(v)) continue;
      out.sizes[id] = Math.max(def.min, Math.round(v));
    }
  }
  return out;
}

/** What the shell persisted before panes existed. */
export interface LegacyLayout {
  drawerOpen?: boolean;
  drawerWidth?: number;
  codeCanvasWidth?: number;
  codeCanvasVisible?: boolean;
  dockOpen?: boolean;
}

/** fromLegacy builds a layout out of the pre-panes settings.
 *
 *  Without this, the first run after panes ships resets everybody's workspace:
 *  the drawer they have had open for months is suddenly closed, at a width they
 *  spent time choosing. A migration is cheaper than that apology, and it only
 *  runs when no layout has been stored yet.
 *
 *  Panes with no legacy equivalent (jobs, inbox) stay closed, which is what
 *  they were — their open state was never persisted at all.
 */
export function fromLegacy(old: LegacyLayout): Layout {
  let l = defaultLayout();
  if (old.drawerOpen) l = openPane(l, 'tree');
  if (old.codeCanvasVisible) l = openPane(l, 'code');
  if (old.dockOpen) l = openPane(l, 'dock');
  if (typeof old.drawerWidth === 'number') l = setPaneSize(l, 'tree', old.drawerWidth);
  if (typeof old.codeCanvasWidth === 'number') l = setPaneSize(l, 'code', old.codeCanvasWidth);
  // The tree is the pane someone who had the drawer open was looking at.
  if (old.drawerOpen) l = activatePane(l, 'tree');
  return l;
}
