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

-- 0033: item lineage event log (CodeDestill_imports use-case #5).
-- One row per notable mutation on an item or its derived work item.
-- owner_type/owner_id point at the affected row (scratchpad_item,
-- todo_item, bug_item, knowledge_entry, use_case_item); the lineage
-- endpoint merges a source item's events with its derived item's.
-- source records the actor surface: 'ui' | 'mcp' | 'agent'.
CREATE TABLE item_events (
    id         TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    summary    TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT 'ui',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_item_events_owner
    ON item_events (owner_type, owner_id, created_at);
