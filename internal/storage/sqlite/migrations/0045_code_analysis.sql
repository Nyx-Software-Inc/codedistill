-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--
--  CodeDistill
--
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Code Analysis (UC-14, slice 1): static-analyzer findings + scan runs.
-- Static analyzers (gosec/staticcheck in v1) detect; findings render on a
-- results page where the user pushes them into a scratchpad (the classifier
-- turns them into bugs/todos) or dismisses them. Re-scans match on fingerprint
-- so findings update in place rather than duplicating, dismissals survive, and
-- a finding gone from a fresh full scan is stamped resolved (fixed in code)
-- rather than deleted — history feeds the dashboard later.

CREATE TABLE code_findings (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  fingerprint   TEXT NOT NULL,              -- analyzer + rule + file + location hash
  analyzer      TEXT NOT NULL,              -- "gosec" | "staticcheck"
  rule_id       TEXT NOT NULL,              -- analyzer-native rule code (G101, SA4006, …)
  severity      TEXT NOT NULL,              -- normalized: high | medium | low | info
  file_path     TEXT NOT NULL,              -- repo-relative
  line_start    INTEGER NOT NULL DEFAULT 0,
  line_end      INTEGER NOT NULL DEFAULT 0,
  title         TEXT NOT NULL DEFAULT '',   -- short analyzer message
  detail        TEXT NOT NULL DEFAULT '',   -- fuller analyzer output / offending code
  llm_summary   TEXT NOT NULL DEFAULT '',   -- triage pass (slice 3); empty until then
  llm_severity  TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'open', -- open | pushed | dismissed
  pushed_item_id TEXT,                       -- scratchpad item created on push
  first_seen_at TIMESTAMP NOT NULL,
  last_seen_at  TIMESTAMP NOT NULL,
  resolved_at   TIMESTAMP,                   -- set when absent from a fresh full scan
  UNIQUE (project_id, fingerprint)
);

CREATE INDEX idx_code_findings_project_status ON code_findings(project_id, status);

CREATE TABLE analysis_scans (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  trigger           TEXT NOT NULL DEFAULT 'manual', -- manual | background
  started_at        TIMESTAMP NOT NULL,
  finished_at       TIMESTAMP,
  files_scanned     INTEGER NOT NULL DEFAULT 0,
  findings_new      INTEGER NOT NULL DEFAULT 0,
  findings_resolved INTEGER NOT NULL DEFAULT 0,
  skipped           TEXT NOT NULL DEFAULT '', -- analyzers skipped (not installed) + install hint
  error             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_analysis_scans_project ON analysis_scans(project_id, started_at);
