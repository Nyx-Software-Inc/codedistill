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

// A process-wide registry of open modals, so that a single Escape closes only
// the TOPMOST one instead of every stacked modal at once (audit M23). Each open
// Modal registers its stacking layer (z) and gets an id; the top is the highest
// z, ties broken by latest registration (last-opened wins). Not reactive — this
// only routes the Escape keypress, it never drives rendering.

type Entry = { id: number; z: number };

let stack: Entry[] = [];
let nextId = 1;

/** Register an open modal at stacking layer z; returns its id. */
export function registerModal(z: number): number {
  const id = nextId++;
  stack.push({ id, z });
  return id;
}

/** Remove a modal from the registry (on close or destroy). */
export function unregisterModal(id: number): void {
  stack = stack.filter((m) => m.id !== id);
}

/** Whether this modal is the topmost open one — highest z, then latest id. */
export function isTopModal(id: number): boolean {
  if (stack.length === 0) return false;
  let top = stack[0];
  for (const m of stack) {
    if (m.z > top.z || (m.z === top.z && m.id > top.id)) top = m;
  }
  return top.id === id;
}
