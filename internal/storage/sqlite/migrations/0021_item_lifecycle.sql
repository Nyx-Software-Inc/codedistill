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

-- v0.8.3-dev — item lifecycle: in_progress + done states + claim tracking.
--
-- Adds:
--   * todo_items:        new statuses (in_progress, abandoned), claim columns
--   * use_case_items:    new status  (in_progress),             claim columns
--   * bug_items:                                                 claim columns
--   * knowledge_entries: new column status (active, deprecated), claim columns
--
-- Status CHECKs on todo + use_case are dropped entirely (mirroring the
-- bug_items widening done in 0020). The Go-side validators in
-- internal/api are now the single source of truth — additive status
-- changes won't need another schema migration.
--
-- Claim model: when an agent (or a user) starts work on an item, the
-- API flips status → in_progress, sets claimed_by + claimed_at. Both
-- columns survive the move to a terminal status as a historical
-- record (so the UI can show "completed by Claude on …").

PRAGMA foreign_keys = OFF;

-- ===== todo_items: drop status CHECK, add claim columns =====

CREATE TABLE todo_items_new (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_item_id  TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject         TEXT NOT NULL,
    notes           TEXT,
    priority        TEXT NOT NULL DEFAULT 'none'
        CHECK (priority IN ('high', 'medium', 'low', 'none')),
    status          TEXT NOT NULL DEFAULT 'incomplete',
    origin          TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at    TIMESTAMP,
    creator_id      TEXT NOT NULL DEFAULT 'local',
    visibility      TEXT NOT NULL DEFAULT 'project'
        CHECK (visibility IN ('private', 'project')),
    number          INTEGER NOT NULL DEFAULT 0,
    remote_id       TEXT NOT NULL DEFAULT '',
    sync_status     TEXT NOT NULL DEFAULT 'local-only'
        CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed')),
    last_sync_at    TIMESTAMP,
    last_sync_error TEXT NOT NULL DEFAULT '',
    embedding       BLOB,
    embedded_at     TIMESTAMP,
    commit_sha      TEXT,
    commit_tag      TEXT,
    claimed_by      TEXT NOT NULL DEFAULT '',
    claimed_at      TIMESTAMP
);

INSERT INTO todo_items_new (
    id, project_id, source_item_id, subject, notes, priority, status, origin,
    created_at, completed_at, creator_id, visibility, number,
    remote_id, sync_status, last_sync_at, last_sync_error,
    embedding, embedded_at, commit_sha, commit_tag
)
SELECT
    id, project_id, source_item_id, subject, notes, priority, status, origin,
    created_at, completed_at, creator_id, visibility, number,
    remote_id, sync_status, last_sync_at, last_sync_error,
    embedding, embedded_at, commit_sha, commit_tag
FROM todo_items;

DROP TABLE todo_items;
ALTER TABLE todo_items_new RENAME TO todo_items;

CREATE INDEX idx_todos_project              ON todo_items(project_id);
CREATE UNIQUE INDEX idx_todos_project_number ON todo_items(project_id, number);
CREATE INDEX idx_todo_items_sync_status     ON todo_items(sync_status);
CREATE INDEX idx_todo_items_unembedded      ON todo_items(created_at) WHERE embedded_at IS NULL;
-- Helps the default "open todos" list view filter out done rows cheaply.
CREATE INDEX idx_todos_project_status       ON todo_items(project_id, status);

-- ===== use_case_items: drop status CHECK, add claim columns =====

CREATE TABLE use_case_items_new (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    creator_id          TEXT NOT NULL DEFAULT 'local',
    visibility          TEXT NOT NULL DEFAULT 'project'
        CHECK (visibility IN ('private', 'project')),
    source_item_id      TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject             TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'proposed',
    target_release      TEXT NOT NULL DEFAULT '',
    implementation_date TIMESTAMP,
    commit_sha          TEXT NOT NULL DEFAULT '',
    commit_tag          TEXT NOT NULL DEFAULT '',
    origin              TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    number              INTEGER NOT NULL DEFAULT 0,
    role                TEXT NOT NULL DEFAULT '',
    want                TEXT NOT NULL DEFAULT '',
    why                 TEXT NOT NULL DEFAULT '',
    remote_id           TEXT NOT NULL DEFAULT '',
    sync_status         TEXT NOT NULL DEFAULT 'local-only'
        CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed')),
    last_sync_at        TIMESTAMP,
    last_sync_error     TEXT NOT NULL DEFAULT '',
    embedding           BLOB,
    embedded_at         TIMESTAMP,
    claimed_by          TEXT NOT NULL DEFAULT '',
    claimed_at          TIMESTAMP
);

INSERT INTO use_case_items_new (
    id, project_id, creator_id, visibility, source_item_id, subject,
    description, status, target_release, implementation_date,
    commit_sha, commit_tag, origin, created_at, updated_at, number,
    role, want, why,
    remote_id, sync_status, last_sync_at, last_sync_error,
    embedding, embedded_at
)
SELECT
    id, project_id, creator_id, visibility, source_item_id, subject,
    description, status, target_release, implementation_date,
    commit_sha, commit_tag, origin, created_at, updated_at, number,
    role, want, why,
    remote_id, sync_status, last_sync_at, last_sync_error,
    embedding, embedded_at
FROM use_case_items;

DROP TABLE use_case_items;
ALTER TABLE use_case_items_new RENAME TO use_case_items;

CREATE INDEX idx_use_cases_project              ON use_case_items(project_id);
CREATE INDEX idx_use_cases_status               ON use_case_items(project_id, status);
CREATE UNIQUE INDEX idx_use_cases_project_number ON use_case_items(project_id, number);
CREATE INDEX idx_use_case_items_sync_status     ON use_case_items(sync_status);
CREATE INDEX idx_use_case_items_unembedded      ON use_case_items(updated_at) WHERE embedded_at IS NULL;

-- ===== bug_items: just add claim columns (CHECK already dropped in 0020) =====

ALTER TABLE bug_items ADD COLUMN claimed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_items ADD COLUMN claimed_at TIMESTAMP;

-- ===== knowledge_entries: add status, claim columns =====
--
-- KB had no lifecycle today; status defaults to 'active'. CHECK is
-- enforced at the schema level here — the value set is small and
-- closed-by-design (deprecated == "kept for history, hidden by
-- default"), unlike todo/use_case which the user may want to extend.

ALTER TABLE knowledge_entries ADD COLUMN status     TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'deprecated'));
ALTER TABLE knowledge_entries ADD COLUMN claimed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_entries ADD COLUMN claimed_at TIMESTAMP;

-- Helps the default "active KB" list view filter out deprecated rows cheaply.
CREATE INDEX idx_kb_project_status ON knowledge_entries(project_id, status);

PRAGMA foreign_keys = ON;
