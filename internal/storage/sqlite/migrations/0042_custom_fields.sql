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

-- v0.12.x — user-defined custom fields (backlog item #1, the custom-fields half;
-- the custom-categories half was split off + deferred).
--
-- A custom field is a TYPED definition scoped to a project and a set of item
-- types. Types: text | number | select | date. Numbers are the "metrics" — kept
-- typed so a later slice can sort/aggregate on them; this slice is display+edit
-- only. Values are polymorphic per item (same owner_type/owner_id pattern as
-- code anchors / verification results / review decisions). Story Points is just
-- a user-defined number field — not a built-in column.
CREATE TABLE custom_field_defs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    field_type  TEXT NOT NULL DEFAULT 'text', -- text | number | select | date
    options     TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings, for select
    applies_to  TEXT NOT NULL DEFAULT '[]',   -- JSON array of owner types this field shows on
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_custom_field_def_project ON custom_field_defs(project_id);

CREATE TABLE custom_field_values (
    field_id    TEXT NOT NULL,
    owner_type  TEXT NOT NULL, -- todo_item | bug_item | use_case_item | knowledge_entry
    owner_id    TEXT NOT NULL,
    value       TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMP NOT NULL,
    PRIMARY KEY (field_id, owner_type, owner_id),
    FOREIGN KEY (field_id) REFERENCES custom_field_defs(id) ON DELETE CASCADE
);
CREATE INDEX idx_custom_field_value_owner ON custom_field_values(owner_type, owner_id);
