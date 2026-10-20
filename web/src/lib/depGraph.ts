/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

// Dependency graph reachability — the part of the picker that has to be right.
//
// PURE: no fetching, no runes, no DOM. The server rejects a self-edge and the
// UNIQUE index rejects a duplicate, but NOTHING rejects a cycle: A blocks B and
// B blocks A is insertable today, and it makes the whole graph unorderable —
// every item in the loop waits for every other one forever.
//
// So the picker has to prevent it, and prevention beats a 400: a candidate that
// would close a loop is disabled with a reason, rather than offered and then
// refused. That requires reachability, which requires a graph walk, which is
// exactly the kind of code that should be tested rather than eyeballed.

/** An edge in the form the API returns. from DEPENDS ON to: `to` happens first. */
export interface DepEdge {
  from_kind: string;
  from_id: string;
  to_kind: string;
  to_id: string;
  kind: string;
  status: string;
}

/** A node handle. Dependencies cross item kinds, so an id alone is ambiguous. */
export const nodeKey = (kind: string, id: string): string => `${kind}:${id}`;

/** The three edge standings, mirroring internal/domain/dependency.go. */
export const EDGE_ADVISORY = 'advisory';
export const EDGE_ACCEPTED = 'accepted';
export const EDGE_DISMISSED = 'dismissed';

/** Edges that actually constrain order — the client's copy of the domain's
 *  InForce().
 *
 *  ADVISORY counts. A derived edge is in force with nobody having ratified it,
 *  which is the opposite of how proposals work elsewhere here: a bad edge
 *  misorders a queue and is noticed the moment someone looks, where a bad
 *  proposal becomes junk work carried for months.
 *
 *  Only a dismissal releases an edge. Dismissed edges are KEPT so a later run
 *  does not re-offer something already argued with, which is exactly why they
 *  have to be filtered out here — a kept edge that still ordered work would
 *  make dismissing it pointless.
 *
 *  Written against the real status vocabulary. An earlier version of this
 *  filtered `status !== 'rejected'` — a value the server never emits — so every
 *  dismissed edge silently went on constraining order. */
export const inForce = (edges: readonly DepEdge[]): DepEdge[] =>
  edges.filter((e) => e.kind === 'blocks'
    && (e.status === EDGE_ADVISORY || e.status === EDGE_ACCEPTED));

/** blockersOf returns the direct prerequisites of a node. */
export function blockersOf(
  edges: readonly DepEdge[],
  kind: string,
  id: string,
): DepEdge[] {
  const key = nodeKey(kind, id);
  return inForce(edges).filter((e) => nodeKey(e.from_kind, e.from_id) === key);
}

/** dependentsOf returns the nodes waiting on this one. */
export function dependentsOf(
  edges: readonly DepEdge[],
  kind: string,
  id: string,
): DepEdge[] {
  const key = nodeKey(kind, id);
  return inForce(edges).filter((e) => nodeKey(e.to_kind, e.to_id) === key);
}

/** reachableFrom walks the DEPENDS-ON direction and returns every node that
 *  `start` already waits for, directly or transitively.
 *
 *  Iterative with a seen-set rather than recursive: the graph may already
 *  contain a cycle (nothing has prevented one until now), and a recursive walk
 *  over a cyclic graph does not return. */
export function reachableFrom(
  edges: readonly DepEdge[],
  startKind: string,
  startID: string,
): Set<string> {
  const live = inForce(edges);
  const out = new Set<string>();
  const stack = [nodeKey(startKind, startID)];
  while (stack.length) {
    const cur = stack.pop()!;
    for (const e of live) {
      if (nodeKey(e.from_kind, e.from_id) !== cur) continue;
      const next = nodeKey(e.to_kind, e.to_id);
      if (out.has(next)) continue;
      out.add(next);
      stack.push(next);
    }
  }
  return out;
}

export type AddVerdict =
  | { ok: true }
  | { ok: false; reason: string };

/** canAdd decides whether "`from` is blocked by `to`" may be added.
 *
 *  Four refusals, in the order a user is likely to hit them. The messages are
 *  the UI's tooltip text, so they say what is wrong rather than naming a rule.
 */
export function canAdd(
  edges: readonly DepEdge[],
  fromKind: string,
  fromID: string,
  toKind: string,
  toID: string,
): AddVerdict {
  if (nodeKey(fromKind, fromID) === nodeKey(toKind, toID)) {
    return { ok: false, reason: 'An item cannot block itself' };
  }
  const already = blockersOf(edges, fromKind, fromID)
    .some((e) => nodeKey(e.to_kind, e.to_id) === nodeKey(toKind, toID));
  if (already) {
    return { ok: false, reason: 'Already a blocker of this item' };
  }
  // The loop-closing case: if `to` already waits for `from` — at any depth —
  // then making `from` wait for `to` means neither can ever start.
  if (reachableFrom(edges, toKind, toID).has(nodeKey(fromKind, fromID))) {
    return {
      ok: false,
      reason: 'That item already waits on this one — adding this would make a loop',
    };
  }
  return { ok: true };
}
