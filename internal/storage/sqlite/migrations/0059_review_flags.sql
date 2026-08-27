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

-- 0059: review_flags — a persistent "this change needs another look" marker,
-- distinct from the human review_decisions (which feed the trust dial) and from
-- the computed risk score. Pass 2 of skill provenance uses it to route changes
-- made under a superseded/flawed skill version back into the review queue.
--
-- A flag is attention-routing ONLY: while active (cleared_at IS NULL) it forces
-- the item into the review queue with its reason, but it is NEVER counted as
-- trust evidence (an unresolved suspicion is not a verdict). It clears when a
-- human approves the item (or an explicit resolve).
CREATE TABLE review_flags (
    id          TEXT PRIMARY KEY,
    owner_type  TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL,
    cleared_at  TIMESTAMP
);
CREATE INDEX idx_review_flags_owner ON review_flags(owner_type, owner_id);
