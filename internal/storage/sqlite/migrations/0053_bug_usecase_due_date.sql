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

-- 0053: optional due date on bugs + use cases (UC-46 — drag an item onto a day
-- in the Calendar view to assign/reschedule its due date). Mirrors the todos
-- due_date column added in 0032.
ALTER TABLE bug_items ADD COLUMN due_date TIMESTAMP;
ALTER TABLE use_case_items ADD COLUMN due_date TIMESTAMP;
