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

// Canvas behavior settings.
//
// The restack layout mode is remembered PER SCRATCHPAD in
// localStorage — it's a device-local ergonomic preference, not data.
// (The old user-setting toggle canvas.restack.auto_resize is retired;
// its stored value is harmless and simply unread.)

import type { RestackMode } from './api';

const restackModeKey = (scratchpadId: string) => `cd.restackMode.${scratchpadId}`;

export function loadRestackMode(scratchpadId: string): RestackMode {
  const v = localStorage.getItem(restackModeKey(scratchpadId));
  return v === 'fit' || v === 'cols2' || v === 'cols3' ? v : 'tidy';
}

export function saveRestackMode(scratchpadId: string, mode: RestackMode): void {
  localStorage.setItem(restackModeKey(scratchpadId), mode);
}

// UC-12: per-scratchpad auto-tidy. Device-local like the restack mode
// — both are canvas ergonomics, not data.
const autoTidyKey = (scratchpadId: string) => `cd.autoTidy.${scratchpadId}`;

export function loadAutoTidy(scratchpadId: string): boolean {
  return localStorage.getItem(autoTidyKey(scratchpadId)) === 'true';
}

export function saveAutoTidy(scratchpadId: string, on: boolean): void {
  localStorage.setItem(autoTidyKey(scratchpadId), String(on));
}
