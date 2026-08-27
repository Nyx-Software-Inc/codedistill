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

// The item dock: following a code link from an item detail docks the item
// beside the code window instead of closing it, so you never lose the link
// between the intent and the code. A small stack (Rich's call: cap 3) — one
// transient slot (auto-replaced when you follow a new link) plus pinned items
// that persist. Modelled as a list from day one so single→stack is additive.
import type { CodeAnchorOwnerType } from './types';

export type DockedItem = {
  key: string; // ownerType:ownerId
  ownerType: 'todo_item' | 'bug_item' | 'use_case_item';
  ownerId: string;
  pinned: boolean;
  collapsed: boolean;
};

const MAX_DOCKED = 3;

export const dock = $state<{ items: DockedItem[] }>({ items: [] });

function keyOf(ownerType: string, ownerId: string): string {
  return ownerType + ':' + ownerId;
}

// dockItem docks (or re-focuses) an item. A newly docked item takes the single
// transient (unpinned) slot, replacing any existing transient; pinned items are
// untouched. Past the cap, the oldest pinned item is evicted (the transient is
// always the freshest, so it stays).
export function dockItem(ownerType: DockedItem['ownerType'], ownerId: string): void {
  const key = keyOf(ownerType, ownerId);
  const existing = dock.items.find((d) => d.key === key);
  if (existing) {
    existing.collapsed = false;
    return;
  }
  // Drop the current transient (unpinned) item — one peek slot at a time.
  const kept = dock.items.filter((d) => d.pinned);
  kept.unshift({ key, ownerType, ownerId, pinned: false, collapsed: false });
  // Cap: evict oldest pinned (end of list) until within MAX_DOCKED.
  while (kept.length > MAX_DOCKED) kept.pop();
  dock.items = kept;
}

export function undock(key: string): void {
  dock.items = dock.items.filter((d) => d.key !== key);
}

export function togglePin(key: string): void {
  const d = dock.items.find((x) => x.key === key);
  if (d) d.pinned = !d.pinned;
}

export function toggleCollapse(key: string): void {
  const d = dock.items.find((x) => x.key === key);
  if (d) d.collapsed = !d.collapsed;
}

export function clearDock(): void {
  dock.items = [];
}

// ownerTypeToPath / label helpers shared with the dock UI.
export function dockOwnerLabel(ownerType: string): string {
  return ownerType === 'todo_item' ? 'Todo' : ownerType === 'bug_item' ? 'Bug' : 'Use case';
}

// The anchor-owner union the API uses matches our narrower set.
export function asAnchorOwner(ownerType: DockedItem['ownerType']): CodeAnchorOwnerType {
  return ownerType as CodeAnchorOwnerType;
}
