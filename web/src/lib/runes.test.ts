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

import { expect, it } from 'vitest';
import { recomputes, recomputesWithGeneric } from './runes-probe.svelte';

// Can this test setup catch a rune that is not a rune?
//
// A preference refused to apply because it was written `$derived<T>(...)`. A
// rune the compiler does not recognise compiles to an ordinary function call,
// evaluated once — which is exactly what the symptom looked like. svelte-check
// passed; only a browser found it.
//
// If the second assertion below ever starts passing, the compiler has learned to
// handle a type argument and the guard can go. If it fails, this harness can see
// the class of bug it was built for.

it('a plain $derived recomputes when its dependency changes', () => {
  const p = recomputes();
  expect(p.doubled).toBe(2);
  p.set(21);
  expect(p.doubled).toBe(42);
});

it('a $derived written with a type argument ALSO recomputes', () => {
  const p = recomputesWithGeneric();
  expect(p.doubled).toBe(2);
  p.set(21);
  // The reason this test exists, and the answer it gave.
  //
  // A scratchpad-view preference would not apply, and the fix changed
  // `$derived<ViewMode>(expr)` to `$derived.by(() => expr)`. The commit claimed
  // the generic was the cause: that a rune the compiler does not recognise
  // compiles to an ordinary call evaluated once.
  //
  // This says otherwise. The generic form recomputes. So that explanation was
  // wrong, or at least incomplete — the fix also changed `let` to `const` and
  // the expression to a callback, and something else in that set did the work.
  //
  // Kept as a guard rather than deleted: if this ever starts returning 2, the
  // compiler HAS begun treating the generic form as a plain call, and the
  // original theory becomes true for whoever is reading this next.
  expect(p.doubled).toBe(42);
});
