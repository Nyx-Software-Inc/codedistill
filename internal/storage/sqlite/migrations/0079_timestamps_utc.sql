-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Rewrite every stored timestamp as UTC in one sortable format.
--
-- Until now the driver wrote time.Time values with Go's time.String():
--
--     2026-06-20 08:29:09.146354825 -0400 EDT m=+376.149400620
--     2026-08-03 21:34:48.9653829 +0000 UTC
--
-- Reading those back works, but SQLite compares them as text, so ORDER BY and
-- range filters on a timestamp column were quietly wrong whenever rows mixed
-- zones (UTC next to EDT, EDT next to EST after a clock change). The store now
-- opens with _time_format=sqlite and _timezone=UTC (sqlite.go connString), so
-- new writes look like
--
--     2026-06-20 12:29:09.146354825+00:00
--
-- and this converts the old rows to match. The instant is unchanged; only its
-- spelling is. Rows already in another form (SQLite's own CURRENT_TIMESTAMP,
-- which is UTC already) do not match the pattern and are left alone.
--
-- Each distinct value is converted once in a scratch table, then every
-- TIMESTAMP column is pointed at the result. The CASTs matter: a TIMESTAMP
-- column has NUMERIC affinity, and comparing it straight to the TEXT key stops
-- SQLite using the key's index, which turns each UPDATE into rows x values
-- (minutes on a real database, at startup). The column list is every column
-- declared TIMESTAMP as of migration 0078.

CREATE TEMP TABLE ts_fix (
    old  TEXT PRIMARY KEY,
    frac TEXT,  -- ".146354825", or '' when the value has no fraction
    off  TEXT,  -- "-0400"
    new  TEXT
);

INSERT OR IGNORE INTO ts_fix (old)
      SELECT "created_at" FROM "acceptance_criteria" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "acceptance_criteria" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "finished_at" FROM "analysis_scans" WHERE "finished_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "started_at" FROM "analysis_scans" WHERE "started_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "api_tokens" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_used_at" FROM "api_tokens" WHERE "last_used_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "architecture_edges" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "architecture_nodes" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "architecture_nodes" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "claimed_at" FROM "bug_items" WHERE "claimed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "completed_at" FROM "bug_items" WHERE "completed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "bug_items" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "due_date" FROM "bug_items" WHERE "due_date" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "bug_items" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_sync_at" FROM "bug_items" WHERE "last_sync_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "code_anchors" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "code_anchors" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "code_chunks" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embed_failed_at" FROM "code_chunks" WHERE "embed_failed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "code_chunks" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "code_chunks" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "first_seen_at" FROM "code_findings" WHERE "first_seen_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_seen_at" FROM "code_findings" WHERE "last_seen_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "resolved_at" FROM "code_findings" WHERE "resolved_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "codebases" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "custom_field_defs" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "custom_field_defs" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "custom_field_values" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "decompose_moves" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "decompose_passes" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "decompose_proposals" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "decided_at" FROM "decompose_proposals" WHERE "decided_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "decompose_runs" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "source_modified" FROM "decompose_runs" WHERE "source_modified" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "group_notes" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "item_dependencies" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "item_events" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "finished_at" FROM "job_phases" WHERE "finished_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "started_at" FROM "job_phases" WHERE "started_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "finished_at" FROM "jobs" WHERE "finished_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "started_at" FROM "jobs" WHERE "started_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "jobs" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "claimed_at" FROM "knowledge_entries" WHERE "claimed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "knowledge_entries" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "knowledge_entries" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_sync_at" FROM "knowledge_entries" WHERE "last_sync_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "mcp_export_queue" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "next_attempt_at" FROM "mcp_export_queue" WHERE "next_attempt_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "mcp_export_queue" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "model_providers" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_ok_at" FROM "model_providers" WHERE "last_ok_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "model_providers" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "project_members" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "project_settings" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "projects" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "review_decisions" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "cleared_at" FROM "review_flags" WHERE "cleared_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "review_flags" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "applied_at" FROM "schema_migrations" WHERE "applied_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "archived_at" FROM "scratchpad_items" WHERE "archived_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "scratchpad_items" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "scratchpad_items" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "og_fetched_at" FROM "scratchpad_items" WHERE "og_fetched_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "scratchpad_items" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "scratchpads" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "sessions" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "expires_at" FROM "sessions" WHERE "expires_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_active_at" FROM "sessions" WHERE "last_active_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "skill_applications" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "skill_retrievals" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "skill_versions" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "skills" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "skills" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "claimed_at" FROM "todo_items" WHERE "claimed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "completed_at" FROM "todo_items" WHERE "completed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "todo_items" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "due_date" FROM "todo_items" WHERE "due_date" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "todo_items" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_sync_at" FROM "todo_items" WHERE "last_sync_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "claimed_at" FROM "use_case_items" WHERE "claimed_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "use_case_items" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "due_date" FROM "use_case_items" WHERE "due_date" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "embedded_at" FROM "use_case_items" WHERE "embedded_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "implementation_date" FROM "use_case_items" WHERE "implementation_date" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "last_sync_at" FROM "use_case_items" WHERE "last_sync_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "use_case_items" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "user_settings" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "users" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "verification_results" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "verification_results" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "workflow_workers" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "workflows" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "updated_at" FROM "workflows" WHERE "updated_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "workspace_members" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *'
