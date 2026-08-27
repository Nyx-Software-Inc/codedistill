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

-- Phase 2 schema migration: introduce Workspace as the multi-tenant root,
-- add Codebases (multi-repo per project), and visibility fields on
-- Scratchpads and derived items. Single-user installs auto-seed an
-- implicit "local" workspace + user so existing data flows through unchanged.
--
-- Visibility model — Option A: derived items inherit from source scratchpad.
-- Defaults set everything to "project" visibility so existing single-user
-- behavior is preserved (everything visible to the implicit local user).
--
-- SQLite ALTER TABLE limitation: a column added via ALTER TABLE cannot have
-- both a NOT NULL constraint and a REFERENCES clause unless the default is
-- NULL. Trade-off taken: NOT NULL DEFAULT 'local' (no FK) on the altered
-- columns; FKs declared on the new tables only. Application enforces the
-- referential integrity for the altered columns. A future migration can
-- rebuild these tables to add proper FK constraints.

-- ---- New tables ----

CREATE TABLE workspaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free'
        CHECK (plan IN ('free', 'team', 'enterprise')),
    seat_limit  INTEGER,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE users (
    id           TEXT PRIMARY KEY,
    email        TEXT,
    display_name TEXT NOT NULL,
    avatar       TEXT,
    -- external_id + provider plumb future SSO (Google, GitHub, generic OIDC).
    -- Both NULL means a "local" identity not backed by any provider.
    external_id  TEXT,
    provider     TEXT
        CHECK (provider IN ('local', 'google', 'github', 'oidc') OR provider IS NULL),
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_users_external ON users(provider, external_id);

CREATE TABLE workspace_members (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL DEFAULT 'member'
        CHECK (role IN ('owner', 'admin', 'member')),
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE TABLE project_members (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member'
        CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE codebases (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    repo_root  TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_codebases_project ON codebases(project_id);

-- ---- Seed the implicit local workspace + user ----

INSERT INTO workspaces (id, name, plan) VALUES ('local', 'Local', 'free');
INSERT INTO users (id, display_name, provider) VALUES ('local', 'Local User', 'local');
INSERT INTO workspace_members (workspace_id, user_id, role)
    VALUES ('local', 'local', 'owner');

-- ---- Extend existing tables ----

-- projects: tenant root pointer
ALTER TABLE projects ADD COLUMN workspace_id TEXT NOT NULL DEFAULT 'local';
CREATE INDEX idx_projects_workspace ON projects(workspace_id);

-- Backfill: each existing project becomes a member of 'local' as owner
INSERT INTO project_members (project_id, user_id, role)
SELECT id, 'local', 'owner' FROM projects;

-- Backfill: each existing project with a non-empty repo_root becomes a
-- single 'main' codebase row. id is derived deterministically from the
-- project id so re-running the migration would be a no-op (it isn't,
-- but defense in depth). NOTE: projects.repo_root column is retained for
-- now; reads should prefer the codebases table going forward and the
-- column will be dropped in a future migration.
INSERT INTO codebases (id, project_id, name, repo_root)
SELECT 'cb-' || id, id, 'main', repo_root
FROM projects
WHERE repo_root IS NOT NULL AND repo_root != '';

-- scratchpads: owner + visibility (private | project)
ALTER TABLE scratchpads ADD COLUMN owner_id TEXT NOT NULL DEFAULT 'local';
ALTER TABLE scratchpads ADD COLUMN visibility TEXT NOT NULL DEFAULT 'project'
    CHECK (visibility IN ('private', 'project'));
CREATE INDEX idx_scratchpads_owner ON scratchpads(owner_id);

-- todo_items: creator + visibility (inherits from source scratchpad at create-time)
ALTER TABLE todo_items ADD COLUMN creator_id TEXT NOT NULL DEFAULT 'local';
ALTER TABLE todo_items ADD COLUMN visibility TEXT NOT NULL DEFAULT 'project'
    CHECK (visibility IN ('private', 'project'));

-- bug_items: creator + visibility
ALTER TABLE bug_items ADD COLUMN creator_id TEXT NOT NULL DEFAULT 'local';
ALTER TABLE bug_items ADD COLUMN visibility TEXT NOT NULL DEFAULT 'project'
    CHECK (visibility IN ('private', 'project'));

-- knowledge_entries: creator + visibility
ALTER TABLE knowledge_entries ADD COLUMN creator_id TEXT NOT NULL DEFAULT 'local';
ALTER TABLE knowledge_entries ADD COLUMN visibility TEXT NOT NULL DEFAULT 'project'
    CHECK (visibility IN ('private', 'project'));

-- code_anchors: optional codebase_id (NULL = anchored against the project's
-- single/primary codebase, the common single-user case). Resolved at read
-- time by the API.
ALTER TABLE code_anchors ADD COLUMN codebase_id TEXT;
CREATE INDEX idx_code_anchors_codebase ON code_anchors(codebase_id);
