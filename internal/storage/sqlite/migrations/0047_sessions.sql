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

-- Multi-user auth (Phase 1a): server-side sessions. The browser cookie carries a
-- random token; `id` here is its SHA-256 — we never store the raw secret, so a
-- DB leak can't be replayed as a session. Resolved per request to set the acting
-- user; revocable (logout / deactivation). Empty in single-user mode (nothing
-- creates sessions without a login). See docs/design/auth.md.
CREATE TABLE sessions (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     TIMESTAMP NOT NULL,
    last_active_at TIMESTAMP NOT NULL,
    expires_at     TIMESTAMP NOT NULL,
    user_agent     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
