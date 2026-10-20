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

// A probe for runes.test.ts. Exists only so the test suite can demonstrate that
// it can tell a reactive derived from one that merely type-checks.

export function recomputes() {
  let n = $state(1);
  const doubled = $derived(n * 2);
  return {
    get doubled() { return doubled; },
    set(v: number) { n = v; },
  };
}

export function recomputesWithGeneric() {
  let n = $state(1);
  // Deliberately the broken form. Do not copy this.
  const doubled = $derived<number>(n * 2);
  return {
    get doubled() { return doubled; },
    set(v: number) { n = v; },
  };
}
