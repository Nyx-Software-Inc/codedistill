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

// The item-number title prefix (UC-65), formatted per the user's chosen mode.
// Reading uiPrefs here keeps every view (canvas / list / calendar) in sync and
// reactive — a change in Settings re-renders titles live.
import { uiPrefs } from './uiPrefs.svelte';

// kind values come from derivedStatus: 'todo' | 'bug' | 'use case' | 'kb'.
const KIND_ABBR: Record<string, string> = {
  todo: 'TODO',
  bug: 'BUG',
  'use case': 'UC',
};

/**
 * The prefix string for an item's title, e.g. "BUG-95", "#95", "[95]", or "".
 * Empty when the mode is off, there's no number, or the kind has no number
 * (kb / unclassified — the use case scopes this to bug/todo/use-case).
 */
export function numberPrefix(kind: string | undefined, num: number | undefined): string {
  const mode = uiPrefs.itemNumberPrefix;
  if (mode === 'off' || !num) return '';
  switch (mode) {
    case 'number':
      return `#${num}`;
    case 'bracket':
      return `[${num}]`;
    case 'typed': {
      const abbr = kind ? KIND_ABBR[kind] : undefined;
      // A kind without an abbreviation (shouldn't happen for numbered items)
      // falls back to the bare number rather than an ugly "undefined-95".
      return abbr ? `${abbr}-${num}` : `#${num}`;
    }
    default:
      return '';
  }
}

/** Prepend the prefix (with a separating space) to a title, or return it as-is. */
export function withNumberPrefix(title: string, kind: string | undefined, num: number | undefined): string {
  const p = numberPrefix(kind, num);
  return p ? `${p}  ${title}` : title;
}
