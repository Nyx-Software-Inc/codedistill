-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- The Coordinator (docs/design/coordinator-agent.md).
--
-- job_decisions is the job's journal: every decision the Coordinator, the
-- Arbiter or the built-in rules made about an item, and why.

CREATE TABLE job_decisions (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id TEXT NOT NULL REFERENCES job_items(id) ON DELETE CASCADE,
  round       INTEGER NOT NULL DEFAULT 0,
  phase       TEXT NOT NULL DEFAULT '',
  situation   TEXT NOT NULL,
  move        TEXT NOT NULL,
  point_id    TEXT REFERENCES job_points(id) ON DELETE SET NULL,
  reason      TEXT NOT NULL DEFAULT '',
  evidence    TEXT NOT NULL DEFAULT '',
  guidance    TEXT NOT NULL DEFAULT '',
  decided_by  TEXT NOT NULL,
  model       TEXT NOT NULL DEFAULT '',
  fallback    TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_job_decisions_item ON job_decisions(job_item_id, created_at);

-- Who coordinates a workflow's jobs, and which challenger lenses review each
-- change (the Security Exploiter reviews every change, whatever is listed).
ALTER TABLE workflows ADD COLUMN coordinator TEXT NOT NULL DEFAULT 'agent'
  CHECK (coordinator IN ('agent','rules'));
ALTER TABLE workflows ADD COLUMN lenses TEXT NOT NULL DEFAULT '';
UPDATE workflows SET lenses = 'code' WHERE id = 'fix_findings';  -- Correctness

UPDATE workflows SET description = 'Agents fix each scanner finding on one branch, highest severity first. The Remediator proposes its approach and challengers review it before anything is built; a TestBuilder writes the tests while the fix is built; the Security Exploiter attacks every change. The Coordinator makes the judgment calls along the way. CodeDistill checks every fix itself: the finding is gone, nothing new appeared, the tests pass, and the new tests failed before the fix.'
  WHERE id = 'fix_findings';

-- The roles as people now know them, and the two new ones, so each gets its
-- model where every role does: Settings > Workflows.
UPDATE workflow_steps SET label = 'Fixes the finding on the job''s branch and commits'
  WHERE workflow_id = 'fix_findings' AND worker_type = 'solutioner';
UPDATE workflow_steps SET label = 'The chosen challengers and the Security Exploiter: try to break each change without seeing how it was made'
  WHERE workflow_id = 'fix_findings' AND worker_type = 'challenger';
UPDATE workflow_steps SET label = 'Writes the tests before the fix exists'
  WHERE workflow_id = 'fix_findings' AND worker_type = 'test_builder';
INSERT INTO workflow_steps (workflow_id, ordinal, worker_type, label, needs_context) VALUES
  ('fix_findings', 3, 'coordinator', 'Makes the judgment calls; writes no code and cannot pass work', 0),
  ('fix_findings', 4, 'arbiter',     'Settles an objection the Coordinator rejected and a challenger still holds', 0);
UPDATE workflow_params SET help = 'How many times the Remediator may answer the challengers before an item is handed to you. No upper limit.'
  WHERE workflow_id = 'fix_findings' AND key = 'round_limit';
