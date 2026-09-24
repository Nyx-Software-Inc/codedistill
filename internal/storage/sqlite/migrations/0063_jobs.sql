-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Long-running work, tracked in one place.
--
-- CodeDistill grew four private job systems before this table existed: the
-- classify queue (an in-memory channel), verification runs, analysis scans
-- (the only one that persisted), and architecture drafting (in-memory map,
-- own progress shape wedged into GET /architecture). Four answers to "what is
-- running and how far along", none of which survive a restart except scans.
--
-- What this table does NOT do is hold a row per unit of work. Architecture
-- drafting already resumes correctly by deriving its remaining work from its
-- own output — "needs enrichment" is a proposed node with an empty description
-- — and that is strictly better than a task ledger that can disagree with
-- reality. So a job records the RUN; how a client resumes is the client's
-- business. Clients that cannot derive remaining work persist it themselves,
-- in their own tables, beside their own output.
--
-- Job results are likewise not stored here. Decomposition produces proposals,
-- architecture drafting produces node descriptions, an agent produces a commit.
-- Those are not a shared blob, and a result column would grow every time a
-- job type is added. Each type writes its own output and this row points at it
-- through (scope_kind, scope_id).

CREATE TABLE jobs (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  -- What kind of work. Open vocabulary on purpose: adding a job type must not
  -- require a migration, because the conductor will add several.
  type          TEXT NOT NULL,               -- 'decompose' | 'arch_draft' | ...

  -- What it is working ON. scope_kind names the table, scope_id the row, so a
  -- job can point at a document item, a project, a directory, a scratchpad.
  -- Both nullable: a whole-project job scopes to nothing narrower.
  scope_kind    TEXT,                        -- 'scratchpad_item' | 'directory' | ...
  scope_id      TEXT,
  scope_label   TEXT,                        -- human-readable, for the dashboard

  status        TEXT NOT NULL
                CHECK (status IN ('running','succeeded','failed','cancelled','interrupted')),

  -- Progress. total is revisable: a phased job does not know the size of its
  -- second phase until the first finishes, and pretending otherwise is how you
  -- get a progress bar that grows.
  phase         TEXT,                        -- 'reading' | 'projecting' | NULL
  done          INTEGER NOT NULL DEFAULT 0,
  total         INTEGER NOT NULL DEFAULT 0,  -- 0 = not yet known

  -- Accounting. Local models are not billed but tokens still bound context,
  -- and per-type duration is what the job dashboard is for.
  model         TEXT,
  tokens_in     INTEGER NOT NULL DEFAULT 0,
  tokens_out    INTEGER NOT NULL DEFAULT 0,

  error         TEXT,
  created_by    TEXT,                        -- user id, NULL for system-initiated
  started_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL,
  finished_at   TIMESTAMP
);

-- The dashboard reads "what is running" and "what did this project run lately".
CREATE INDEX idx_jobs_project_status ON jobs(project_id, status, started_at DESC);
-- A client asking "is there already a job for this thing" — the singleton check
-- arch drafting does today with an in-memory map keyed by project.
CREATE INDEX idx_jobs_scope ON jobs(type, scope_kind, scope_id, status);
