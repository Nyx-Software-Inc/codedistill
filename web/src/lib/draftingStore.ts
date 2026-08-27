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

// The set of owner ids (derived todo/bug/use-case item ids) currently running a
// background "Draft with AI" acceptance-criteria job. Shared so the modal AND
// every card can show a "drafting…" indicator without threading a prop through
// the whole tree. App syncs it from the server; the criteria editor marks an
// item optimistically the moment it starts one, and the server's completion SSE
// re-syncs (clearing it) shortly after.
import { writable } from 'svelte/store';

export const draftingCriteria = writable<Set<string>>(new Set());

/** Optimistically mark an item as drafting (on click, before the server SSE). */
export function markDrafting(ownerId: string): void {
  draftingCriteria.update((s) => {
    if (s.has(ownerId)) return s;
    const next = new Set(s);
    next.add(ownerId);
    return next;
  });
}

/** Replace the set with the authoritative server list (owner ids). */
export function syncDrafting(ownerIds: string[]): void {
  draftingCriteria.set(new Set(ownerIds));
}
