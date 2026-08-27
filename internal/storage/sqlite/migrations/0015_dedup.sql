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

-- v0.8.1-dev experiment/embeddings: dedup-at-classify support.
--
-- After the agent embeds a new item, it scans for the most similar
-- existing item in the same project and (when similarity crosses a
-- threshold) records the candidate match here. The SPA surfaces it in
-- the pending-review banner so the user can decide if it's a real
-- duplicate before the item joins the corpus permanently.
--
-- Schema is intentionally minimal:
--   similar_to_id    — id of the candidate match (any scratchpad_item
--                      in the same project; nullable; cleared by the
--                      "not a duplicate" action)
--   similarity_score — cosine value, 0..1; null when no candidate
--
-- No FK on similar_to_id: if the target is deleted we don't want a
-- cascade pulling this row down too. Stale references are silently
-- dropped at read time when the target row is missing.

ALTER TABLE scratchpad_items ADD COLUMN similar_to_id    TEXT;
ALTER TABLE scratchpad_items ADD COLUMN similarity_score REAL;
