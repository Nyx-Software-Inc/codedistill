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

-- Phase 2 (Stage 4) per-item quick wins: optional explicit name on
-- scratchpad items. Until now the "name" displayed in the grid header has
-- been derived via firstLineOrTruncate(content). That works for short notes
-- but loses the user's mental label for an item. Adding an explicit field
-- with empty default — UI falls back to the first-line excerpt when name
-- is empty so existing items render unchanged.
--
-- Per Option A (locked): name does NOT propagate retroactively to derived
-- todo/bug/KB subjects. Items diverge after creation. Future Field_Provenance
-- work (Idea.md Req 23) can add propagation behind a settings flag.

ALTER TABLE scratchpad_items ADD COLUMN name TEXT NOT NULL DEFAULT '';
