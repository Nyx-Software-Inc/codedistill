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

-- Rich Canvas Slice 6: groups as canvas objects.
--
-- A group is itself a scratchpad_item with content_type='group'.
-- Other items reference their containing group via the new
-- group_id column (FK to scratchpad_items.id). Single-membership
-- model: an item is in at most one group at a time.
--
-- ON DELETE SET NULL: deleting a group orphans its children
-- rather than cascading. Application code (the API handler) is
-- responsible for the inverse cleanup: when an item's group_id
-- changes such that the previous group has no remaining
-- children, the empty group is auto-deleted.
--
-- New columns:
--   group_id   — FK to the parent group's id. NULL for ungrouped
--                items and for the groups themselves.
--   collapsed  — 0/1 boolean. Only meaningful when
--                content_type='group'. UI hides the group's
--                children when set.
--
-- Why polymorphic ('group' as a content_type, not a sibling
-- table): keeps the canvas object model unified. Existing
-- list/get/move/delete plumbing works for groups without forks.
-- The 'content' column is empty for groups (their identity is
-- name + membership, not text).
--
-- Same table-rebuild pattern as migrations 0010 / 0022 / 0023 /
-- 0025 / 0026.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE scratchpad_items_new (
    id                        TEXT PRIMARY KEY,
    scratchpad_id             TEXT NOT NULL REFERENCES scratchpads(id) ON DELETE CASCADE,
    content_type              TEXT NOT NULL
        CHECK (content_type IN ('text', 'code_snippet', 'link', 'image', 'file', 'sketch', 'composite', 'group')),
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
    name                      TEXT NOT NULL DEFAULT '',
    proposed_role             TEXT NOT NULL DEFAULT '',
    proposed_want             TEXT NOT NULL DEFAULT '',
    proposed_why              TEXT NOT NULL DEFAULT '',
    embedding                 BLOB,
    embedded_at               TIMESTAMP,
    similar_to_id             TEXT,
    similarity_score          REAL,
    blob_sha                  TEXT,
    mime_type                 TEXT,
    file_name                 TEXT,
    byte_size                 INTEGER,
    width                     INTEGER,
    height                    INTEGER,
    og_title                  TEXT,
    og_description            TEXT,
    og_image_sha              TEXT,
    og_fetched_at             TIMESTAMP,
    -- Slice 6 columns.
    group_id                  TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    collapsed                 INTEGER NOT NULL DEFAULT 0
);

INSERT INTO scratchpad_items_new (
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name,
    proposed_role, proposed_want, proposed_why,
    embedding, embedded_at, similar_to_id, similarity_score,
    blob_sha, mime_type, file_name, byte_size, width, height,
    og_title, og_description, og_image_sha, og_fetched_at
) SELECT
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name,
    proposed_role, proposed_want, proposed_why,
    embedding, embedded_at, similar_to_id, similarity_score,
    blob_sha, mime_type, file_name, byte_size, width, height,
    og_title, og_description, og_image_sha, og_fetched_at
FROM scratchpad_items;

DROP TABLE scratchpad_items;
ALTER TABLE scratchpad_items_new RENAME TO scratchpad_items;

CREATE INDEX idx_items_scratchpad           ON scratchpad_items(scratchpad_id);
CREATE INDEX idx_items_state                ON scratchpad_items(classification_state);
CREATE INDEX idx_items_hidden               ON scratchpad_items(scratchpad_id, hidden);
CREATE INDEX idx_scratchpad_items_unembedded ON scratchpad_items(updated_at) WHERE embedded_at IS NULL;
CREATE INDEX idx_items_blob_sha             ON scratchpad_items(blob_sha) WHERE blob_sha IS NOT NULL;
CREATE INDEX idx_items_og_image_sha         ON scratchpad_items(og_image_sha) WHERE og_image_sha IS NOT NULL;
CREATE INDEX idx_items_composite            ON scratchpad_items(content_type)
    WHERE content_type = 'composite';
-- Backs the API handler's "is this group empty?" lookup after
-- moving an item out — fast scan of children given a parent id.
CREATE INDEX idx_items_group_id             ON scratchpad_items(group_id)
    WHERE group_id IS NOT NULL;
