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

import { describe, expect, it } from 'vitest';
import {
  PANES, SLOTS, insertionIndex,
  defaultLayout, normalise, openPane, closePane, togglePane,
  activatePane, movePane, setPaneSize, paneSize, slotOf, isOpen,
  visibleSlots, paneDef, fromLegacy, type Layout,
  floatPane, dockPane, floatOf, setFloatBounds, FLOAT_MIN_W, FLOAT_MIN_H,
} from './layout';

describe('the registry', () => {
  // Adding a pane is meant to be one row here and no other change, so a bad row
  // is the likeliest way to break the shell.
  it('has a unique id, a real slot and a usable default size per pane', () => {
    const ids = PANES.map((p) => p.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const p of PANES) {
      expect(SLOTS).toContain(p.slot);
      expect(p.title.length).toBeGreaterThan(0);
      expect(p.min).toBeGreaterThan(0);
      // A default below the minimum would be clamped away on first render.
      expect(p.size).toBeGreaterThanOrEqual(p.min);
    }
  });

  it('every pane can be opened and reaches its declared slot', () => {
    for (const p of PANES) {
      const l = openPane(defaultLayout(), p.id);
      expect(slotOf(l, p.id)).toBe(p.slot);
      expect(l.slots[p.slot].active).toBe(p.id);
    }
  });
});

describe('opening and closing', () => {
  it('opens a pane into its declared slot and shows it', () => {
    const l = openPane(defaultLayout(), 'jobs');
    expect(slotOf(l, 'jobs')).toBe('right');
    expect(l.slots.right.active).toBe('jobs');
  });

  it('opening an already-open pane activates it rather than adding it twice', () => {
    let l = openPane(defaultLayout(), 'jobs');
    l = openPane(l, 'dock');           // dock now active, jobs tabbed away
    expect(l.slots.right.active).toBe('dock');
    l = openPane(l, 'jobs');           // clicking Jobs again
    expect(l.slots.right.panes).toEqual(['jobs', 'dock']);
    expect(l.slots.right.active).toBe('jobs');
  });

  it('closing the showing pane falls back to a neighbour, never to nothing', () => {
    let l = openPane(openPane(defaultLayout(), 'jobs'), 'dock');
    l = closePane(l, 'dock');
    // A slot with tabs and no active pane renders a header above blankness.
    expect(l.slots.right.panes).toEqual(['jobs']);
    expect(l.slots.right.active).toBe('jobs');
  });

  it('closing the last pane empties the slot entirely', () => {
    let l = openPane(defaultLayout(), 'jobs');
    l = closePane(l, 'jobs');
    expect(l.slots.right.panes).toEqual([]);
    expect(l.slots.right.active).toBeNull();
    expect(visibleSlots(l)).toEqual([]);
  });

  it('toggle is open-then-close', () => {
    const l = togglePane(defaultLayout(), 'tree');
    expect(isOpen(l, 'tree')).toBe(true);
    expect(isOpen(togglePane(l, 'tree'), 'tree')).toBe(false);
  });

  it('ignores panes that do not exist', () => {
    const l = defaultLayout();
    expect(openPane(l, 'nonsense')).toBe(l);
    expect(movePane(l, 'nonsense', 'left')).toBe(l);
  });

  it('tabs Needs review beside Jobs rather than replacing it', () => {
    // The motivating arrangement: watch jobs and work the review queue from the
    // same edge, one showing. If opening one closed the other, the pane model
    // would have bought nothing over the modal it replaced.
    let l = openPane(defaultLayout(), 'jobs');
    l = openPane(l, 'inbox');
    expect(l.slots.right.panes).toEqual(['jobs', 'inbox']);
    expect(l.slots.right.active).toBe('inbox');
    expect(isOpen(l, 'jobs')).toBe(true);
  });

  it('never leaves a pane in two slots at once', () => {
    let l = openPane(defaultLayout(), 'jobs');
    l = movePane(l, 'jobs', 'bottom');
    expect(l.slots.right.panes).toEqual([]);
    expect(l.slots.bottom.panes).toEqual(['jobs']);
    expect(slotOf(l, 'jobs')).toBe('bottom');
  });
});

