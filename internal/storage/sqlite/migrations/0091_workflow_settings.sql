-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- What a workflow declares about itself (docs/design/workflows-and-roles.md,
-- Part 1). The queue reads these and never a workflow's name.
--
--   resumable       can pause and resume: its jobs stop at a checkpoint and
--                   carry on, as the same job, where they stopped
--   max_priority    the highest priority its jobs may have: high or immediate
--   fixed_priority  its jobs keep the priority they were submitted with:
--                   nobody raises it and waiting does not either
--
-- The built-ins' values. Fixing scanner findings keeps the resume contract
-- and may be Immediate. Splitting a document was marked resumable, but running
-- it again starts a new run rather than carrying on the same job, so it is not,
-- until its runner keeps the contract.

ALTER TABLE workflows ADD COLUMN max_priority TEXT NOT NULL DEFAULT 'high'
  CHECK (max_priority IN ('high','immediate'));
ALTER TABLE workflows ADD COLUMN fixed_priority INTEGER NOT NULL DEFAULT 0;
UPDATE workflows SET max_priority = 'immediate' WHERE id = 'fix_findings';
UPDATE workflows SET resumable = 0 WHERE id = 'decompose';
