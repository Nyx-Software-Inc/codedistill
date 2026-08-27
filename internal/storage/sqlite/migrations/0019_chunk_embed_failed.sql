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

-- v0.8.1-dev — let the code-chunk indexer give up on chunks the embedder
-- can never make sense of (typically SVG / minified bundles / base64
-- blobs that slipped past the IsBinary heuristic).
--
-- Without this, every 60s indexer tick re-attempts the same N oldest
-- failing chunks first (ORDER BY created_at LIMIT 50), blocking healthy
-- chunks behind them from ever getting embedded.
--
-- embed_failed_at is set when embedWithFallback returns its
-- "content too unusual" sentinel after all halve-and-retry attempts.
-- embed_last_error captures the final error string for triage.
-- ListUnembeddedCodeChunks filters these out; a future SPA action can
-- clear the column to retry (e.g. after upgrading the embedder).

ALTER TABLE code_chunks ADD COLUMN embed_failed_at  TIMESTAMP;
ALTER TABLE code_chunks ADD COLUMN embed_last_error TEXT;
