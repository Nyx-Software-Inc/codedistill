-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Claims, verdicts and per-run stats for agent workflows
-- (docs/design/fix-scanner-findings.md).
--
-- A challenger needs something precise to attack. "Review this fix" invites
-- an opinion; "the fixer says SHA-256 is adequate for password storage — is
-- it?" invites a verdict. So the fixer states its claims, each lens of the
-- challenger rules on every one, and both are kept.

-- What an agent asserted while doing the work.
CREATE TABLE job_claims (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id TEXT NOT NULL REFERENCES job_items(id) ON DELETE CASCADE,
  round       INTEGER NOT NULL,
  author_role TEXT NOT NULL,                 -- 'solutioner'
  -- fact:       about the code as it is ("Fingerprint stores passwords")
  -- assumption: taken as true without checking
  -- decision:   a choice the fix made, and what it rests on
  kind        TEXT NOT NULL CHECK (kind IN ('fact','assumption','decision')),
  statement   TEXT NOT NULL,
  evidence    TEXT NOT NULL DEFAULT '',      -- file:line, a command and its output
  falsifier   TEXT NOT NULL DEFAULT '',      -- what observation would prove it wrong
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_job_claims_item ON job_claims(job_item_id, round);

-- A lens's ruling on a claim, or a problem it found that no claim covered
-- (claim_id NULL, verdict 'problem'). Each lens runs separately and never sees
-- the other's verdicts.
CREATE TABLE claim_verdicts (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id TEXT NOT NULL REFERENCES job_items(id) ON DELETE CASCADE,
  round       INTEGER NOT NULL,
  lens        TEXT NOT NULL,                 -- 'security' | 'code'
  claim_id    TEXT REFERENCES job_claims(id) ON DELETE CASCADE,
  verdict     TEXT NOT NULL CHECK (verdict IN ('holds','false','unclear','problem')),
  checked     TEXT NOT NULL DEFAULT '',      -- what it looked at or ran
  detail      TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_claim_verdicts_item ON claim_verdicts(job_item_id, round, lens);

-- One row per agent run: what each agent consumed and produced. Columns an
-- agent cannot report stay NULL — unknown is not zero.
CREATE TABLE agent_runs (
  id             TEXT PRIMARY KEY,
  job_id         TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id    TEXT REFERENCES job_items(id) ON DELETE CASCADE,
  role           TEXT NOT NULL,
  lens           TEXT NOT NULL DEFAULT '',
  round          INTEGER NOT NULL,
  agent          TEXT NOT NULL DEFAULT '',   -- 'claude-code', or the command
  model          TEXT,
  outcome        TEXT NOT NULL,              -- committed | no_commit | verdicts | no_verdict | gave_up | silent | cancelled | failed
  exit_code      INTEGER,
  started_at     TIMESTAMP NOT NULL,
  duration_ms    INTEGER NOT NULL,
  silent_ms      INTEGER NOT NULL DEFAULT 0,  -- longest silence seen
  turns          INTEGER,
  tokens_in      INTEGER,
  tokens_out     INTEGER,
  cache_read     INTEGER,
  cache_write    INTEGER,
  cost_usd       REAL,
  tool_calls     TEXT NOT NULL DEFAULT '',   -- JSON {tool: count}
  files_changed  INTEGER,
  lines_added    INTEGER,
  lines_deleted  INTEGER,
  claims_made    INTEGER,
  claims_refuted INTEGER
);
CREATE INDEX idx_agent_runs_job ON agent_runs(job_id, started_at);
CREATE INDEX idx_agent_runs_role ON agent_runs(role, lens);
