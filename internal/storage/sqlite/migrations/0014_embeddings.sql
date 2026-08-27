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

-- v0.8.1-dev experiment/embeddings: add embedding columns to every item
-- table. Vectors are stored as a raw float32 BLOB (4 bytes per dim ×
-- 768 dims for nomic-embed-text = ~3KB per row). Cosine similarity is
-- computed in Go at search time — at solo-dev scale (1000s of items)
-- a linear scan finishes in milliseconds, so no sqlite-vec extension
-- is needed.
--
-- Column shape per table:
--   embedding     BLOB      — null means "not embedded yet"
--   embedded_at   TIMESTAMP — when we last embedded; null = never.
--                             Backfill walks rows where embedded_at IS
--                             NULL OR embedded_at < updated_at, so an
--                             update to content invalidates the embed
--                             implicitly (updated_at advances).
--
-- All five item tables get the same shape. Code anchors are paths
-- only; not embedded in this stage. Code-chunk embedding (for line-
-- range auto-anchor + Q&A) is a future stage with its own table.

ALTER TABLE scratchpad_items ADD COLUMN embedding   BLOB;
ALTER TABLE scratchpad_items ADD COLUMN embedded_at TIMESTAMP;

ALTER TABLE todo_items       ADD COLUMN embedding   BLOB;
ALTER TABLE todo_items       ADD COLUMN embedded_at TIMESTAMP;

ALTER TABLE bug_items        ADD COLUMN embedding   BLOB;
ALTER TABLE bug_items        ADD COLUMN embedded_at TIMESTAMP;

ALTER TABLE knowledge_entries ADD COLUMN embedding   BLOB;
ALTER TABLE knowledge_entries ADD COLUMN embedded_at TIMESTAMP;

ALTER TABLE use_case_items   ADD COLUMN embedding   BLOB;
ALTER TABLE use_case_items   ADD COLUMN embedded_at TIMESTAMP;

-- Partial index over un-embedded rows so the backfill scan stays
-- cheap as the corpus grows. SQLite uses partial indexes when the
-- WHERE clause matches; the backfill query mirrors this predicate.
-- todo_items / bug_items / knowledge_entries don't carry an updated_at
-- in the v1 schema (derived items were modeled as write-once with
-- only state changes); embedded_at < created_at can never be true so
-- we just index created_at for ordering. Adding updated_at to those
-- tables is its own migration if we ever want stale-content rebakes.
CREATE INDEX idx_scratchpad_items_unembedded ON scratchpad_items(updated_at) WHERE embedded_at IS NULL;
CREATE INDEX idx_todo_items_unembedded       ON todo_items(created_at)       WHERE embedded_at IS NULL;
CREATE INDEX idx_bug_items_unembedded        ON bug_items(created_at)        WHERE embedded_at IS NULL;
CREATE INDEX idx_knowledge_entries_unembedded ON knowledge_entries(created_at) WHERE embedded_at IS NULL;
CREATE INDEX idx_use_case_items_unembedded   ON use_case_items(updated_at)   WHERE embedded_at IS NULL;
