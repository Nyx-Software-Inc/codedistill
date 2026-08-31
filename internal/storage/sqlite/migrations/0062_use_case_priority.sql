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

-- Use cases gain a priority, matching the todo vocabulary exactly.
--
-- Todos have had priority since 0001; bugs rank by severity
-- (critical/major/minor/trivial) and deliberately do NOT gain a second axis —
-- two overlapping rankings on one entity would leave every consumer inventing a
-- rule for "trivial bug at high priority". Bug severity is instead MAPPED onto
-- the same order for sorting (see domain.PriorityRank). Use cases had no ranking
-- field at all, which is the real gap.
--
-- Numbered 0062, not 0061: the Postgres set carries a PG-only
-- 0061_security_severity_double.sql, so 0061 is taken on that side. Using 0062
-- in BOTH directories keeps the one-for-one pairing that has held since 0046,
-- at the cost of a documented gap at SQLite 0061.

ALTER TABLE use_case_items ADD COLUMN priority TEXT NOT NULL DEFAULT 'none'
    CHECK (priority IN ('high', 'medium', 'low', 'none'));
