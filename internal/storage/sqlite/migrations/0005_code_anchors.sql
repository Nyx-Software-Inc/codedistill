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

-- Code anchors: structured pointers from any domain item (scratchpad item,
-- todo, bug, KB entry) to code — either a file range, a commit SHA, or a PR
-- URL. Polymorphic on (owner_type, owner_id); cascade on owner delete is
-- handled in Go alongside each Delete* method, not via FK.
--
-- projects.repo_root: optional absolute path to the project's git worktree.
-- Set to enable the Files panel and file-drop anchor modal (M-Anchor-2).

CREATE TABLE code_anchors (
    id          TEXT PRIMARY KEY,
    owner_type  TEXT NOT NULL
        CHECK (owner_type IN (
            'scratchpad_item', 'todo_item', 'bug_item', 'knowledge_entry'
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
        CHECK (provenance IN ('user-set', 'url-detected', 'file-dropped')),
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_code_anchors_owner ON code_anchors (owner_type, owner_id);

ALTER TABLE projects ADD COLUMN repo_root TEXT;
