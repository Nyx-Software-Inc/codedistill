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

-- v0.8.1-dev experiment/embeddings: code chunks indexer.
--
-- Each row is a sliding-window slice of a project's source file with a
-- vector embedding of its content. Used by:
--   - auto-anchor: cosine-rank chunks vs. paste content; suggest the
--     best file + line range as agent-suggested anchors instead of
--     guessing from path alone.
--   - future Q&A: retrieval-augmented context over the codebase.
--
-- Schema:
--   id                — UUID, primary key
--   project_id        — FK to projects (cascade delete)
--   file_path         — repo-relative, forward-slashed
--   line_start        — 1-indexed, inclusive
--   line_end          — 1-indexed, inclusive (line_end >= line_start)
--   content_hash      — fnv64 hex of chunk content. Indexer skips re-
--                       embedding when hash matches existing row.
--   content           — the chunk text itself, denormalized so search
--                       results can render snippets without re-reading
--                       files (~2KB per chunk × thousands manageable).
--   embedding         — float32 BLOB; null until first embed succeeds
--   embedded_at       — timestamp of last successful embed; null = never
--   created_at, updated_at — bookkeeping
--
-- Uniqueness: (project_id, file_path, line_start, line_end) is unique
-- so the indexer's upsert path is deterministic — re-indexing the same
-- region replaces in place rather than creating duplicates.
--
-- Cascade: ON DELETE CASCADE for project_id means deleting a project
-- removes its code chunks.

CREATE TABLE code_chunks (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    line_start    INTEGER NOT NULL,
    line_end      INTEGER NOT NULL,
    content_hash  TEXT NOT NULL,
    content       TEXT NOT NULL,
    embedding     BLOB,
    embedded_at   TIMESTAMP,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (project_id, file_path, line_start, line_end)
);

-- Lookup paths: by-project for project-wide search, by-file for re-index.
CREATE INDEX idx_code_chunks_project ON code_chunks (project_id);
CREATE INDEX idx_code_chunks_file    ON code_chunks (project_id, file_path);

-- Partial index over un-embedded chunks for the indexer's "next batch
-- to embed" queries (fed in chunks of N to keep memory bounded).
CREATE INDEX idx_code_chunks_unembedded
    ON code_chunks (project_id, created_at)
    WHERE embedded_at IS NULL;
