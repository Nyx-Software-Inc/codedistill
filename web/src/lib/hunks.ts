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

// Hunk shaping on the client, for "expand".
//
// The server shapes hunks at a fixed context (internal/git/diff.go shapeHunks)
// and that is what the changeset pane renders first. Expanding a hunk needs
// lines the server did not send, so the pane fetches the file once with
// context "full" — every line, numbered on both sides, as ONE hunk — and from
// then on shapes hunks here. A hunk is a window onto that full line list;
// expanding is widening a window; windows that meet are merged.
//
// PURE and tested. The port of shapeHunks is kept line for line so the
// client's 3-context hunks are the server's, and expanding one is a strict
// superset of what was on screen.

import type { DiffHunk, DiffLine } from './api';

/** A window onto the full line list: indices [start, end). */
export interface Window {
  start: number;
  end: number;
}

/** The windows a context of `context` lines would show around every change.
 *  Mirrors shapeHunks: nothing changed → no windows. */
export function windowsFor(lines: DiffLine[], context: number): Window[] {
  const n = lines.length;
  if (n === 0) return [];
  const keep = new Array<boolean>(n).fill(false);
  let changed = false;
  for (let i = 0; i < n; i++) {
    if (lines[i].op === 'ctx') continue;
    changed = true;
    const lo = Math.max(0, i - context);
    const hi = Math.min(n - 1, i + context);
    for (let j = lo; j <= hi; j++) keep[j] = true;
  }
  if (!changed) return [];
  const out: Window[] = [];
  for (let i = 0; i < n; ) {
    if (!keep[i]) { i++; continue; }
    let j = i;
    while (j < n && keep[j]) j++;
    out.push({ start: i, end: j });
    i = j;
  }
  return out;
}

/** Widen window `i` upward (toward the file's start) by `by` lines, merging
 *  with the window above if they meet. */
export function expandUp(ws: Window[], i: number, by: number): Window[] {
  if (i < 0 || i >= ws.length) return ws;
  const out = ws.map((w) => ({ ...w }));
  out[i].start = Math.max(0, out[i].start - by);
  return merge(out);
}

/** Widen window `i` downward by `by` lines; `total` is the full line count. */
export function expandDown(ws: Window[], i: number, by: number, total: number): Window[] {
  if (i < 0 || i >= ws.length) return ws;
  const out = ws.map((w) => ({ ...w }));
  out[i].end = Math.min(total, out[i].end + by);
  return merge(out);
}

/** One window over everything: the whole file. */
export function expandAll(total: number): Window[] {
  return total > 0 ? [{ start: 0, end: total }] : [];
}

function merge(ws: Window[]): Window[] {
  const out: Window[] = [];
  for (const w of ws) {
    const last = out[out.length - 1];
    if (last && w.start <= last.end) {
      // A window that grew upward past the one above it swallows it.
      last.start = Math.min(last.start, w.start);
      last.end = Math.max(last.end, w.end);
    } else {
      out.push({ ...w });
    }
  }
  return out;
}

/** Hunks for the windows, with @@ headers computed the way git (and the
 *  server) compute them: a side with no lines in the run reports the count
 *  before it and length 0; otherwise the start is the next line, 1-based. */
export function hunksFor(lines: DiffLine[], ws: Window[]): DiffHunk[] {
  // oldBefore[i] / newBefore[i]: how many old / new lines precede index i.
  const oldBefore = new Array<number>(lines.length + 1).fill(0);
  const newBefore = new Array<number>(lines.length + 1).fill(0);
  for (let i = 0; i < lines.length; i++) {
    oldBefore[i + 1] = oldBefore[i] + (lines[i].op !== 'add' ? 1 : 0);
    newBefore[i + 1] = newBefore[i] + (lines[i].op !== 'del' ? 1 : 0);
  }
  return ws.map((w) => {
    const run = lines.slice(w.start, w.end);
    let oldLines = 0;
    let newLines = 0;
    for (const l of run) {
      if (l.op !== 'add') oldLines++;
      if (l.op !== 'del') newLines++;
    }
    return {
      old_start: oldBefore[w.start] + (oldLines > 0 ? 1 : 0),
      old_lines: oldLines,
      new_start: newBefore[w.start] + (newLines > 0 ? 1 : 0),
      new_lines: newLines,
      lines: run,
    };
  });
}

/** True when there are lines above window `i` not shown by any window. */
export function hasGapAbove(ws: Window[], i: number): boolean {
  if (i < 0 || i >= ws.length) return false;
  const prevEnd = i === 0 ? 0 : ws[i - 1].end;
  return ws[i].start > prevEnd;
}

/** True when there are lines below the last window. */
export function hasGapBelow(ws: Window[], total: number): boolean {
  if (ws.length === 0) return total > 0;
  return ws[ws.length - 1].end < total;
}
