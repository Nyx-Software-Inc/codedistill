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

-- Rich Canvas Slice 1: image support on scratchpad_items.
--
-- Widens the content_type CHECK to include 'image' and adds six
-- columns for binary-content metadata. All new columns are NULL-able
-- so existing text/code_snippet/link rows migrate as a SELECT-with-NULLs.
--
-- See docs/rich-canvas-design.md for the full architecture. The
-- BlobStore interface (internal/blobstore) stores the bytes; this
-- migration only adds the schema hook items use to reference them.
--
-- Subsequent slices widen the CHECK further: 'file' (Slice 2),
-- 'sketch' (Slice 4). URL embeds (Slice 3) extend the existing 'link'
-- type with og_* metadata columns — separate migration.
--
-- SQLite cannot ALTER a CHECK constraint, so the table is rebuilt.
-- Four tables FK source_item_id → scratchpad_items(id): todo_items,
-- bug_items, knowledge_entries, use_case_items. PRAGMA
-- defer_foreign_keys = ON defers FK enforcement to COMMIT, by which
-- point the new table has the same name and the FKs resolve cleanly.
-- (PRAGMA foreign_keys = OFF is the standard advice but it's a no-op
-- inside the migrator's transaction; defer_foreign_keys is the
-- inside-a-tx equivalent. Same pattern as migration 0010.)

PRAGMA defer_foreign_keys = ON;

CREATE TABLE scratchpad_items_new (
    id                        TEXT PRIMARY KEY,
    scratchpad_id             TEXT NOT NULL REFERENCES scratchpads(id) ON DELETE CASCADE,
    content_type              TEXT NOT NULL
        CHECK (content_type IN ('text', 'code_snippet', 'link', 'image')),
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
    -- Use-case extraction columns (migration 0011). Populated when
    -- the classifier proposes a USE_CASE; empty strings otherwise.
    proposed_role             TEXT NOT NULL DEFAULT '',
    proposed_want             TEXT NOT NULL DEFAULT '',
    proposed_why              TEXT NOT NULL DEFAULT '',
    -- Embedding columns (migration 0014).
    embedding                 BLOB,
    embedded_at               TIMESTAMP,
    -- Dedup columns (migration 0015).
    similar_to_id             TEXT,
    similarity_score          REAL,
    -- Rich-canvas blob columns (this migration). NULL for text /
    -- code_snippet / link rows; populated for binary content types.
    -- blob_sha is the SHA-256 hex digest the BlobStore returned when
    -- the bytes were ingested — the cross-reference into the
    -- content-addressed store. mime_type lets the HTTP layer set
    -- Content-Type without re-sniffing. width/height are intrinsic
    -- pixel dimensions (images only; null for other binary types).
    -- file_name preserves the original upload filename for UX and
    -- download Content-Disposition. byte_size is denormalized from
    -- the blob's actual size so list views don't need to round-trip
    -- to the store.
    blob_sha                  TEXT,
    mime_type                 TEXT,
    file_name                 TEXT,
    byte_size                 INTEGER,
    width                     INTEGER,
    height                    INTEGER
);

INSERT INTO scratchpad_items_new (
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name,
    proposed_role, proposed_want, proposed_why,
    embedding, embedded_at, similar_to_id, similarity_score
) SELECT
    id, scratchpad_id, content_type, content,
    classification_state, skipped_reason, classification_override,
    proposed_category, classification_confidence, classification_reasoning,
    derived_item_id, created_at, updated_at,
    grid_col, grid_row, grid_w, grid_h, hidden, annotations, tags, name,
    proposed_role, proposed_want, proposed_why,
    embedding, embedded_at, similar_to_id, similarity_score
FROM scratchpad_items;

DROP TABLE scratchpad_items;
ALTER TABLE scratchpad_items_new RENAME TO scratchpad_items;

-- Recreate the indexes that existed on the old table.
CREATE INDEX idx_items_scratchpad           ON scratchpad_items(scratchpad_id);
CREATE INDEX idx_items_state                ON scratchpad_items(classification_state);
CREATE INDEX idx_items_hidden               ON scratchpad_items(scratchpad_id, hidden);
CREATE INDEX idx_scratchpad_items_unembedded ON scratchpad_items(updated_at) WHERE embedded_at IS NULL;

-- New partial index: backs the GC sweeper's "what blobs are live"
-- query (SELECT DISTINCT blob_sha FROM scratchpad_items WHERE
-- blob_sha IS NOT NULL). Partial because most rows are text and would
-- waste index space if included.
CREATE INDEX idx_items_blob_sha ON scratchpad_items(blob_sha) WHERE blob_sha IS NOT NULL;
