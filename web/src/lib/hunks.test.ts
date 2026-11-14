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
import type { DiffLine } from './api';
import {
  windowsFor, expandUp, expandDown, expandAll, hunksFor, hasGapAbove, hasGapBelow,
} from './hunks';

// A 20-line file with one deletion at old line 6 and one insertion after
// old line 14 — the same shape the Go test uses, so the headers can be
// checked against what the server produces ({8,3,7,4} for the second hunk
// at context 1 was the case the Go test got wrong first).
function fixture(): DiffLine[] {
  const out: DiffLine[] = [];
  let o = 1;
  let n = 1;
  for (let i = 1; i <= 20; i++) {
    if (i === 6) { out.push({ op: 'del', old: o++, text: `old ${i}` }); continue; }
    if (i === 15) out.push({ op: 'add', new: n++, text: 'inserted' });
    out.push({ op: 'ctx', old: o++, new: n++, text: `line ${i}` });
  }
  return out;
}

describe('windowsFor', () => {
  it('is empty for no lines and for no changes', () => {
    expect(windowsFor([], 3)).toEqual([]);
    const ctx: DiffLine[] = [{ op: 'ctx', old: 1, new: 1, text: 'a' }];
    expect(windowsFor(ctx, 3)).toEqual([]);
  });
  it('one window per change at small context, clamped to the file', () => {
    const ws = windowsFor(fixture(), 1);
    // del is at index 5; add at index 14 (after 14 ctx + 1 del = idx 14).
    expect(ws).toEqual([{ start: 4, end: 7 }, { start: 13, end: 16 }]);
  });
  it('windows that touch merge', () => {
    const ws = windowsFor(fixture(), 5);
    expect(ws).toEqual([{ start: 0, end: 20 }]);
  });
});

describe('hunksFor headers', () => {
  it('matches git conventions for a deletion and an insertion', () => {
    const lines = fixture();
    const hs = hunksFor(lines, windowsFor(lines, 1));
    expect(hs[0]).toMatchObject({ old_start: 5, old_lines: 3, new_start: 5, new_lines: 2 });
    expect(hs[1]).toMatchObject({ old_start: 14, old_lines: 2, new_start: 13, new_lines: 3 });
  });
  it('a run with no old lines reports the count before it and 0', () => {
    const lines: DiffLine[] = [{ op: 'add', new: 1, text: 'x' }];
    expect(hunksFor(lines, [{ start: 0, end: 1 }])[0]).toMatchObject({
      old_start: 0, old_lines: 0, new_start: 1, new_lines: 1,
    });
  });
});

describe('expanding', () => {
  const lines = fixture();
  const ws = windowsFor(lines, 1);
  it('up widens toward the start and clamps at 0', () => {
    expect(expandUp(ws, 0, 10)).toEqual([{ start: 0, end: 7 }, { start: 13, end: 16 }]);
  });
  it('down widens and clamps at the total', () => {
    // 21 lines: 20 on the old side plus the insertion.
    expect(expandDown(ws, 1, 10, lines.length)).toEqual([{ start: 4, end: 7 }, { start: 13, end: 21 }]);
  });
  it('windows that meet merge into one', () => {
    expect(expandDown(ws, 0, 10, lines.length)).toEqual([{ start: 4, end: 17 }]);
    expect(expandUp(ws, 1, 10)).toEqual([{ start: 3, end: 16 }]);
  });
  it('all is one window', () => {
    expect(expandAll(20)).toEqual([{ start: 0, end: 20 }]);
    expect(expandAll(0)).toEqual([]);
  });
  it('gaps are reported', () => {
    expect(hasGapAbove(ws, 0)).toBe(true);
    expect(hasGapAbove(ws, 1)).toBe(true);
    expect(hasGapAbove([{ start: 0, end: 3 }], 0)).toBe(false);
    expect(hasGapBelow(ws, 20)).toBe(true);
    expect(hasGapBelow([{ start: 0, end: 20 }], 20)).toBe(false);
  });
  it('out-of-range indices are a no-op', () => {
    expect(expandUp(ws, 5, 10)).toBe(ws);
    expect(expandDown(ws, -1, 10, 20)).toBe(ws);
  });
});
