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

-- v0.11.x — architecture diagram (glass-box Phase 5, slice 5.3a).
--
-- The self-maintaining architecture diagram: the LLM drafts the *conceptual*
-- structure (nodes + edges) from the project brain + use cases + a code map; the
-- human ratifies (proposed → ratified); drift later keeps it honest. Provenance
-- renders as ratified=solid / proposed=dashed; pos_x/pos_y let the human arrange
-- the layout and have the system preserve it (a later slice).
CREATE TABLE architecture_nodes (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'component', -- ui | service | store | external | component
    description TEXT NOT NULL DEFAULT '',
    area        TEXT NOT NULL DEFAULT '',          -- optional path hint wiring the node to code
    pos_x       REAL NOT NULL DEFAULT 0,
    pos_y       REAL NOT NULL DEFAULT 0,
    provenance  TEXT NOT NULL DEFAULT 'proposed',  -- proposed | ratified
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_arch_node_project ON architecture_nodes(project_id);

CREATE TABLE architecture_edges (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    from_node  TEXT NOT NULL,
    to_node    TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    provenance TEXT NOT NULL DEFAULT 'proposed',
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_arch_edge_project ON architecture_edges(project_id);
