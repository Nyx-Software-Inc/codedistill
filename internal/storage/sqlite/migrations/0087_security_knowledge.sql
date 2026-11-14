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

-- The security knowledge base (docs/design/security-knowledge-base.md):
-- published sources kept locally and refreshed on a schedule, so the
-- security agent reasons from them, and what people are shown about a
-- security matter is the source's own text.
--
-- Shared by every project and owned by CodeDistill, so kept apart from a
-- project's own knowledge (knowledge_entries).

-- One row per source: what was last fetched, and whether the refresh is on
-- schedule. State (current / retrying / stale / never synced) is computed
-- from these, never stored, so it cannot go stale itself.
CREATE TABLE security_sources (
  id             TEXT PRIMARY KEY,               -- 'cwe', 'kev'
  last_attempt   TIMESTAMP,
  last_success   TIMESTAMP,
  last_error     TEXT NOT NULL DEFAULT '',
  failures       INTEGER NOT NULL DEFAULT 0,     -- consecutive, for back-off
  version        TEXT NOT NULL DEFAULT '',      -- the source's own version
  last_modified  TEXT NOT NULL DEFAULT '',      -- its Last-Modified, for conditional downloads
  entries        INTEGER NOT NULL DEFAULT 0
);

-- One row per entry: a CWE weakness now; attack patterns, verification
-- requirements and cheat-sheet sections later. The text is the source's.
CREATE TABLE security_entries (
  key        TEXT PRIMARY KEY,                   -- 'CWE-916'
  source     TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  url        TEXT NOT NULL DEFAULT '',
  version    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_security_entries_source ON security_entries(source);

-- CISA's Known Exploited Vulnerabilities: a CVE on this list is being
-- exploited in the wild, which changes how urgent a dependency finding is.
CREATE TABLE security_kev (
  cve              TEXT PRIMARY KEY,
  vendor           TEXT NOT NULL DEFAULT '',
  product          TEXT NOT NULL DEFAULT '',
  name             TEXT NOT NULL DEFAULT '',
  date_added       TEXT NOT NULL DEFAULT '',
  description      TEXT NOT NULL DEFAULT '',
  required_action  TEXT NOT NULL DEFAULT '',
  due_date         TEXT NOT NULL DEFAULT '',
  ransomware       TEXT NOT NULL DEFAULT '',
  cwes             TEXT NOT NULL DEFAULT '',     -- comma-separated CWE ids
  version          TEXT NOT NULL DEFAULT ''
);
