-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- What has to happen before what.
--
-- Forty-nine use cases derived from one document, and nothing anywhere said
-- which could be started first. Priority is a wish about importance; this is a
-- claim about ORDER, and the two are independent — the most important item is
-- frequently the one that cannot be started yet.
--
-- TWO tables, because an edge is born before either end exists as work.
-- Decomposition proposes edges between PROPOSALS, at a point where nothing has
-- been accepted; only when both ends become items is there an item-to-item
-- dependency to record. Rejecting one end must take the edge with it, and a
-- single table keyed on item ids could not represent the interval in between.

-- Edges between real work. The durable one.
CREATE TABLE item_dependencies (
  id          TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  -- Kind + id rather than a column per table: dependencies cross kinds. A bug
  -- can block a use case, and a todo can depend on a knowledge entry being
  -- written. Validated in Go, like every other open vocabulary here.
  --   'use_case' | 'todo' | 'bug' | 'kb'
  from_kind   TEXT NOT NULL,
  from_id     TEXT NOT NULL,
  to_kind     TEXT NOT NULL,
  to_id       TEXT NOT NULL,

  -- from_id DEPENDS ON to_id: to_id has to happen first. Stated here because
  -- the arrow direction is the one thing every dependency graph gets wrong, and
  -- reading it backwards inverts the entire plan silently.
  --
  --   'blocks'   to_id must be DONE before from_id can start
  --   'informs'  from_id is easier once to_id exists, but not blocked.
  --              Weaker on purpose: a model that can only say "blocks" will say
  --              it about everything, and a plan where all 49 block each other
  --              is the same as no plan.
  kind        TEXT NOT NULL DEFAULT 'blocks',

  -- Why, in the document's own words. An edge a human cannot check is an edge
  -- they have to take on faith, which is the opposite of the point.
  rationale   TEXT NOT NULL DEFAULT '',
  -- Line spans in the source document, when the edge came from one.
  lines       TEXT NOT NULL DEFAULT '',

  -- 'human' | 'decompose' — who claimed it. A human edge outranks a derived one
  -- and must never be silently replaced by a later run.
  origin      TEXT NOT NULL DEFAULT 'human',

  created_at  TIMESTAMP NOT NULL,
  created_by  TEXT,

  -- The same dependency stated twice is one dependency.
  UNIQUE (from_kind, from_id, to_kind, to_id)
);

CREATE INDEX idx_item_deps_from    ON item_dependencies(from_kind, from_id);
CREATE INDEX idx_item_deps_to      ON item_dependencies(to_kind, to_id);
CREATE INDEX idx_item_deps_project ON item_dependencies(project_id);

-- Edges a run proposed, between proposals, awaiting the same judgement as the
-- proposals themselves.
CREATE TABLE decompose_edges (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  from_proposal TEXT NOT NULL REFERENCES decompose_proposals(id) ON DELETE CASCADE,
  to_proposal   TEXT NOT NULL REFERENCES decompose_proposals(id) ON DELETE CASCADE,

  kind        TEXT NOT NULL DEFAULT 'blocks',
  rationale   TEXT NOT NULL DEFAULT '',
  lines       TEXT NOT NULL DEFAULT '',

  -- Mirrors decompose_proposals.status. An edge whose ends were both accepted
  -- becomes a row in item_dependencies and is marked accepted here; one whose
  -- end was rejected is marked 'dropped', which is a different fact from a
  -- human rejecting the edge itself and worth being able to tell apart.
  --   'pending' | 'accepted' | 'rejected' | 'dropped'
  status      TEXT NOT NULL DEFAULT 'pending',
  created_at  TIMESTAMP NOT NULL,

  UNIQUE (job_id, from_proposal, to_proposal)
);

CREATE INDEX idx_decompose_edges_job ON decompose_edges(job_id);
