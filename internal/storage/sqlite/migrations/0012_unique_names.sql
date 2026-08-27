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

-- v0.8.0-dev: names become canonical identity at the workspace / project /
-- scratchpad / codebase level. Adds UNIQUE indexes so the schema enforces
-- what was previously only a UI convention. MCP tools (next patch) take
-- names instead of opaque ids; this migration is the foundation.
--
-- Pre-existing collisions are auto-resolved before the indexes go in: the
-- oldest row by (created_at, id) keeps its name; later rows in the same
-- scope get -2, -3, -4 suffixes appended. The user can rename them after
-- the fact via the UI; the suffixes are documented in CHANGELOG so a
-- "why is my pad called main-2?" is answerable.
--
-- SQLite supports window functions since 3.25 and CTEs in UPDATE; the
-- modernc.org/sqlite driver bundles a recent enough version. The pattern
-- below is portable to Postgres if/when that storage backend lands.
--
-- No table rebuild needed — UNIQUE INDEX is additive in SQLite. FK
-- references on these tables are by id, not by name, so renames don't
-- cascade and don't break data integrity.

-- 1. Workspaces — UNIQUE(name). Workspace is the multi-tenant root; name
-- is global (no parent to scope under).
WITH dupes AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY name ORDER BY created_at, id
    ) AS rn
    FROM workspaces
)
UPDATE workspaces
SET name = workspaces.name || '-' || (
    SELECT rn FROM dupes WHERE dupes.id = workspaces.id
)
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

CREATE UNIQUE INDEX idx_workspaces_name ON workspaces(name);

-- 2. Projects — UNIQUE(workspace_id, name). Two projects in the same
-- workspace can't share a name; same name across workspaces is fine.
WITH dupes AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY workspace_id, name ORDER BY created_at, id
    ) AS rn
    FROM projects
)
UPDATE projects
SET name = projects.name || '-' || (
    SELECT rn FROM dupes WHERE dupes.id = projects.id
)
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

CREATE UNIQUE INDEX idx_projects_workspace_name ON projects(workspace_id, name);

-- 3. Scratchpads — UNIQUE(project_id, name). Same name across projects
-- is fine; common case is "main" in every project.
WITH dupes AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY project_id, name ORDER BY created_at, id
    ) AS rn
    FROM scratchpads
)
UPDATE scratchpads
SET name = scratchpads.name || '-' || (
    SELECT rn FROM dupes WHERE dupes.id = scratchpads.id
)
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

CREATE UNIQUE INDEX idx_scratchpads_project_name ON scratchpads(project_id, name);

-- 4. Codebases — UNIQUE(project_id, name). Multi-codebase projects can
-- have a "frontend" and "backend"; not two "frontend"s.
WITH dupes AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY project_id, name ORDER BY created_at, id
    ) AS rn
    FROM codebases
)
UPDATE codebases
SET name = codebases.name || '-' || (
    SELECT rn FROM dupes WHERE dupes.id = codebases.id
)
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

CREATE UNIQUE INDEX idx_codebases_project_name ON codebases(project_id, name);
