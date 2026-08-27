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

-- v0.8.2-dev: foundation for outbound MCP export (#9 client).
--
-- Two pieces of schema, both prerequisites for the rest of v0.8.2:
--   1. Per-item-type sync columns on todo_items, bug_items,
--      knowledge_entries, use_case_items so we can track what's been
--      pushed to the destination MCP server, when, and with what
--      remote id (for idempotent re-pushes and the "destination is
--      write-of-record" semantics).
--   2. mcp_export_queue — the durable work queue the background
--      worker drains. Async fire-and-forget pushes get enqueued here
--      with retry/backoff state on the row itself.
--
-- Design rationale and the locked v1 decisions live in
-- project_mcp_design.md. Short version: per-user config in
-- user_settings.mcp.export, destination is write-of-record, CodeDistill
-- caches a read view, full CRUD pushed async, sync-failed indicator per
-- item.
--
-- Backfill: existing items get sync_status='local-only' (the default)
-- so they're invisible to the export pipeline until either (a) the
-- migration-on-configure prompt opts them in, or (b) they're edited
-- after a destination is configured. No remote IDs to backfill — those
-- only exist for items pushed via this pipeline.

-- Per-item-type sync state. Same four columns on each table.
-- ALTER TABLE ADD COLUMN is additive; no rebuild required.

ALTER TABLE todo_items         ADD COLUMN remote_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE todo_items         ADD COLUMN sync_status     TEXT NOT NULL DEFAULT 'local-only'
    CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed'));
ALTER TABLE todo_items         ADD COLUMN last_sync_at    TIMESTAMP;
ALTER TABLE todo_items         ADD COLUMN last_sync_error TEXT NOT NULL DEFAULT '';

ALTER TABLE bug_items          ADD COLUMN remote_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_items          ADD COLUMN sync_status     TEXT NOT NULL DEFAULT 'local-only'
    CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed'));
ALTER TABLE bug_items          ADD COLUMN last_sync_at    TIMESTAMP;
ALTER TABLE bug_items          ADD COLUMN last_sync_error TEXT NOT NULL DEFAULT '';

ALTER TABLE knowledge_entries  ADD COLUMN remote_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_entries  ADD COLUMN sync_status     TEXT NOT NULL DEFAULT 'local-only'
    CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed'));
ALTER TABLE knowledge_entries  ADD COLUMN last_sync_at    TIMESTAMP;
ALTER TABLE knowledge_entries  ADD COLUMN last_sync_error TEXT NOT NULL DEFAULT '';

ALTER TABLE use_case_items     ADD COLUMN remote_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE use_case_items     ADD COLUMN sync_status     TEXT NOT NULL DEFAULT 'local-only'
    CHECK (sync_status IN ('local-only', 'pending', 'synced', 'failed'));
ALTER TABLE use_case_items     ADD COLUMN last_sync_at    TIMESTAMP;
ALTER TABLE use_case_items     ADD COLUMN last_sync_error TEXT NOT NULL DEFAULT '';

-- Indexes on sync_status to make the indicator's "show me everything failing"
-- query cheap. Per item type because the worker / UI scopes by type.
CREATE INDEX idx_todo_items_sync_status        ON todo_items(sync_status);
CREATE INDEX idx_bug_items_sync_status         ON bug_items(sync_status);
CREATE INDEX idx_knowledge_entries_sync_status ON knowledge_entries(sync_status);
CREATE INDEX idx_use_case_items_sync_status    ON use_case_items(sync_status);

-- Outbound work queue. One row per pending push attempt. The worker SELECTs
-- WHERE next_attempt_at <= now() ORDER BY next_attempt_at LIMIT N, processes
-- each, then either DELETEs (success) or UPDATEs attempts/next_attempt_at/
-- last_error (retry).
--
-- owner_id is intentionally NOT a foreign key — same polymorphic pattern as
-- code_anchors. Application code handles cascade-on-delete: when an item is
-- deleted locally we either enqueue a delete op (if the item had been pushed
-- previously) or drop any pending non-delete ops for that owner. Doing this
-- in app code rather than SQLite triggers keeps the lifecycle visible.
--
-- payload is a JSON blob — what the worker hands to the resolved remote
-- tool. The shape depends on the destination's tool schema (discovered at
-- configure time and stored in user_settings.mcp.export.<type>.mapping).
CREATE TABLE mcp_export_queue (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_type      TEXT NOT NULL
        CHECK (owner_type IN (
            'todo_item', 'bug_item', 'knowledge_entry', 'use_case_item'
        )),
    owner_id        TEXT NOT NULL,
    op              TEXT NOT NULL
        CHECK (op IN ('create', 'update', 'status_change', 'delete')),
    payload         TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Worker poll: the only hot query on this table. Order matters; covering
-- index on (next_attempt_at) is enough since the worker filters and orders
-- by that single column.
CREATE INDEX idx_mcp_export_queue_due ON mcp_export_queue(next_attempt_at);

-- Per-owner index for the cascade-on-delete and "drop pending ops for this
-- item" lookups in app code.
CREATE INDEX idx_mcp_export_queue_owner ON mcp_export_queue(owner_type, owner_id);
