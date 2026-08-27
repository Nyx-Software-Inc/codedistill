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

-- Allow scratchpad items to be hidden from the canvas without deleting.
-- Hidden items preserve grid_col/row/w/h so unhide restores them in place.
-- Hidden items are also skipped by NextAvailableGridRow so the canvas
-- compacts when items are hidden.

ALTER TABLE scratchpad_items ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_items_hidden ON scratchpad_items(scratchpad_id, hidden);