describe('movePane — the operation drag and drop will call', () => {
  const threeOnTheRight = (): Layout => {
    let l = openPane(defaultLayout(), 'dock');
    l = openPane(l, 'jobs');
    // A third, moved in from the left — which also exercises the cross-slot path.
    return movePane(openPane(l, 'tree'), 'tree', 'right');
  };

  it('inserts at an index', () => {
    const l = movePane(threeOnTheRight(), 'tree', 'right', 0);
    expect(l.slots.right.panes).toEqual(['tree', 'dock', 'jobs']);
  });

  it('reorders within the same slot without duplicating', () => {
    const l = movePane(threeOnTheRight(), 'dock', 'right', 2);
    // Removed before inserting, so the index reads against the list WITHOUT the
    // pane in it — which is what dragging between two tab positions means.
    expect(l.slots.right.panes).toEqual(['jobs', 'tree', 'dock']);
    expect(l.slots.right.panes.filter((p) => p === 'dock')).toHaveLength(1);
  });

  it('clamps an index past the end, and a negative one', () => {
    expect(movePane(threeOnTheRight(), 'tree', 'left', 99).slots.left.panes)
      .toEqual(['tree']);
    expect(movePane(threeOnTheRight(), 'tree', 'left', -5).slots.left.panes)
      .toEqual(['tree']);
  });

  it('shows the pane you just moved', () => {
    const l = movePane(threeOnTheRight(), 'dock', 'bottom');
    expect(l.slots.bottom.active).toBe('dock');
  });

  it('leaves the vacated slot with a valid active pane', () => {
    const l = movePane(threeOnTheRight(), 'tree', 'bottom');
    expect(l.slots.right.active).not.toBeNull();
    expect(l.slots.right.panes).toContain(l.slots.right.active!);
  });

  it('refuses an unknown slot', () => {
    const l = threeOnTheRight();
    expect(movePane(l, 'jobs', 'top' as never)).toBe(l);
  });
});

