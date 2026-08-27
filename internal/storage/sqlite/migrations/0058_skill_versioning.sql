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

-- 0058: Skill versioning + evidence-gated tie to the throughline (Pro).
--
-- Skills become versioned: every content/name change cuts a new immutable
-- version. A skill's use is tied to code ONLY on evidence — never blanket:
--   * skill_retrievals  — the raw fact that get_skill fetched a version (audit).
--   * skill_applications — a version bound to a specific change (owner + commit),
--                          labelled by how we know: 'attested' (the agent
--                          declared it via skills_used) or 'retrieved'. An empty
--                          set means "no skill was used" — the honest default.
-- The reverse index on (skill_id, skill_version) answers the remediation query:
-- "find every change made under version N of this skill."

-- Head-version pointer on the skill row. Existing skills start at v1.
ALTER TABLE skills ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

-- Immutable version history. Rows are append-only; the only removal is a
-- deliberate compliance purge (e.g. a secret pasted into a version).
CREATE TABLE skill_versions (
    id          TEXT PRIMARY KEY,
    skill_id    TEXT NOT NULL,
    version     INTEGER NOT NULL,
    name        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    author      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX idx_skill_versions_skill_ver ON skill_versions(skill_id, version);

-- Audit log of get_skill fetches. A fetch is a fact, not proof of use — kept
-- separate from applications so "retrieved" is never mistaken for "applied".
CREATE TABLE skill_retrievals (
    id            TEXT PRIMARY KEY,
    skill_id      TEXT NOT NULL,
    skill_version INTEGER NOT NULL,
    project_id    TEXT NOT NULL,
    actor         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL
);
CREATE INDEX idx_skill_retrievals_skill_ver ON skill_retrievals(skill_id, skill_version);

-- Evidence-gated binding of a skill version to a specific change. Written only
-- when there is evidence of use; owner_type/owner_id/commit_sha locate the
-- change so the throughline can reach the actual code.
CREATE TABLE skill_applications (
    id            TEXT PRIMARY KEY,
    skill_id      TEXT NOT NULL,
    skill_version INTEGER NOT NULL,
    owner_type    TEXT NOT NULL,
    owner_id      TEXT NOT NULL,
    commit_sha    TEXT NOT NULL DEFAULT '',
    evidence      TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL
);
CREATE INDEX idx_skill_applications_skill_ver ON skill_applications(skill_id, skill_version);
CREATE INDEX idx_skill_applications_owner ON skill_applications(owner_type, owner_id);

-- Backfill: every existing skill gets an initial v1 snapshot of its current
-- content, so history is complete from the first edit forward.
INSERT INTO skill_versions (id, skill_id, version, name, content, author, created_at)
SELECT id || ':1', id, 1, name, content, '', created_at FROM skills;
