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

-- v0.7.0-dev: USE_CASE classification.
--
-- Adds the use_case_items table for the new fourth classifier output, and
-- extends code_anchors.owner_type to allow anchors to point at use_case_items
-- (for "where does this capability live in the code" tracking, and for the
-- forthcoming MCP write-back of agent-suggested implementation locations).
--
-- The provenance CHECK on code_anchors also gets a fourth value,
-- 'agent-suggested', for anchors created by an LLM via MCP — these are
-- proposed locations the user confirms or edits.
--
-- The scratchpad_items.classification_override CHECK is intentionally NOT
-- updated here. The agent path writes to proposed_category (no CHECK), and
-- rebuilding scratchpad_items is awkward inside the migration transaction
-- because it has incoming FK references from the three derived-item tables
-- and SQLite's foreign_keys pragma cannot be toggled inside a transaction.
-- A manual override-to-USE_CASE can ship in a separate migration if needed.

CREATE TABLE use_case_items (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    creator_id          TEXT NOT NULL DEFAULT 'local',
    visibility          TEXT NOT NULL DEFAULT 'project'
        CHECK (visibility IN ('private', 'project')),
    source_item_id      TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject             TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'proposed'
        CHECK (status IN ('proposed', 'implemented', 'abandoned')),
    target_release      TEXT NOT NULL DEFAULT '',
    implementation_date TIMESTAMP,
    commit_sha          TEXT NOT NULL DEFAULT '',
    commit_tag          TEXT NOT NULL DEFAULT '',
    origin              TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_use_cases_project ON use_case_items(project_id);
CREATE INDEX idx_use_cases_status  ON use_case_items(project_id, status);

-- code_anchors rebuild: extend owner_type and provenance CHECKs.
-- Safe inside the migration transaction because code_anchors has no
-- incoming FK references (cascades are handled in Go) and no outgoing
-- FKs (codebase_id is intentionally a plain TEXT column).

CREATE TABLE code_anchors_new (
    id          TEXT PRIMARY KEY,
    owner_type  TEXT NOT NULL
        CHECK (owner_type IN (
            'scratchpad_item', 'todo_item', 'bug_item',
            'knowledge_entry', 'use_case_item'
        )),
    owner_id    TEXT NOT NULL,
    kind        TEXT NOT NULL
        CHECK (kind IN ('file', 'commit', 'pr')),
    path        TEXT,
    line_start  INTEGER,
    line_end    INTEGER,
    revision    TEXT,
    url         TEXT,
    label       TEXT,
    provenance  TEXT NOT NULL
        CHECK (provenance IN (
            'user-set', 'url-detected', 'file-dropped', 'agent-suggested'
        )),
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    codebase_id TEXT
);

INSERT INTO code_anchors_new (
    id, owner_type, owner_id, kind, path, line_start, line_end,
    revision, url, label, provenance, created_at, updated_at, codebase_id
) SELECT
    id, owner_type, owner_id, kind, path, line_start, line_end,
    revision, url, label, provenance, created_at, updated_at, codebase_id
FROM code_anchors;

DROP TABLE code_anchors;
ALTER TABLE code_anchors_new RENAME TO code_anchors;

CREATE INDEX idx_code_anchors_owner    ON code_anchors (owner_type, owner_id);
CREATE INDEX idx_code_anchors_codebase ON code_anchors (codebase_id);
