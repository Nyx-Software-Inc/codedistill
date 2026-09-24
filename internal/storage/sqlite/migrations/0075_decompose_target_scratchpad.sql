-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Where the derived work goes, which need not be where the document lives.
--
-- Specs in one scratchpad and the work they imply in another is the normal
-- arrangement, and until now it was inexpressible: every accepted proposal
-- pointed at the DOCUMENT as its base item, so all of them surfaced on the
-- document's pad whether that made sense or not. Each accepted proposal now
-- gets its own base item, which is what gives it somewhere to be.
--
-- Optional. Empty means the document's own scratchpad — the behaviour anyone
-- already has, so nothing changes for a user who does not care.
--
-- No schema change beyond this row: the answer is recorded in job_params like
-- every other submitted argument, and accept reads it back from the run.

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, multiple) VALUES
  ('decompose', 2, 'target_scratchpad', 'Put the derived work in',
   'Leave empty to use the document''s own scratchpad. Set it to keep specifications in one place and the work they imply in another.',
   'scratchpad', 0, '', 0);
