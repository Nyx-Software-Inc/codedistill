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

-- Rich Canvas Slice 4: freehand sketches on scratchpad_items.
--
-- Widens the content_type CHECK to include 'sketch'. The sketch
-- scene (Excalidraw JSON) lives in the existing `content` column
-- — no blob bytes, no new columns. Excalidraw scene state is
-- JSON-serializable and typically <50KB for a real sketch, so
-- inline TEXT storage is fine.
--
-- Same table-rebuild pattern as migrations 0010 / 0022 / 0023.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE scratchpad_items_new (
    id                        TEXT PRIMARY KEY,
    scratchpad_id             TEXT NOT NULL REFERENCES scratchpads(id) ON DELETE CASCADE,
    content_type              TEXT NOT NULL
        CHECK (content_type IN ('text', 'code_snippet', 'link', 'image', 'file', 'sketch')),
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
    og_fetched_at             TIMESTAMP
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
