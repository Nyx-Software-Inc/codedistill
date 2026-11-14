-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- The TestBuilder's tests, and its review of the tests already there
-- (docs/design/fix-scanner-findings.md, "The TestBuilder and the three kinds
-- of tests").
--
-- A new test is aimed at the problem (it must fail on the code before the
-- fix and pass after it) or guards against breaking something (it must pass
-- after). An existing test the TestBuilder reviewed still applies, or no
-- longer does, with the reason: reported, never changed by it.
CREATE TABLE job_tests (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id TEXT NOT NULL REFERENCES job_items(id) ON DELETE CASCADE,
  origin      TEXT NOT NULL CHECK (origin IN ('new','existing')),
  name        TEXT NOT NULL,
  file        TEXT NOT NULL DEFAULT '',
  purpose     TEXT NOT NULL DEFAULT '' CHECK (purpose IN ('','problem','guard')),          -- new tests
  applies     INTEGER,                                                               -- existing tests: 1 applies, 0 obsolete
  criterion   TEXT NOT NULL DEFAULT '',                                              -- what it proves
  reason      TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_job_tests_item ON job_tests(job_item_id);
