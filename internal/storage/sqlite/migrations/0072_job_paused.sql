-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- jobs.status gains 'paused'.
--
-- Paused is distinct from cancelled and interrupted: cancelled means abandoned,
-- interrupted means the process died, paused means a human stopped it and
-- expects it back. Collapsing them would make the monitor unable to offer
-- Resume on the one that deserves it.
--
-- The CHECK is KEPT here, unlike protocol (0067) and worker_type (0069). Those
-- are extensible by design — a new adapter or a user-defined workflow can
-- legitimately introduce a value the schema never heard of. Status is not: it
-- is a small closed vocabulary owned entirely by this codebase's state machine,
-- and an unknown status would be a bug, not a feature. The constraint earns
-- its place by catching exactly the mistake that produced this migration —
-- adding a constant in Go and forgetting the database.

CREATE TABLE jobs_new (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  type          TEXT NOT NULL,
  scope_kind    TEXT,
  scope_id      TEXT,
  scope_label   TEXT,
  status        TEXT NOT NULL
                CHECK (status IN ('running','succeeded','failed','cancelled','interrupted','paused')),
  phase         TEXT,
  done          INTEGER NOT NULL DEFAULT 0,
  total         INTEGER NOT NULL DEFAULT 0,
  model         TEXT,
  tokens_in     INTEGER NOT NULL DEFAULT 0,
  tokens_out    INTEGER NOT NULL DEFAULT 0,
  error         TEXT,
  created_by    TEXT,
  started_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL,
  finished_at   TIMESTAMP
);

INSERT INTO jobs_new SELECT
  id, project_id, type, scope_kind, scope_id, scope_label, status, phase,
  done, total, model, tokens_in, tokens_out, error, created_by,
  started_at, updated_at, finished_at
FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX idx_jobs_project_status ON jobs(project_id, status, started_at DESC);
CREATE INDEX idx_jobs_scope ON jobs(type, scope_kind, scope_id, status);
