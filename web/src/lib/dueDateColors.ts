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

// UC-48: color a canvas item's header bar by its due date so upcoming and
// overdue deadlines are visible at a glance. Stored in server-backed user
// settings (same as type colors), so the toggle follows the user across
// devices. The due date is the item's derived todo's due_date (via the
// dueDateBySource map) — including one assigned after classification.

import { loadUserObject, saveUserObject } from './userSettings';

export interface DueDateColorsConfig {
  enabled: boolean;
}

export const DUE_DATE_COLORS_KEY = 'canvas.due_date_colors';

// On by default — the use case asks for the coloring; the toggle turns it off.
export const DEFAULT_DUE_DATE_COLORS: DueDateColorsConfig = { enabled: true };

export const loadDueDateColors = () =>
  loadUserObject<DueDateColorsConfig>(DUE_DATE_COLORS_KEY, DEFAULT_DUE_DATE_COLORS);

export const saveDueDateColors = (cfg: DueDateColorsConfig) =>
  saveUserObject(DUE_DATE_COLORS_KEY, cfg);

// dueDateClass maps an ISO due date to a header state relative to nowMs:
//   > 48h out            → ''           (normal)
//   24h .. 48h out       → 'due-warn'   (yellow)
//   < 24h out / overdue  → 'due-urgent' (red)
export function dueDateClass(
  iso: string | undefined,
  nowMs: number,
): '' | 'due-warn' | 'due-urgent' {
  if (!iso) return '';
  const due = Date.parse(iso);
  if (Number.isNaN(due)) return '';
  const hours = (due - nowMs) / 3_600_000;
  if (hours < 24) return 'due-urgent'; // includes overdue (negative hours)
  if (hours <= 48) return 'due-warn';
  return '';
}
