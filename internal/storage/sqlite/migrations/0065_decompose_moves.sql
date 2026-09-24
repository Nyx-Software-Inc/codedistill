-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Reading passes, persisted as they finish.
--
-- Decomposition held every move in memory until the whole run completed, so an
-- interruption threw all of it away. Measured: four of five reading passes
-- finished, 24 minutes of model time, and zero rows survived — the fifth pass
-- failed and took the other four with it. A laptop lid closing is not an
-- unusual event, and neither is a model server restarting.
--
-- Architecture drafting already solved this by deriving its remaining work from
-- its own output rather than from job state, which is strictly better than a
-- progress counter that can disagree with reality. The same shape applies here:
-- a pass whose moves are on disk is done, whatever any counter says.
--
-- Moves are keyed by (source document, window) rather than by job, so a resumed
-- run is a NEW job that inherits the completed passes of the old one. Keying by
-- job would mean a retry starts over, which is the thing being fixed.

CREATE TABLE decompose_moves (
  id             TEXT PRIMARY KEY,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  -- The document's content hash, not its item id: the identity that makes a
  -- re-drop recognisable is the same identity that makes completed reading
  -- reusable. A revised document has a different hash and correctly shares
  -- nothing.
  source_hash    TEXT NOT NULL,

  -- Which reading pass produced this. Sentence indices are stable for a given
  -- hash, so (hash, window_start) names a pass exactly.
  window_start   INTEGER NOT NULL,
  window_end     INTEGER NOT NULL,

  sentence       INTEGER NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('introduce','elaborate','retract','meta')),
  label          TEXT NOT NULL DEFAULT '',
  -- NULL is meaningful and distinct from 0: the model flagged a refinement
  -- without being able to name what it refines, which layer 3 handles by
  -- attaching to the nearest open idea and marking the link inferred.
  target         INTEGER,

  created_at     TIMESTAMP NOT NULL
);

-- "Which passes are already done for this document?" — the resume query.
CREATE INDEX idx_decompose_moves_source ON decompose_moves(source_hash, window_start);
CREATE INDEX idx_decompose_moves_project ON decompose_moves(project_id, source_hash);

-- A completed pass is recorded even when it produced NO moves, because
-- "finished and found nothing" and "never ran" are different facts and only one
-- of them is worth 5 minutes to redo.
CREATE TABLE decompose_passes (
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_hash    TEXT NOT NULL,
  window_start   INTEGER NOT NULL,
  window_end     INTEGER NOT NULL,
  moves          INTEGER NOT NULL DEFAULT 0,
  rejected       INTEGER NOT NULL DEFAULT 0,
  created_at     TIMESTAMP NOT NULL,
  PRIMARY KEY (project_id, source_hash, window_start)
);
