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

-- Attribution (multi-user groundwork): WHO performed each logged action — a
-- user id, distinct from `source` (the mechanism: ui / mcp / agent). In
-- single-user mode this is always the local user; it only varies once per-user
-- auth lands. Nullable + no FK so pre-existing rows and the 'local' default stay
-- simple (matches the projects.workspace_id pattern).
ALTER TABLE item_events ADD COLUMN actor_user_id TEXT;
