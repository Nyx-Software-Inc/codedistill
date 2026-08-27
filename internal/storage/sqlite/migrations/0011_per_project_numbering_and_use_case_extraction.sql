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

-- v0.7.x-dev: per-project per-type numbering + use-case structured extraction.
--
-- Adds a `number` INTEGER column to all four classifier-output tables. The
-- value is a 1-based per-project sequence, independent per type — UC-1, T-1,
-- B-1, KB-1 can coexist in the same project. Stored as a plain integer; the
-- "UC-" / "T-" / "B-" / "KB-" prefix is presentation-only and lives in the UI.
-- Today only USE_CASE surfaces the prefix to users; the others store numbers
-- so external-ID sync (Track B / MCP) and cross-references can attach later
-- without another migration.
--
-- Adds `role`, `want`, `why` TEXT columns to `use_case_items` only, populated
-- by the agent's classify pass when the LLM detects an "as a [role] I want X
-- so that Y" shape. Empty when the source doesn't convey a user story; the
-- original paste lives in `description` (unchanged) and is the durable record.
--
-- External identifiers (Linear keys, GitHub issue numbers, etc.) are *not*
-- added here. They land alongside MCP push/pull in Track B as a separate
-- `external_refs` table; today's numbering is the internal half only.

ALTER TABLE todo_items         ADD COLUMN number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE bug_items          ADD COLUMN number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_entries  ADD COLUMN number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE use_case_items     ADD COLUMN number INTEGER NOT NULL DEFAULT 0;

ALTER TABLE use_case_items ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE use_case_items ADD COLUMN want TEXT NOT NULL DEFAULT '';
ALTER TABLE use_case_items ADD COLUMN why  TEXT NOT NULL DEFAULT '';

-- Shadow fields on scratchpad_items mirror the existing proposed_category /
-- classification_reasoning pattern: agent-extracted user-story pieces survive
-- the pending-review round-trip so the Inbox Accept path can populate the
-- derived use_case_item without re-running the LLM. Empty for non-use-case
-- categories or when the source doesn't convey a Role/Want/Why structure.
ALTER TABLE scratchpad_items ADD COLUMN proposed_role TEXT NOT NULL DEFAULT '';
ALTER TABLE scratchpad_items ADD COLUMN proposed_want TEXT NOT NULL DEFAULT '';
ALTER TABLE scratchpad_items ADD COLUMN proposed_why  TEXT NOT NULL DEFAULT '';

-- Backfill numbers per (project_id, table) in (created_at, id) order. The
-- correlated-subquery COUNT pattern is fine under SQLite's single-writer
-- model and produces a 1-based dense sequence per project. Ties on created_at
-- are broken by id so the result is deterministic.
UPDATE todo_items SET number = (
    SELECT COUNT(*) FROM todo_items t2
    WHERE t2.project_id = todo_items.project_id
      AND (t2.created_at < todo_items.created_at
           OR (t2.created_at = todo_items.created_at AND t2.id <= todo_items.id))
);

UPDATE bug_items SET number = (
    SELECT COUNT(*) FROM bug_items b2
    WHERE b2.project_id = bug_items.project_id
      AND (b2.created_at < bug_items.created_at
           OR (b2.created_at = bug_items.created_at AND b2.id <= bug_items.id))
);

UPDATE knowledge_entries SET number = (
    SELECT COUNT(*) FROM knowledge_entries k2
    WHERE k2.project_id = knowledge_entries.project_id
      AND (k2.created_at < knowledge_entries.created_at
           OR (k2.created_at = knowledge_entries.created_at AND k2.id <= knowledge_entries.id))
);

UPDATE use_case_items SET number = (
    SELECT COUNT(*) FROM use_case_items u2
    WHERE u2.project_id = use_case_items.project_id
      AND (u2.created_at < use_case_items.created_at
           OR (u2.created_at = use_case_items.created_at AND u2.id <= use_case_items.id))
);

CREATE UNIQUE INDEX idx_todos_project_number     ON todo_items(project_id, number);
CREATE UNIQUE INDEX idx_bugs_project_number      ON bug_items(project_id, number);
CREATE UNIQUE INDEX idx_kb_project_number        ON knowledge_entries(project_id, number);
CREATE UNIQUE INDEX idx_use_cases_project_number ON use_case_items(project_id, number);
