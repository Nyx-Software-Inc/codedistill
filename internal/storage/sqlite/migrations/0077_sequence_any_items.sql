-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Ordering is not a property of documents.
--
-- 0076 put proposed edges in their own table because decomposition proposes
-- them between PROPOSALS, before either end is work. That was a consequence of
-- building sequencing inside decompose — and it left every other item with no
-- way to be ordered at all. Items typed by hand, derived by the classifier or
-- created over MCP are exactly as orderable, and the sweep never read the
-- document: it needs a label, a kind, a subject and a sentence of body.
--
-- So sequencing runs over ITEMS, always, as its own workflow. Both ends exist
-- before it starts, there is no interval to model, and one table does.
--
-- decompose_edges is dropped rather than left dormant. It shipped one commit
-- ago and holds nothing; a table that exists but is never written is a question
-- for whoever reads the schema next.

DROP TABLE IF EXISTS decompose_edges;

-- A proposed edge is still a claim by a model, so it waits for a human like
-- every other claim here. Status is how it waits.
--
--   'proposed' | 'accepted' | 'rejected'
--
-- Rejected rows are KEPT. A later run over the same items would otherwise
-- propose the same edge again, and re-asking a question already answered is
-- how a review screen teaches people to stop reading it.
ALTER TABLE item_dependencies ADD COLUMN status TEXT NOT NULL DEFAULT 'accepted';

-- Which run proposed it, when one did. Null for a human-drawn edge.
ALTER TABLE item_dependencies ADD COLUMN job_id TEXT;

CREATE INDEX idx_item_deps_status ON item_dependencies(project_id, status);
