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

-- v0.8.1-dev — widen bug_items.status to support the loosened workflow.
--
-- Old CHECK constraint pinned status to:
--   open | investigating | in-progress | fixed | verified | closed
--
-- New API surface lets a bug also resolve as:
--   not_a_bug | wont_fix | duplicate
--
-- AND removes the strict forward-only transition rule (validBugTransition
-- in internal/api/bugs.go is now a no-op; the API validates membership
-- via validBugStatus but not order). Solo-dev workflow needs to flip
-- freely: jump from "open" to "not_a_bug" without walking the chain,
-- reopen a "fixed" bug, etc.
--
-- SQLite can't ALTER a CHECK constraint in place; rebuild the table.
-- The new table omits the status CHECK entirely — the API enforces the
-- valid set so a code change is sufficient to extend it next time.

PRAGMA foreign_keys = OFF;

CREATE TABLE bug_items_new (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_item_id     TEXT REFERENCES scratchpad_items(id) ON DELETE SET NULL,
    subject            TEXT NOT NULL,
    notes              TEXT,
    severity           TEXT NOT NULL DEFAULT 'minor'
        CHECK (severity IN ('critical', 'major', 'minor', 'trivial')),
    status             TEXT NOT NULL DEFAULT 'open',
    steps_to_reproduce TEXT,
    expected_behavior  TEXT,
    actual_behavior    TEXT,
    environment        TEXT,
    affected_component TEXT,
    origin             TEXT NOT NULL
        CHECK (origin IN ('manual', 'agent-derived')),
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    creator_id         TEXT NOT NULL DEFAULT 'local',
    visibility         TEXT NOT NULL DEFAULT 'project'
        CHECK (visibility IN ('private', 'project')),
    number             INTEGER NOT NULL DEFAULT 0,
    remote_id          TEXT NOT NULL DEFAULT '',
    sync_status        TEXT NOT NULL DEFAULT 'local-only'
        CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed')),
    last_sync_at       TIMESTAMP,
    last_sync_error    TEXT NOT NULL DEFAULT '',
    embedding          BLOB,
    embedded_at        TIMESTAMP,
    completed_at       TIMESTAMP,
    commit_sha         TEXT,
    commit_tag         TEXT
);

INSERT INTO bug_items_new SELECT
    id, project_id, source_item_id, subject, notes, severity, status,
    steps_to_reproduce, expected_behavior, actual_behavior,
    environment, affected_component, origin, created_at,
    creator_id, visibility, number,
    remote_id, sync_status, last_sync_at, last_sync_error,
    embedding, embedded_at, completed_at, commit_sha, commit_tag
FROM bug_items;

DROP TABLE bug_items;
ALTER TABLE bug_items_new RENAME TO bug_items;

CREATE INDEX idx_bugs_project              ON bug_items(project_id);
CREATE UNIQUE INDEX idx_bugs_project_number ON bug_items(project_id, number);
CREATE INDEX idx_bug_items_sync_status     ON bug_items(sync_status);
CREATE INDEX idx_bug_items_unembedded      ON bug_items(created_at) WHERE embedded_at IS NULL;

PRAGMA foreign_keys = ON;
