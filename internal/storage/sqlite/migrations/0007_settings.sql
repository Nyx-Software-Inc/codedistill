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

-- Phase 2 (Stage 3) settings tables: per-User and per-Project key/value
-- stores. Drawer state is the wedge — first persisted setting and motivation
-- for the table — but the schema is generic so the set can grow (model
-- selection, default priority, theme, points scale, etc.) without migrations.
--
-- Cascade lookup (scratchpad > project > user > built-in default) is encoded
-- at the call site, not at the storage layer. The scratchpad scope continues
-- to use dedicated columns on the scratchpads row (e.g. classification_mode);
-- no third settings table is introduced for it.
--
-- Value is stored as JSON-encoded TEXT so primitive (string/number/bool) and
-- structured values share a single column. Callers serialize/deserialize.

CREATE TABLE user_settings (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, key)
);

CREATE TABLE project_settings (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, key)
);
