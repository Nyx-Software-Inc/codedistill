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

import { describe, expect, it } from 'vitest';
import {
  blockersOf, canAdd, dependentsOf, inForce, nodeKey, reachableFrom,
  type DepEdge,
} from './depGraph';

/** `a` depends on `b` — b has to happen first. */
const dep = (a: string, b: string, over: Partial<DepEdge> = {}): DepEdge => ({
  from_kind: 'todo', from_id: a,
  to_kind: 'todo', to_id: b,
  kind: 'blocks', status: 'accepted',
  ...over,
});

describe('which edges constrain order', () => {
  it('ignores DISMISSED edges', () => {
    // Dismissed edges are KEPT so a later run does not re-offer one somebody
    // already argued with. If they still ordered the graph, dismissing would
    // accomplish nothing. The status is 'dismissed' — an earlier version of
    // inForce() tested for 'rejected', which the server never emits, so every
    // dismissed edge went on constraining order.
    const edges = [dep('a', 'b', { status: 'dismissed' })];
    expect(inForce(edges)).toEqual([]);
    expect(blockersOf(edges, 'todo', 'a')).toEqual([]);
  });

  it('counts an ADVISORY edge, because derived edges need no ratification', () => {
    // The deliberate asymmetry: a bad edge misorders a queue and is noticed
    // immediately, so derived edges are in force before anyone looks.
    expect(inForce([dep('a', 'b', { status: 'advisory' })])).toHaveLength(1);
    expect(inForce([dep('a', 'b', { status: 'accepted' })])).toHaveLength(1);
  });

  it('refuses to be fooled by an unknown status', () => {
    // Fail closed: a status this build does not know is not assumed to order
    // work. The previous != 'rejected' form did the opposite.
    expect(inForce([dep('a', 'b', { status: 'something-new' })])).toEqual([]);
  });

  it('ignores informs, which is not a blocker', () => {
    expect(inForce([dep('a', 'b', { kind: 'informs' })])).toEqual([]);
  });
});

describe('direction', () => {
  const edges = [dep('a', 'b')];

  it('reads from DEPENDS ON to, not the other way round', () => {
    // The one thing every dependency graph gets wrong. Reading it backwards
    // inverts the entire plan silently.
    expect(blockersOf(edges, 'todo', 'a').map((e) => e.to_id)).toEqual(['b']);
    expect(blockersOf(edges, 'todo', 'b')).toEqual([]);
    expect(dependentsOf(edges, 'todo', 'b').map((e) => e.from_id)).toEqual(['a']);
    expect(dependentsOf(edges, 'todo', 'a')).toEqual([]);
  });

  it('keys by kind as well as id, since dependencies cross kinds', () => {
    // A bug and a todo can share an id; an id alone is ambiguous.
    const mixed = [dep('x', 'x', { from_kind: 'todo', to_kind: 'bug' })];
    expect(blockersOf(mixed, 'todo', 'x')).toHaveLength(1);
    expect(blockersOf(mixed, 'bug', 'x')).toHaveLength(0);
    expect(nodeKey('todo', 'x')).not.toBe(nodeKey('bug', 'x'));
  });
});

describe('reachableFrom', () => {
  it('follows the chain transitively', () => {
    const edges = [dep('a', 'b'), dep('b', 'c'), dep('c', 'd')];
    const r = reachableFrom(edges, 'todo', 'a');
    expect([...r].sort()).toEqual(['todo:b', 'todo:c', 'todo:d']);
  });

  it('terminates on a graph that ALREADY has a cycle', () => {
    // Nothing has prevented cycles until now, so old data may contain one. A
    // recursive walk would never return here.
    const edges = [dep('a', 'b'), dep('b', 'a')];
    expect(reachableFrom(edges, 'todo', 'a').has('todo:a')).toBe(true);
  });

  it('does not include the start unless a cycle reaches back to it', () => {
    expect(reachableFrom([dep('a', 'b')], 'todo', 'a').has('todo:a')).toBe(false);
  });
});

describe('canAdd', () => {
  it('allows an ordinary new blocker', () => {
    expect(canAdd([], 'todo', 'a', 'todo', 'b')).toEqual({ ok: true });
  });

  it('refuses an item blocking itself', () => {
    const v = canAdd([], 'todo', 'a', 'todo', 'a');
    expect(v.ok).toBe(false);
  });

  it('refuses a duplicate', () => {
    const v = canAdd([dep('a', 'b')], 'todo', 'a', 'todo', 'b');
    expect(v.ok).toBe(false);
    if (!v.ok) expect(v.reason).toMatch(/already a blocker/i);
  });

  it('refuses the direct inverse', () => {
    // b already waits on a; making a wait on b means neither can start.
    const v = canAdd([dep('b', 'a')], 'todo', 'a', 'todo', 'b');
    expect(v.ok).toBe(false);
    if (!v.ok) expect(v.reason).toMatch(/loop/i);
  });

  it('refuses a LONG cycle, not just the obvious one', () => {
    // b -> c -> d -> a already. Adding "a depends on b" closes a four-node
    // loop, which is the case a naive inverse-only check misses entirely.
    const edges = [dep('b', 'c'), dep('c', 'd'), dep('d', 'a')];
    const v = canAdd(edges, 'todo', 'a', 'todo', 'b');
    expect(v.ok).toBe(false);
    if (!v.ok) expect(v.reason).toMatch(/loop/i);
  });

  it('allows a diamond, which is not a cycle', () => {
    // a waits on b and c; both wait on d. Perfectly orderable.
    const edges = [dep('a', 'b'), dep('b', 'd'), dep('c', 'd')];
    expect(canAdd(edges, 'todo', 'a', 'todo', 'c')).toEqual({ ok: true });
  });

  it('ignores a dismissed edge when judging a loop', () => {
    // The inverse was dismissed, so it constrains nothing and must not block an
    // edge the user is now asserting by hand.
    const edges = [dep('b', 'a', { status: 'dismissed' })];
    expect(canAdd(edges, 'todo', 'a', 'todo', 'b')).toEqual({ ok: true });
  });
});
