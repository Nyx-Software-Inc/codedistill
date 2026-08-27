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

-- v0.11.x — review decisions (glass-box Phase 4, slice 4.3 — closing the loop).
--
-- The human floor acting on an escalated change: a reviewer approves or rejects
-- the implementation, and that decision becomes evidence the earned-trust dial
-- weighs (approve → positive, reject → negative) — so oversight closes on itself.
--
-- owner_type/owner_id address the work item, mirroring code_anchors /
-- acceptance_criteria / verification_results. commit_sha records which recorded
-- change was judged. Rows accumulate (history); the latest per owner is current.
CREATE TABLE review_decisions (
    id         TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    decision   TEXT NOT NULL,            -- "approved" | "rejected"
    commit_sha TEXT NOT NULL DEFAULT '', -- the change reviewed
    reviewer   TEXT NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_rd_owner ON review_decisions(owner_type, owner_id, created_at DESC);
