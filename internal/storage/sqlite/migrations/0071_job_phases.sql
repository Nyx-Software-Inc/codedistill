-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Per-phase timing, so a finished run can say where its time went.
--
-- jobs.phase is a single CURRENT value that gets overwritten, which answers
-- "what is it doing now" and destroys "what did it do". A real decomposition
-- spends 24 minutes reading and 5 sorting; the jobs row remembers neither once
-- it moves on.
--
-- This is also what makes "typical run" possible — the only number that turns
-- "12 minutes elapsed" into something a human can act on. Without a history to
-- compare against, elapsed time is a number with no verdict attached.
--
-- worker_type is recorded per phase because a workflow with interacting agents
-- has several: a solutioner and a challenger take turns, and "the solutioner
-- spent 4 minutes and the challenger 90 seconds" is the useful shape. Today
-- every phase has one worker and this column is constant per job; writing it
-- down now costs nothing and avoids a migration when that stops being true.

CREATE TABLE job_phases (
  id           TEXT PRIMARY KEY,
  job_id       TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,

  -- Ordinal rather than relying on time: two phases can share a start instant
  -- at second resolution, and a run must render in the order it happened.
  ordinal      INTEGER NOT NULL,
  phase        TEXT NOT NULL,           -- 'reading', 'sorting', 'reconciling'
  worker_type  TEXT NOT NULL DEFAULT '',

  -- Round counts the exchange for interacting agents: solutioner proposes,
  -- challenger attacks, repeat. 0 for a single-pass phase.
  round        INTEGER NOT NULL DEFAULT 0,

  done         INTEGER NOT NULL DEFAULT 0,
  total        INTEGER NOT NULL DEFAULT 0,
  tokens_in    INTEGER NOT NULL DEFAULT 0,
  tokens_out   INTEGER NOT NULL DEFAULT 0,

  error        TEXT,
  started_at   TIMESTAMP NOT NULL,
  -- NULL means still running. A phase that never finished is exactly what you
  -- want to see on a job that died, so it is not backfilled.
  finished_at  TIMESTAMP
);

CREATE INDEX idx_job_phases_job ON job_phases(job_id, ordinal);
-- "How long does this workflow usually take?" — the typical-run comparison,
-- which reads finished phases across every past run of a workflow.
CREATE INDEX idx_job_phases_phase ON job_phases(phase, finished_at);