UNION SELECT "created_at" FROM "workspaces" WHERE "created_at" GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]* [+-][0-9][0-9][0-9][0-9] *';

UPDATE ts_fix SET frac = CASE WHEN substr(old, 20, 1) = '.'
                              THEN substr(old, 20, instr(substr(old, 20), ' ') - 1)
                              ELSE '' END;
UPDATE ts_fix SET off = substr(old, 21 + length(frac), 5);

-- local = UTC + offset, so UTC = local - offset: "-0400" adds 240 minutes.
UPDATE ts_fix SET new =
    strftime('%Y-%m-%d %H:%M:%S', substr(old, 1, 19),
             ((CASE substr(off, 1, 1) WHEN '-' THEN 1 ELSE -1 END)
              * (CAST(substr(off, 2, 2) AS INTEGER) * 60 + CAST(substr(off, 4, 2) AS INTEGER)))
             || ' minutes')
    || frac || '+00:00'
WHERE off GLOB '[+-][0-9][0-9][0-9][0-9]';

UPDATE "acceptance_criteria" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("acceptance_criteria"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "acceptance_criteria" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("acceptance_criteria"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "analysis_scans" SET "finished_at" = (SELECT new FROM ts_fix WHERE old = CAST("analysis_scans"."finished_at" AS TEXT))
 WHERE CAST("finished_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "analysis_scans" SET "started_at" = (SELECT new FROM ts_fix WHERE old = CAST("analysis_scans"."started_at" AS TEXT))
 WHERE CAST("started_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "api_tokens" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("api_tokens"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "api_tokens" SET "last_used_at" = (SELECT new FROM ts_fix WHERE old = CAST("api_tokens"."last_used_at" AS TEXT))
 WHERE CAST("last_used_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "architecture_edges" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("architecture_edges"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "architecture_nodes" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("architecture_nodes"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "architecture_nodes" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("architecture_nodes"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "claimed_at" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."claimed_at" AS TEXT))
 WHERE CAST("claimed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "completed_at" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."completed_at" AS TEXT))
 WHERE CAST("completed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "due_date" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."due_date" AS TEXT))
 WHERE CAST("due_date" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "bug_items" SET "last_sync_at" = (SELECT new FROM ts_fix WHERE old = CAST("bug_items"."last_sync_at" AS TEXT))
 WHERE CAST("last_sync_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_anchors" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_anchors"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_anchors" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_anchors"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_chunks" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_chunks"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_chunks" SET "embed_failed_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_chunks"."embed_failed_at" AS TEXT))
 WHERE CAST("embed_failed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_chunks" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_chunks"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_chunks" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_chunks"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_findings" SET "first_seen_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_findings"."first_seen_at" AS TEXT))
 WHERE CAST("first_seen_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_findings" SET "last_seen_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_findings"."last_seen_at" AS TEXT))
 WHERE CAST("last_seen_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "code_findings" SET "resolved_at" = (SELECT new FROM ts_fix WHERE old = CAST("code_findings"."resolved_at" AS TEXT))
 WHERE CAST("resolved_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "codebases" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("codebases"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "custom_field_defs" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("custom_field_defs"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "custom_field_defs" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("custom_field_defs"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "custom_field_values" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("custom_field_values"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_moves" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_moves"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_passes" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_passes"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_proposals" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_proposals"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_proposals" SET "decided_at" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_proposals"."decided_at" AS TEXT))
 WHERE CAST("decided_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_runs" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_runs"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "decompose_runs" SET "source_modified" = (SELECT new FROM ts_fix WHERE old = CAST("decompose_runs"."source_modified" AS TEXT))
 WHERE CAST("source_modified" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "group_notes" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("group_notes"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "item_dependencies" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("item_dependencies"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "item_events" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("item_events"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "job_phases" SET "finished_at" = (SELECT new FROM ts_fix WHERE old = CAST("job_phases"."finished_at" AS TEXT))
 WHERE CAST("finished_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "job_phases" SET "started_at" = (SELECT new FROM ts_fix WHERE old = CAST("job_phases"."started_at" AS TEXT))
 WHERE CAST("started_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "jobs" SET "finished_at" = (SELECT new FROM ts_fix WHERE old = CAST("jobs"."finished_at" AS TEXT))
 WHERE CAST("finished_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "jobs" SET "started_at" = (SELECT new FROM ts_fix WHERE old = CAST("jobs"."started_at" AS TEXT))
 WHERE CAST("started_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "jobs" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("jobs"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "knowledge_entries" SET "claimed_at" = (SELECT new FROM ts_fix WHERE old = CAST("knowledge_entries"."claimed_at" AS TEXT))
 WHERE CAST("claimed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "knowledge_entries" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("knowledge_entries"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "knowledge_entries" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("knowledge_entries"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "knowledge_entries" SET "last_sync_at" = (SELECT new FROM ts_fix WHERE old = CAST("knowledge_entries"."last_sync_at" AS TEXT))
 WHERE CAST("last_sync_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "mcp_export_queue" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("mcp_export_queue"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "mcp_export_queue" SET "next_attempt_at" = (SELECT new FROM ts_fix WHERE old = CAST("mcp_export_queue"."next_attempt_at" AS TEXT))
 WHERE CAST("next_attempt_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "mcp_export_queue" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("mcp_export_queue"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "model_providers" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("model_providers"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "model_providers" SET "last_ok_at" = (SELECT new FROM ts_fix WHERE old = CAST("model_providers"."last_ok_at" AS TEXT))
 WHERE CAST("last_ok_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "model_providers" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("model_providers"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "project_members" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("project_members"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "project_settings" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("project_settings"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "projects" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("projects"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "review_decisions" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("review_decisions"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "review_flags" SET "cleared_at" = (SELECT new FROM ts_fix WHERE old = CAST("review_flags"."cleared_at" AS TEXT))
 WHERE CAST("cleared_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "review_flags" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("review_flags"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "schema_migrations" SET "applied_at" = (SELECT new FROM ts_fix WHERE old = CAST("schema_migrations"."applied_at" AS TEXT))
 WHERE CAST("applied_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpad_items" SET "archived_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpad_items"."archived_at" AS TEXT))
 WHERE CAST("archived_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpad_items" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpad_items"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpad_items" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpad_items"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpad_items" SET "og_fetched_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpad_items"."og_fetched_at" AS TEXT))
 WHERE CAST("og_fetched_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpad_items" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpad_items"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "scratchpads" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("scratchpads"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "sessions" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("sessions"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "sessions" SET "expires_at" = (SELECT new FROM ts_fix WHERE old = CAST("sessions"."expires_at" AS TEXT))
 WHERE CAST("expires_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "sessions" SET "last_active_at" = (SELECT new FROM ts_fix WHERE old = CAST("sessions"."last_active_at" AS TEXT))
 WHERE CAST("last_active_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "skill_applications" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("skill_applications"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "skill_retrievals" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("skill_retrievals"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "skill_versions" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("skill_versions"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "skills" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("skills"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "skills" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("skills"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "claimed_at" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."claimed_at" AS TEXT))
 WHERE CAST("claimed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "completed_at" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."completed_at" AS TEXT))
 WHERE CAST("completed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "due_date" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."due_date" AS TEXT))
 WHERE CAST("due_date" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "todo_items" SET "last_sync_at" = (SELECT new FROM ts_fix WHERE old = CAST("todo_items"."last_sync_at" AS TEXT))
 WHERE CAST("last_sync_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "claimed_at" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."claimed_at" AS TEXT))
 WHERE CAST("claimed_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "due_date" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."due_date" AS TEXT))
 WHERE CAST("due_date" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "embedded_at" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."embedded_at" AS TEXT))
 WHERE CAST("embedded_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "implementation_date" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."implementation_date" AS TEXT))
 WHERE CAST("implementation_date" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "last_sync_at" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."last_sync_at" AS TEXT))
 WHERE CAST("last_sync_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "use_case_items" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("use_case_items"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "user_settings" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("user_settings"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "users" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("users"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "verification_results" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("verification_results"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "verification_results" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("verification_results"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "workflow_workers" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("workflow_workers"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "workflows" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("workflows"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "workflows" SET "updated_at" = (SELECT new FROM ts_fix WHERE old = CAST("workflows"."updated_at" AS TEXT))
 WHERE CAST("updated_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "workspace_members" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("workspace_members"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);
UPDATE "workspaces" SET "created_at" = (SELECT new FROM ts_fix WHERE old = CAST("workspaces"."created_at" AS TEXT))
 WHERE CAST("created_at" AS TEXT) IN (SELECT old FROM ts_fix WHERE new IS NOT NULL);

DROP TABLE ts_fix;
