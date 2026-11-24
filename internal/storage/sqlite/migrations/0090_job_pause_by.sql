-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Pausing to make room (docs/design/job-queue.md, steps 4-5).
--
-- paused_by says who paused a job: 'person' (it stays paused until someone
-- resumes it), 'queue' (paused to make room for an Immediate job; it waits in
-- line and resumes on its own) or 'restart' (running when the server stopped;
-- back in line to resume). Cleared when the job starts again.
--
-- Fix scanner findings becomes resumable: it pauses between items, and a
-- resumed run continues on its branch from the last fixed item.

ALTER TABLE jobs ADD COLUMN paused_by TEXT;
UPDATE workflows SET resumable = 1 WHERE id = 'fix_findings';
