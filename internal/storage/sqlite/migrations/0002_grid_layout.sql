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

-- Adds grid-layout attributes to scratchpad_items so the UI can place
-- items freely (drag + resize, no overlap) on a snap-to-grid canvas.
-- 24-column grid; row units are CSS pixels divided by row_height on the client.
-- Existing rows are stacked vertically at full-ish width in creation order.

ALTER TABLE scratchpad_items ADD COLUMN grid_col INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scratchpad_items ADD COLUMN grid_row INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scratchpad_items ADD COLUMN grid_w   INTEGER NOT NULL DEFAULT 12;
ALTER TABLE scratchpad_items ADD COLUMN grid_h   INTEGER NOT NULL DEFAULT 4;

-- Backfill: stack existing items per-scratchpad in creation order.
-- Each item gets row = idx * grid_h, full starting width (12 cols).
UPDATE scratchpad_items
SET grid_row = (
    SELECT (row_number - 1) * 4
    FROM (
        SELECT id,
               ROW_NUMBER() OVER (PARTITION BY scratchpad_id ORDER BY created_at) AS row_number
        FROM scratchpad_items
    ) AS t
    WHERE t.id = scratchpad_items.id
);