describe('popping a pane out', () => {
  const withJobs = () => openPane(openPane(defaultLayout(), 'jobs'), 'inbox');

  it('takes the pane out of its slot and remembers where it came from', () => {
    const l = floatPane(withJobs(), 'jobs');
    expect(slotOf(l, 'jobs')).toBeNull();
    expect(floatOf(l, 'jobs')?.from).toBe('right');
    expect(isOpen(l, 'jobs')).toBe(true);
    // The slot it left still shows something.
    expect(l.slots.right.active).toBe('inbox');
  });

  it('pops out a closed pane, returning it to its usual slot', () => {
    const l = floatPane(defaultLayout(), 'review');
    expect(floatOf(l, 'review')?.from).toBe('right');
  });

  it('refuses a pane that cannot float', () => {
    const l = openPane(defaultLayout(), 'dock');
    expect(floatPane(l, 'dock')).toBe(l);
    expect(floatPane(l, 'nope')).toBe(l);
  });

  it('opens its window no smaller than usable', () => {
    const f = floatOf(floatPane(openPane(defaultLayout(), 'tree'), 'tree'), 'tree')!;
    expect(f.w).toBeGreaterThanOrEqual(FLOAT_MIN_W);
    expect(f.h).toBeGreaterThanOrEqual(FLOAT_MIN_H);
  });

  it('keeps its window size while popped out, and forgets it once docked', () => {
    let l = setFloatBounds(floatPane(withJobs(), 'jobs'), 'jobs', { w: 900, h: 650 });
    // Popping out again (a second drag out) moves the window, keeps its size.
    l = floatPane(l, 'jobs', { x: 10 });
    expect(floatOf(l, 'jobs')).toMatchObject({ w: 900, h: 650, x: 10 });
    l = floatPane(dockPane(l, 'jobs'), 'jobs');
    expect(floatOf(l, 'jobs')?.w).toBe(Math.max(FLOAT_MIN_W, paneSize(l, 'jobs')));
  });

  it('records bounds, clamped, and ignores what is not a number', () => {
    let l = floatPane(withJobs(), 'jobs');
    l = setFloatBounds(l, 'jobs', { w: 10, h: 10, x: NaN, y: 30.6 });
    expect(floatOf(l, 'jobs')).toMatchObject({ w: FLOAT_MIN_W, h: FLOAT_MIN_H, y: 31 });
    expect(floatOf(l, 'jobs')?.x).toBeUndefined();
    expect(setFloatBounds(withJobs(), 'jobs', { w: 900 })).toEqual(withJobs());
  });

  it('docks back into the slot it came from, showing', () => {
    let l = movePane(withJobs(), 'jobs', 'bottom');
    l = dockPane(floatPane(l, 'jobs'), 'jobs');
    expect(slotOf(l, 'jobs')).toBe('bottom');
    expect(l.slots.bottom.active).toBe('jobs');
    expect(floatOf(l, 'jobs')).toBeNull();
  });

  it('docks where it was dropped', () => {
    const l = dockPane(floatPane(withJobs(), 'jobs'), 'jobs', 'left', 0);
    expect(l.slots.left.panes).toEqual(['jobs']);
  });

  it('a move into a slot docks it too', () => {
    const l = movePane(floatPane(withJobs(), 'jobs'), 'jobs', 'bottom');
    expect(floatOf(l, 'jobs')).toBeNull();
    expect(slotOf(l, 'jobs')).toBe('bottom');
  });

  it('open and toggle leave a popped-out pane where it is', () => {
    const l = floatPane(withJobs(), 'jobs');
    expect(openPane(l, 'jobs')).toBe(l);
    expect(togglePane(l, 'jobs')).toBe(l);
  });

  it('close removes it, and opening again uses its usual slot', () => {
    let l = closePane(floatPane(withJobs(), 'jobs'), 'jobs');
    expect(isOpen(l, 'jobs')).toBe(false);
    l = openPane(l, 'jobs');
    expect(slotOf(l, 'jobs')).toBe('right');
  });

  it('never mutates its input', () => {
    const l = withJobs();
    const before = JSON.stringify(l);
    const f = floatPane(l, 'jobs');
    setFloatBounds(f, 'jobs', { w: 999 });
    dockPane(f, 'jobs');
    closePane(f, 'jobs');
    expect(JSON.stringify(l)).toBe(before);
    expect(floatOf(f, 'jobs')?.w).not.toBe(999);
  });
});

describe('insertionIndex — where a dropped tab lands', () => {
  // Three 100px tabs: [0,100] [100,200] [200,300]. Midpoints at 50, 150, 250.
  const tabs = [
    { start: 0, end: 100 },
    { start: 100, end: 200 },
    { start: 200, end: 300 },
  ];

  it('lands before the tab whose midpoint has not been passed', () => {
    expect(insertionIndex(tabs, 10)).toBe(0);
    expect(insertionIndex(tabs, 49)).toBe(0);
    expect(insertionIndex(tabs, 51)).toBe(1);
    expect(insertionIndex(tabs, 149)).toBe(1);
    expect(insertionIndex(tabs, 151)).toBe(2);
  });

  it('appends past the last midpoint', () => {
    expect(insertionIndex(tabs, 251)).toBe(3);
    expect(insertionIndex(tabs, 9999)).toBe(3);
  });

  it('is exactly at the midpoint, not off by one', () => {
    // The boundary itself: at the midpoint the pointer has reached the second
    // half, so the drop goes after. An inclusive comparison here is the classic
    // off-by-one that makes a small drag feel like it did nothing.
    expect(insertionIndex(tabs, 50)).toBe(1);
    expect(insertionIndex(tabs, 150)).toBe(2);
  });

  it('an empty strip takes the drop at 0', () => {
    expect(insertionIndex([], 0)).toBe(0);
    expect(insertionIndex([], 500)).toBe(0);
  });

  it('handles the before-the-strip case', () => {
    // Negative, which happens when the strip is scrolled or the pointer is left
    // of the first tab.
    expect(insertionIndex(tabs, -40)).toBe(0);
  });

  it('a drag a short way right swaps with the neighbour', () => {
    // The behaviour the midpoint rule exists for, end to end: 'a' dragged just
    // past b's midpoint lands at index 2, and movePane removes before inserting,
    // so the result is b,a,c rather than a,b,c unchanged.
    let l = openPane(openPane(defaultLayout(), 'dock'), 'jobs');
    l = movePane(openPane(l, 'inbox'), 'dock', 'right', insertionIndex(tabs, 151));
    expect(l.slots.right.panes).toEqual(['jobs', 'inbox', 'dock']);
  });
});

