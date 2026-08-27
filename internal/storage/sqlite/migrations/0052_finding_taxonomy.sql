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

-- Code Analysis (UC-14 SARIF redesign, slice 3): finding taxonomy + provenance.
-- SARIF carries a security-severity score (CVSS-style 0–10) and rule tags that
-- bucket a finding (security/correctness/quality/performance); `source` records
-- which producer reported it (the built-in scan today; CI/watched/MCP ingest
-- later). The governance gate keys on security_severity >= 7 on open findings.

ALTER TABLE code_findings ADD COLUMN category          TEXT NOT NULL DEFAULT '';
ALTER TABLE code_findings ADD COLUMN security_severity REAL NOT NULL DEFAULT 0;
ALTER TABLE code_findings ADD COLUMN source            TEXT NOT NULL DEFAULT 'scan';
