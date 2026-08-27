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

-- Per-user API tokens (multi-user auth, Phase 2). Each user mints named tokens
-- for their agents; an agent presenting one acts AS that user (its actions
-- attribute to them) and is authorized to write. Replaces the single shared
-- token for multi-user. `id` is a non-secret management handle; `token_hash` is
-- the SHA-256 of the raw token (shown once on create, never stored raw).
-- See docs/design/auth.md.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);