describe('sizes are per pane', () => {
  it('remembers a size for each pane sharing a slot', () => {
    let l = openPane(openPane(defaultLayout(), 'jobs'), 'inbox');
    l = setPaneSize(l, 'jobs', 400);
    l = setPaneSize(l, 'inbox', 500);
    // Sharing one size per slot is what would make switching tabs jump.
    expect(paneSize(l, 'jobs')).toBe(400);
    expect(paneSize(l, 'inbox')).toBe(500);
  });

  it('clamps below the declared minimum', () => {
    const min = paneDef('jobs')!.min;
    const l = setPaneSize(defaultLayout(), 'jobs', 10);
    expect(paneSize(l, 'jobs')).toBe(min);
  });

  it('falls back to the default when the stored size is unusable', () => {
    const l = defaultLayout();
    l.sizes.jobs = 5; // below min, as if hand-edited
    expect(paneSize(l, 'jobs')).toBe(paneDef('jobs')!.size);
  });
});

describe('normalise — stored layouts outlive the code that wrote them', () => {
  it('survives junk', () => {
    expect(normalise(null)).toEqual(defaultLayout());
    expect(normalise('{}')).toEqual(defaultLayout());
    expect(normalise(42)).toEqual(defaultLayout());
    expect(normalise({ slots: 'nope' })).toEqual(defaultLayout());
  });

  it('drops panes it does not recognise', () => {
    const l = normalise({
      slots: { right: { panes: ['jobs', 'from-a-newer-build'], active: 'jobs' } },
    });
    // A pane removed in a later release would otherwise render as a blank column.
    expect(l.slots.right.panes).toEqual(['jobs']);
  });

  it('deduplicates a pane listed twice, or in two slots', () => {
    const l = normalise({
      slots: {
        left: { panes: ['jobs', 'jobs'], active: 'jobs' },
        right: { panes: ['jobs'], active: 'jobs' },
      },
    });
    // Rendered twice, and "close" would become ambiguous.
    expect(slotOf(l, 'jobs')).toBe('left');
    expect(l.slots.left.panes).toEqual(['jobs']);
    expect(l.slots.right.panes).toEqual([]);
  });

  it('repairs an active pane the slot does not contain', () => {
    const l = normalise({
      slots: { right: { panes: ['jobs'], active: 'dock' } },
    });
    // Tabs above nothing.
    expect(l.slots.right.active).toBe('jobs');
  });

  it('clears active when the slot ends up empty', () => {
    const l = normalise({ slots: { right: { panes: ['unknown'], active: 'unknown' } } });
    expect(l.slots.right.panes).toEqual([]);
    expect(l.slots.right.active).toBeNull();
  });

  it('keeps usable sizes and discards the rest', () => {
    const l = normalise({ sizes: { jobs: 999, inbox: 'wide', ghost: 300, tree: NaN } });
    expect(l.sizes.jobs).toBe(999);
    expect(paneSize(l, 'inbox')).toBe(paneDef('inbox')!.size);
    expect(l.sizes.ghost).toBeUndefined();
    expect(paneSize(l, 'tree')).toBe(paneDef('tree')!.size);
  });

  it('loads popped-out panes, and drops bad ones', () => {
    const l = normalise({
      slots: { right: { panes: ['jobs'], active: 'jobs' } },
      floating: [
        { pane: 'review', from: 'bottom', w: 800, h: 600, x: 5, y: 6 },
        { pane: 'jobs', from: 'right', w: 500, h: 500 }, // also in a slot
        { pane: 'dock', from: 'right', w: 500, h: 500 }, // cannot float
        { pane: 'gone', from: 'left', w: 500, h: 500 },
        { pane: 'inbox', from: 'top', w: 'wide' }, // bad slot, bad size
        'junk',
      ],
    });
    expect(l.floating.map((f) => f.pane)).toEqual(['review', 'inbox']);
    expect(floatOf(l, 'review')).toEqual({ pane: 'review', from: 'bottom', w: 800, h: 600, x: 5, y: 6 });
    expect(floatOf(l, 'inbox')?.from).toBe('right');
    expect(slotOf(l, 'jobs')).toBe('right');
  });

  it('loads a layout stored before panes could pop out', () => {
    const l = normalise({ slots: { left: { panes: ['tree'], active: 'tree' } }, sizes: {} });
    expect(l.floating).toEqual([]);
  });

  it('round-trips a layout it produced', () => {
    let l = openPane(defaultLayout(), 'tree');
    l = openPane(l, 'jobs');
    l = movePane(l, 'jobs', 'bottom');
    l = setPaneSize(l, 'jobs', 333);
    expect(normalise(JSON.parse(JSON.stringify(l)))).toEqual(l);
  });
});

