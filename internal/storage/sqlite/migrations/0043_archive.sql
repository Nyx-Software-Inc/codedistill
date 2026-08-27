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

-- v0.12.x — archive feature (backlog item #19).
--
-- Archived scratchpad items are kept in full but removed from the canvas; they
-- stay searchable and can be un-archived. Distinct from `hidden` (which is
-- transient canvas-compaction): archived_at marks a deliberate lifecycle state.
ALTER TABLE scratchpad_items ADD COLUMN archived_at TIMESTAMP;
