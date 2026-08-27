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

-- v0.7.0-dev: extend scratchpad_items.classification_override CHECK to allow
-- 'use_case' so the inbox Reclassify UI can re-route an agent's proposal to
-- USE_CASE.
--
-- SQLite cannot ALTER a CHECK constraint, so the table is rebuilt. Four
-- tables reference scratchpad_items(id) via FK on source_item_id
-- (todo_items, bug_items, knowledge_entries, use_case_items). The standard
-- workaround for rebuilding under FKs is `PRAGMA foreign_keys = OFF` outside
-- a transaction, but this migration runs inside the migrator's auto-tx and
-- foreign_keys is a no-op there. `PRAGMA defer_foreign_keys = ON` is the
-- inside-a-transaction equivalent: it defers FK enforcement to COMMIT, by
-- which point the new table has the same name as the old and all four
-- references resolve correctly.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE scratchpad_items_new (
    id                        TEXT PRIMARY KEY,
    scratchpad_id             TEXT NOT NULL REFERENCES scratchpads(id) ON DELETE CASCADE,
    content_type              TEXT NOT NULL
        CHECK (content_type IN ('text', 'code_snippet', 'link')),
    content                   TEXT NOT NULL,
    classification_state      TEXT NOT NULL DEFAULT 'unprocessed'
        CHECK (classification_state IN (
            'unprocessed', 'processing', 'classified',
            'pending-review', 'skipped', 'failed'
        )),
    skipped_reason            TEXT
        CHECK (skipped_reason IN (
            'strict_mode_low_confidence', 'user_rejected_from_inbox',
            'override_skip', 'derived_item_deleted'
        ) OR skipped_reason IS NULL),
    classification_override   TEXT
        CHECK (classification_override IN ('todo', 'bug', 'kb', 'skip', 'use_case')
               OR classification_override IS NULL),
    proposed_category         TEXT,
    classification_confidence REAL,
    classification_reasoning  TEXT,
    derived_item_id           TEXT,
    created_at                TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    grid_col                  INTEGER NOT NULL DEFAULT 0,
    grid_row                  INTEGER NOT NULL DEFAULT 0,
    grid_w                    INTEGER NOT NULL DEFAULT 12,
    grid_h                    INTEGER NOT NULL DEFAULT 4,
    hidden                    INTEGER NOT NULL DEFAULT 0,
    annotations               TEXT NOT NULL DEFAULT '',
    tags                      TEXT NOT NULL DEFAULT '[]',
    name                      TEXT NOT NULL DEFAULT ''
);

INSERT INTO scratchpad_items_new (
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name
) SELECT
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name
FROM scratchpad_items;

DROP TABLE scratchpad_items;
ALTER TABLE scratchpad_items_new RENAME TO scratchpad_items;

CREATE INDEX idx_items_scratchpad ON scratchpad_items(scratchpad_id);
CREATE INDEX idx_items_state      ON scratchpad_items(classification_state);
CREATE INDEX idx_items_hidden     ON scratchpad_items(scratchpad_id, hidden);