describe('purity', () => {
  it('never mutates the layout it was given', () => {
    const l = openPane(defaultLayout(), 'jobs');
    const before = JSON.stringify(l);
    openPane(l, 'inbox');
    closePane(l, 'jobs');
    movePane(l, 'jobs', 'left');
    activatePane(l, 'jobs');
    setPaneSize(l, 'jobs', 700);
    // The reactive wrapper assigns the RESULT; if these mutated in place, a
    // Svelte $state would see changes it was never told about.
    expect(JSON.stringify(l)).toBe(before);
  });
});

describe('fromLegacy — nobody wakes up to a reset workspace', () => {
  it('carries the drawer forward, open and at its width', () => {
    const l = fromLegacy({ drawerOpen: true, drawerWidth: 241 });
    expect(isOpen(l, 'tree')).toBe(true);
    expect(paneSize(l, 'tree')).toBe(241);
    expect(l.slots.left.active).toBe('tree');
  });

  it('leaves a closed drawer closed', () => {
    expect(isOpen(fromLegacy({ drawerOpen: false }), 'tree')).toBe(false);
  });

  it('drops the old code window, which is no longer a pane', () => {
    // The code opens in the centre now (docs/design/center-tabs.md); a layout
    // from when it was a side window keeps the drawer and loses nothing else.
    const l = fromLegacy({ drawerOpen: true, codeCanvasVisible: true, codeCanvasWidth: 410 });
    expect(l.slots.left.panes).toEqual(['tree']);
    expect(l.slots.left.active).toBe('tree');
  });

  it('drops a stored code pane', () => {
    const l = normalise({ slots: { left: { panes: ['tree', 'code'], active: 'code' } } });
    expect(l.slots.left.panes).toEqual(['tree']);
    expect(l.slots.left.active).toBe('tree');
  });

  it('leaves panes that never persisted their state closed', () => {
    const l = fromLegacy({ drawerOpen: true });
    // Jobs and the dock had no stored open flag — they always started closed.
    expect(isOpen(l, 'jobs')).toBe(false);
    expect(isOpen(l, 'dock')).toBe(false);
  });

  it('ignores a legacy width below the usable minimum', () => {
    const l = fromLegacy({ drawerOpen: true, drawerWidth: 20 });
    expect(paneSize(l, 'tree')).toBe(paneDef('tree')!.min);
  });
});
