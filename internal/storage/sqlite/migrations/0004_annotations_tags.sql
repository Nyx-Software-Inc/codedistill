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

-- Promotes "annotations" and "tags" from the deferred list in 0001_initial.sql.
-- annotations: free-form TEXT notes on a scratchpad item.
-- tags: JSON text array of normalized (lowercased, trimmed, unique) tag strings.
-- Empty-string annotations and '[]' tags are the zero values; NOT NULL to keep
-- the Go scan path simple (no sql.Null* wrappers needed).

ALTER TABLE scratchpad_items ADD COLUMN annotations TEXT NOT NULL DEFAULT '';
ALTER TABLE scratchpad_items ADD COLUMN tags        TEXT NOT NULL DEFAULT '[]';
