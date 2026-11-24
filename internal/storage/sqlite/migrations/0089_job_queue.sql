-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- The job queue (docs/design/job-queue.md).
--
-- jobs.status gains 'queued': a job submitted while every job slot is busy
-- waits in line. priority orders the line; queued_at is when it joined it.
-- started_at stays NOT NULL and holds the submit time until the job leaves the
-- line, when it becomes the real start.
--
-- A table rebuild, because SQLite cannot change a CHECK in place. Ten tables
-- refer to jobs with ON DELETE CASCADE, so the rebuild must run with foreign
-- keys off, or DROP TABLE would delete every job's items, runs and params:
-- migrate: foreign keys off

CREATE TABLE jobs_new (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  type          TEXT NOT NULL,
  scope_kind    TEXT,
  scope_id      TEXT,
  scope_label   TEXT,
  status        TEXT NOT NULL
                CHECK (status IN ('queued','running','succeeded','failed','cancelled','interrupted','paused')),
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
  finished_at   TIMESTAMP,
  branch        TEXT,
  base_sha      TEXT,
  priority      TEXT NOT NULL DEFAULT 'medium'
                CHECK (priority IN ('low','medium','high','immediate')),
  queued_at     TIMESTAMP
);

INSERT INTO jobs_new (id, project_id, type, scope_kind, scope_id, scope_label, status, phase,
  done, total, model, tokens_in, tokens_out, error, created_by,
  started_at, updated_at, finished_at, branch, base_sha)
SELECT id, project_id, type, scope_kind, scope_id, scope_label, status, phase,
  done, total, model, tokens_in, tokens_out, error, created_by,
  started_at, updated_at, finished_at, branch, base_sha
FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX idx_jobs_project_status ON jobs(project_id, status, started_at DESC);
CREATE INDEX idx_jobs_scope ON jobs(type, scope_kind, scope_id, status);
CREATE INDEX idx_jobs_status ON jobs(status);
