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

-- 0063: changesets + review_comments — the write side of code review
-- (docs/design/code-review.md, slice 4). A changeset is base..head, stored
-- canonical (full SHAs; head "working" for the working copy with base = the
-- HEAD it was reviewed against) so the same commit picked two ways is one row.
-- A comment is anchored to (changeset, path, side, line_start..line_end):
-- side is old|new, because a remark about a deleted line is a remark about
-- the deletion. promoted_owner_* names the bug/todo a comment became, if any;
-- the item carries the code anchor and the changeset as provenance.
CREATE TABLE changesets (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    kind        TEXT NOT NULL,
    base_sha    TEXT NOT NULL DEFAULT '',
    head_sha    TEXT NOT NULL,
    ref         TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL,
    UNIQUE (project_id, base_sha, head_sha)
);

CREATE TABLE review_comments (
    id                  TEXT PRIMARY KEY,
    changeset_id        TEXT NOT NULL REFERENCES changesets(id) ON DELETE CASCADE,
    path                TEXT NOT NULL,
    side                TEXT NOT NULL,
    line_start          INTEGER NOT NULL,
    line_end            INTEGER NOT NULL,
    body                TEXT NOT NULL,
    author              TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    resolved_at         TIMESTAMP,
    promoted_owner_type TEXT NOT NULL DEFAULT '',
    promoted_owner_id   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_review_comments_changeset ON review_comments(changeset_id, path);
