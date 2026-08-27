-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--
--  CodeDistill
--
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Member lifecycle (multi-user auth, Phase 3). A workspace member's status drives
-- seat accounting: only `active` members consume a license seat. `invited` is a
-- pending member (pre-first-login); `deactivated` is offboarded — login blocked,
-- seat freed, history kept. Existing members backfill to 'active'. Enum is
-- enforced in Go (SQLite ADD COLUMN keeps it simple). See docs/design/auth.md.
ALTER TABLE workspace_members ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
