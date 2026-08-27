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

import type { SyncFields } from './types';

// Helpers for the MCP-export sync cache (v0.8.2 steps 8+9). Shared by
// the SyncIndicator placements and the delete-confirm gates.

/** True when the item lives on (or is queued for) a remote MCP
 *  destination — a local delete will push a delete there too. */
export function isRemoteSynced(item: SyncFields): boolean {
  const s = item.sync_status ?? '';
  return s !== '' && s !== 'local-only';
}

/** Extra sentence for delete-confirm dialogs on synced items. Empty
 *  string for local-only items so callers can append unconditionally. */
export function deleteSyncWarning(item: SyncFields | null): string {
  if (!item || !isRemoteSynced(item)) return '';
  return ' This item is synced to an external MCP destination — deleting it here will also delete it there.';
}
