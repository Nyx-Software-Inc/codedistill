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

-- Rich Canvas Slice 3: OpenGraph metadata on link items.
--
-- 'link' is already a valid content_type (since migration 0001), so
-- this migration is just ALTER TABLE ADD COLUMN — no table rebuild
-- needed. Plain ADD COLUMN is FK-safe and quick.
--
-- Columns:
--   og_title         — <meta property="og:title">          (TEXT, nullable)
--   og_description   — <meta property="og:description">    (TEXT, nullable)
--   og_image_sha     — SHA of the og:image, stored in the BlobStore
--                      so the card thumbnail loads instantly without
--                      hitting the external host every render. (TEXT,
--                      nullable)
--   og_fetched_at    — when we last attempted the fetch. NULL means
--                      "never tried"; set on both success AND failure
--                      so we can avoid re-fetching items that
--                      legitimately have no OG data.
--
-- The fetcher (internal/api/og.go) populates these asynchronously
-- after item create — the SPA polls and the card re-renders when
-- the fetch lands.
--
-- GC note: og_image_sha references a blob in the BlobStore. The GC
-- sweeper's ListLiveBlobShas query is widened in this slice to UNION
-- blob_sha and og_image_sha so OG thumbnails don't get swept.

ALTER TABLE scratchpad_items ADD COLUMN og_title       TEXT;
ALTER TABLE scratchpad_items ADD COLUMN og_description TEXT;
ALTER TABLE scratchpad_items ADD COLUMN og_image_sha   TEXT;
ALTER TABLE scratchpad_items ADD COLUMN og_fetched_at  TIMESTAMP;

-- Partial index on og_image_sha for the widened ListLiveBlobShas query.
CREATE INDEX idx_items_og_image_sha
    ON scratchpad_items(og_image_sha)
    WHERE og_image_sha IS NOT NULL;
