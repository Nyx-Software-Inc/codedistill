-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Points: what the reviewers raised about an item, stored once.
--
-- The first plan-review run logged 14 notes that were about 8 points: the
-- security and code agents each found bcrypt's 72-byte limit, in different
-- words, in both phases. A point is stored once; each time it is raised is a
-- verdict carrying its id, so how often and by whom come from the verdicts.
-- A point raised again and again is a signal, and a round that raises only
-- points already answered is a workflow going in circles.
CREATE TABLE job_points (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_item_id TEXT NOT NULL REFERENCES job_items(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL CHECK (kind IN ('objection','note')),
  about       TEXT NOT NULL DEFAULT 'change',   -- change | setup
  text        TEXT NOT NULL,                    -- its first wording
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_job_points_item ON job_points(job_item_id);

ALTER TABLE claim_verdicts ADD COLUMN point_id TEXT REFERENCES job_points(id) ON DELETE SET NULL;
