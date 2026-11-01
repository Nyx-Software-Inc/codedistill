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

import { afterAll, beforeAll, expect, it, vi } from 'vitest';
import { dueDayKey, endOfDayISO } from './dueDate';

// Node reads TZ when it next needs it, so this pins the local zone for the
// file: the cases below are about a user west of UTC. vi.stubEnv rather than
// process.env: the app's tsconfig has no Node types, and a clean `npm ci` (as
// the release build runs it) rejects `process` — v1.6.4's build failed on it.
beforeAll(() => { vi.stubEnv('TZ', 'America/New_York'); });
afterAll(() => { vi.unstubAllEnvs(); });

it('reads an end-of-day due date as the local day, whatever zone it was written in', () => {
  // "by Friday" = Fri 23:59 EDT. The server now returns it in UTC, which is
  // already Saturday; the field must still say Friday.
  expect(dueDayKey('2026-08-07T03:59:59Z')).toBe('2026-08-06');
  expect(dueDayKey('2026-08-06T23:59:59-04:00')).toBe('2026-08-06');
});

it('keeps a plain date stored as midnight UTC on that date', () => {
  // The API saved a bare "2026-08-07" as 00:00Z; read as an instant that is
  // Aug 6 in New York, which would show every such date a day early.
  expect(dueDayKey('2026-08-07T00:00:00Z')).toBe('2026-08-07');
});

it('stores a picked day as 23:59 local, and reads it back as the same day', () => {
  const iso = endOfDayISO('2026-11-20');
  expect(iso).toBe('2026-11-21T04:59:00.000Z'); // EST after the clocks go back
  expect(dueDayKey(iso)).toBe('2026-11-20');
});

it('treats empty and unparseable values as no due date', () => {
  expect(dueDayKey(undefined)).toBeUndefined();
  expect(dueDayKey('')).toBeUndefined();
  expect(dueDayKey('not a date')).toBeUndefined();
});
