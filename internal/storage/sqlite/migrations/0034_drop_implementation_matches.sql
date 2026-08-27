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

-- v0.11.0 — retire the implementation matcher.
--
-- "Matcher is dead" (glass-box plan, locked principle #1): guessing which
-- commit implemented which item after the fact was noisy and routinely
-- dismissed. It's replaced by authoritative item↔code association —
-- agent-authored (record_implementation) + user-drawn anchors.
--
-- Confirmed matches already wrote their result into the underlying item
-- (terminal status + commit_sha) via the old confirm path, so that work
-- persists independently. Only the suggestion queue + the scan cursor are
-- dropped here.
DROP TABLE IF EXISTS implementation_matches;
ALTER TABLE projects DROP COLUMN matcher_last_scan_at;
