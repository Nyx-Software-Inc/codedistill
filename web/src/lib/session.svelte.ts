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

// Who is signed in and what they may do (docs/design/workflows-and-roles.md).
//
// Read once at start (Root.svelte) from GET /api/v1/me. The app hides what a
// person may not do; the server refuses it regardless, so this is courtesy,
// not security.
import type { Me } from './api';

export const session = $state<{ me: Me | null }>({ me: null });

/** Whether the person may do something. Before /me has loaded, and on the
 *  desktop, everything is allowed: there the one person may do anything. */
export function can(perm: string): boolean {
  const me = session.me;
  if (!me || !me.multi_user) return true;
  return (me.permissions ?? []).includes(perm);
}

/** The person's own user id, for telling their jobs from others'. */
export function myId(): string {
  return session.me?.id ?? 'local';
}
