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

-- v0.8.1-dev — implementation matcher (priority #3).
--
-- A background job walks each project's git log and ranks recent commits
-- against open todos / bugs / use_cases via embedding cosine + an LLM
-- judge. High-confidence pairs land here as 'pending' suggestions; the
-- user confirms (flips item status + auto-fills commit_sha via the
-- priority-#2 plumbing) or dismisses (never re-surfaced).
--
-- commit_message + commit_date are snapshotted because a rebase or
-- force-push can erase the commit from git log; the suggestion is still
-- meaningful to the user. cosine_score is always populated; llm_*
-- columns are null when the judge step failed or was skipped.
--
-- UNIQUE(project_id, commit_sha, item_type, item_id) makes the matcher
-- pass idempotent — re-running across overlapping commit ranges will
-- collide rather than duplicate. The matcher uses INSERT OR IGNORE.

CREATE TABLE implementation_matches (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    commit_sha      TEXT NOT NULL,
    commit_message  TEXT NOT NULL,
    commit_date     TIMESTAMP NOT NULL,
    item_type       TEXT NOT NULL,  -- todo_item | bug_item | use_case_item
    item_id         TEXT NOT NULL,
    cosine_score    REAL NOT NULL,
    llm_confidence  REAL,
    reasoning       TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending | confirmed | dismissed
    suggested_at    TIMESTAMP NOT NULL,
    resolved_at     TIMESTAMP,
    UNIQUE(project_id, commit_sha, item_type, item_id)
);

CREATE INDEX idx_im_pending
    ON implementation_matches(project_id, status, suggested_at DESC);

-- Per-project scan cursor. Timestamp-based rather than SHA-based so a
-- rebase doesn't strand us at a no-longer-existing commit. The matcher
-- lists commits with author_when > matcher_last_scan_at and the UNIQUE
-- constraint catches replays.
ALTER TABLE projects ADD COLUMN matcher_last_scan_at TIMESTAMP;
