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

-- v0.11.x — acceptance criteria (glass-box Phase 2, slice 1).
--
-- Measurable intent: an item's "definition of done" as a list of discrete,
-- individually-checkable assertions. The keystone the verification portfolio
-- and trust dial later consume. SOFT in this slice — criteria are a ratified
-- contract, not a gate; gating lands with the governance phase.
--
-- owner_type/owner_id mirror code_anchors addressing so criteria and the
-- throughline share one model (polymorphic, no FK — same as code_anchors).
--
-- state:             proposed (AI draft or fresh) → accepted (human-ratified)
--                    → satisfied | failed (Phase 3 verification); rejected =
--                    dismissed.
-- verification_kind: reserves the "how is this checked" seam — test | check |
--                    human | unspecified — for the type→contract idea.
-- satisfied_by:      nullable commit SHA the verification phase fills in so the
--                    throughline can later show "commit X satisfied criterion 3".
CREATE TABLE acceptance_criteria (
    id                TEXT PRIMARY KEY,
    owner_type        TEXT NOT NULL,
    owner_id          TEXT NOT NULL,
    position          INTEGER NOT NULL DEFAULT 0,
    text              TEXT NOT NULL,
    verification_kind TEXT NOT NULL DEFAULT 'unspecified',
    state             TEXT NOT NULL DEFAULT 'proposed',
    provenance        TEXT NOT NULL DEFAULT 'user-authored',
    satisfied_by      TEXT,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);

CREATE INDEX idx_ac_owner ON acceptance_criteria(owner_type, owner_id, position);
