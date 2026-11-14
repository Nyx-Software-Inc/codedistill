-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Plan review, and objections that say how much they matter.
--
-- The first real run built and reviewed an approach (SHA-256 for passwords)
-- that a one-paragraph plan review would have rejected, so the fixer now
-- proposes its approach as claims and the challenger lenses rule on it BEFORE
-- any code is written. Claims, verdicts and stats record which phase they
-- belong to.
--
-- The same run kept finding smaller things each round, including the lens's
-- own "low severity, design note" and a complaint about CodeDistill's
-- configuration rather than the fix. Only a BLOCKING objection about the
-- CHANGE sends work back; minor points and notes about the setup are kept for
-- the person instead.

ALTER TABLE job_items ADD COLUMN phase TEXT NOT NULL DEFAULT 'build';      -- plan | build: what the item is in now

ALTER TABLE job_claims ADD COLUMN phase TEXT NOT NULL DEFAULT 'build';

ALTER TABLE claim_verdicts ADD COLUMN phase TEXT NOT NULL DEFAULT 'build';
ALTER TABLE claim_verdicts ADD COLUMN severity TEXT NOT NULL DEFAULT 'blocking'; -- blocking | minor | note
ALTER TABLE claim_verdicts ADD COLUMN about TEXT NOT NULL DEFAULT 'change';      -- change | setup

ALTER TABLE agent_runs ADD COLUMN phase TEXT NOT NULL DEFAULT 'build';
