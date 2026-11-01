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

// Due dates are stored as instants, but people mean a day. One rule for which
// day, shared by the date field and the calendar so the two can't disagree:
//
//   - the local calendar day of the instant ("by Friday" is stored as Friday
//     23:59 local, and must read as Friday wherever the server writes it in
//     UTC — the server now always does);
//   - except exactly midnight UTC, which is how a bare "2026-06-30" was saved
//     by the API. That one is a date, not an instant, and keeps its UTC day —
//     otherwise everyone west of Greenwich would see it a day early.

export function dueDayKey(iso: string | undefined | null): string | undefined {
  if (!iso) return undefined;
  const d = new Date(iso);
  if (isNaN(d.getTime())) return undefined;
  const dateOnly = d.getUTCHours() === 0 && d.getUTCMinutes() === 0 &&
    d.getUTCSeconds() === 0 && d.getUTCMilliseconds() === 0;
  return dateOnly ? d.toISOString().slice(0, 10) : localDayKey(d);
}

export function localDayKey(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

// The instant a chosen day is stored as: the end of that day, local time.
export function endOfDayISO(dayKey: string): string {
  const [y, m, d] = dayKey.split('-').map(Number);
  return new Date(y, m - 1, d, 23, 59, 0, 0).toISOString();
}
