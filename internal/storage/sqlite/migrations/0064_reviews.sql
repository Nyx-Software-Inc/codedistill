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

-- 0064: reviews + changeset_commits — the review process (docs/design/
-- code-review.md, slice 5). One review per changeset: status open →
-- changes_requested → approved → merged | abandoned, and how many approvals
-- it needs (one in single-user mode, two in a multi-user workspace). The
-- decisions themselves are review_decisions rows with owner_type 'review', so
-- the existing table carries them. changeset_commits records which commits a
-- changeset covers, captured from git when the review is opened, so the
-- governance gate can ask "is this item's commit under an open review" with
-- SQL alone.
CREATE TABLE reviews (
    id                 TEXT PRIMARY KEY,
    changeset_id       TEXT NOT NULL UNIQUE REFERENCES changesets(id) ON DELETE CASCADE,
    project_id         TEXT NOT NULL,
    status             TEXT NOT NULL,
    required_approvals INTEGER NOT NULL DEFAULT 1,
    author_email       TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX idx_reviews_project_status ON reviews(project_id, status);

CREATE TABLE changeset_commits (
    changeset_id TEXT NOT NULL REFERENCES changesets(id) ON DELETE CASCADE,
    sha          TEXT NOT NULL,
    PRIMARY KEY (changeset_id, sha)
);
CREATE INDEX idx_changeset_commits_sha ON changeset_commits(sha);
