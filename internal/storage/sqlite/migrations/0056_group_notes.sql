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

-- 0056: group notes get their own store (item-editing canvas rework, slice C2).
-- Group frames stored their note in scratchpad_items.annotations, which blocks
-- retiring that column. Give groups a dedicated table and copy the existing
-- notes over. Additive; the annotations column stays for now (dual-written on
-- save) and is dropped in a later migration.
CREATE TABLE group_notes (
    group_id   TEXT PRIMARY KEY,
    note       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL
);

INSERT INTO group_notes (group_id, note, updated_at)
SELECT id, annotations, updated_at
FROM scratchpad_items
WHERE content_type = 'group' AND annotations IS NOT NULL AND trim(annotations) <> '';
