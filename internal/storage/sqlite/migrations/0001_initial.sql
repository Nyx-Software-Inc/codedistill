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

-- Phase 1 initial schema. Tables: projects, scratchpads, scratchpad_items,
-- todo_items, bug_items, knowledge_entries. Deferred: annotations, tags,
-- cross-references, attachments, extraction, settings per-user.

CREATE TABLE projects (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE scratchpads (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    classification_mode TEXT NOT NULL DEFAULT 'full'
        CHECK (classification_mode IN ('off', 'strict', 'full')),
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE scratchpad_items (
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
        CHECK (classification_override IN ('todo', 'bug', 'kb', 'skip')
               OR classification_override IS NULL),
    proposed_category         TEXT,
    classification_confidence REAL,
    classification_reasoning  TEXT,
    derived_item_id           TEXT,
    created_at                TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE todo_items (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_item_id TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject        TEXT NOT NULL,
    notes          TEXT,
    priority       TEXT NOT NULL DEFAULT 'none'
        CHECK (priority IN ('high', 'medium', 'low', 'none')),
    status         TEXT NOT NULL DEFAULT 'incomplete'
        CHECK (status IN ('incomplete', 'complete')),
    origin         TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at   TIMESTAMP
);

CREATE TABLE bug_items (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_item_id     TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject            TEXT NOT NULL,
    notes              TEXT,
    severity           TEXT NOT NULL DEFAULT 'minor'
        CHECK (severity IN ('critical', 'major', 'minor', 'trivial')),
    status             TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'investigating', 'in-progress',
                          'fixed', 'verified', 'closed')),
    steps_to_reproduce TEXT,
    expected_behavior  TEXT,
    actual_behavior    TEXT,
    environment        TEXT,
    affected_component TEXT,
    origin             TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE knowledge_entries (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_item_id TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    title          TEXT NOT NULL,
    content        TEXT NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_scratchpads_project    ON scratchpads(project_id);
CREATE INDEX idx_items_scratchpad       ON scratchpad_items(scratchpad_id);
CREATE INDEX idx_items_state            ON scratchpad_items(classification_state);
CREATE INDEX idx_todos_project          ON todo_items(project_id);
CREATE INDEX idx_bugs_project           ON bug_items(project_id);
CREATE INDEX idx_kb_project             ON knowledge_entries(project_id);
