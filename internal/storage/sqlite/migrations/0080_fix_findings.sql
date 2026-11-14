-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- "Fix scanner findings": the first workflow that runs outside agents
-- (docs/design/fix-scanner-findings.md).
--
-- A job now owns a git branch. The Conductor cuts one branch per job from the
-- project's HEAD and hands its worktree to the agents; every commit left on it
-- is a verified fix.

ALTER TABLE jobs ADD COLUMN branch TEXT;     -- 'cd/fix/<job id>'
ALTER TABLE jobs ADD COLUMN base_sha TEXT;   -- where the branch was cut

-- One row per item a job works through, in the order it works them.
--
-- A job's own row says how the run is going; this says what happened to each
-- thing it touched. They are separate because a scratchpad run is many
-- outcomes under one job, and "the job succeeded" says nothing about which
-- items were fixed and which are waiting on a person.
CREATE TABLE job_items (
  id               TEXT PRIMARY KEY,
  job_id           TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  position         INTEGER NOT NULL,           -- work order, highest priority first

  -- The work item the outcome is recorded on (a bug, usually), and the scanner
  -- finding it came from. The finding is the identity: an item belongs in this
  -- workflow because a finding points at it, not because of how it was
  -- classified.
  owner_type       TEXT NOT NULL,              -- 'bug_item' | 'todo_item' | ... | 'scratchpad_item'
  owner_id         TEXT NOT NULL,
  finding_id       TEXT REFERENCES code_findings(id) ON DELETE SET NULL,
  label            TEXT NOT NULL DEFAULT '',

  status           TEXT NOT NULL
                   CHECK (status IN ('queued','running','fixed','waiting','skipped','cancelled')),
  -- Why it ended where it did, in words a person can act on: which check
  -- failed, what the challenger objected to, that the agent gave up or went
  -- silent. Empty for 'fixed' and 'queued'.
  reason           TEXT NOT NULL DEFAULT '',

  commit_sha       TEXT,      -- the verified fix, when there is one
  attempt_ref      TEXT,      -- a set-aside attempt, kept for inspection
  rounds           INTEGER NOT NULL DEFAULT 0,
  checks           TEXT NOT NULL DEFAULT '',   -- JSON: the four checks' verdicts

  -- Silence detection. An agent reports progress over MCP (report_progress);
  -- the Conductor watches this, flags an item that has gone quiet, and asks a
  -- person after a long silence. Slowness is never a reason to stop anything.
  progress_note    TEXT NOT NULL DEFAULT '',
  last_progress_at TIMESTAMP,

  started_at       TIMESTAMP,
  finished_at      TIMESTAMP
);

CREATE INDEX idx_job_items_job ON job_items(job_id, position);
CREATE INDEX idx_job_items_owner ON job_items(owner_type, owner_id);

INSERT INTO workflows (id, name, description, builtin, resumable, enabled, created_at, updated_at) VALUES
  ('fix_findings', 'Fix scanner findings',
   'An outside coding agent fixes each scanner finding on one branch, highest priority first. CodeDistill checks every fix itself: the finding is gone, nothing new appeared, the tests pass. A challenger that never saw the fix being made tries to break it.',
   1, 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- Steps name the roles, in order. The agents are outside programs (Claude Code
-- first), so no model provider is bound here; which agent and model play a
-- role are settings, not steps.
INSERT INTO workflow_steps (workflow_id, ordinal, worker_type, label, needs_context) VALUES
  ('fix_findings', 0, 'test_deriver', 'Writes an attack test for the weakness, when it can be exercised', 0),
  ('fix_findings', 1, 'solutioner',   'Fixes the cause on the job''s branch and commits', 0),
  ('fix_findings', 2, 'challenger',   'Tries to break the fix without seeing how it was made', 0);

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, options, multiple) VALUES
  ('fix_findings', 0, 'scratchpad', 'Scratchpad',
   'Every scanner finding on this scratchpad is worked, highest severity first. Items that did not come from a scanner are listed and left alone.',
   'scratchpad', 1, '', '', 0),
  ('fix_findings', 1, 'items', 'Only these items',
   'Leave empty to work the whole scratchpad.',
   'scratchpad_item', 0, '', '', 1),
  ('fix_findings', 2, 'round_limit', 'Rounds per item',
   'How many times the fixer may answer the challenger before an item is handed to you. No upper limit.',
   'text', 1, '3', '', 0),
  ('fix_findings', 3, 'stop_below', 'Stop below',
   'Optionally stop once everything at or above this severity has been attempted.',
   'select', 0, '', '|Work everything' || char(10) || 'critical|Critical' || char(10) || 'high|High' || char(10) || 'medium|Medium' || char(10) || 'low|Low', 0);
