-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- model_providers.protocol becomes an OPEN vocabulary.
--
-- 0066 pinned it to ('ollama','openai') with a CHECK, and adding Anthropic and
-- Gemini immediately cost a table rebuild. Every future adapter would cost
-- another — for a value whose correctness is a property of the Go code, not of
-- the data. A protocol is only valid if an adapter implements it, and SQLite
-- cannot know that.
--
-- So validation moves to domain.ValidProtocol, which is where the adapters
-- live and where a test already fails if a vendor names a protocol nothing
-- implements. This matches jobs.type, left open for the same reason: the
-- conductor will add several, and a migration per addition is a tax on being
-- able to add them.
--
-- The rebuild copies rather than recreates: a provider row carries an
-- ENCRYPTED api_key, and losing one means a credential the user must find and
-- re-enter.

CREATE TABLE model_providers_new (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  -- No CHECK. Validated in Go, where the adapters are.
  protocol       TEXT NOT NULL DEFAULT 'ollama',
  endpoint       TEXT NOT NULL,
  model          TEXT NOT NULL,
  api_key        TEXT NOT NULL DEFAULT '',
  context_tokens INTEGER NOT NULL DEFAULT 4096,
  is_local       INTEGER NOT NULL DEFAULT 1,
  enabled        INTEGER NOT NULL DEFAULT 1,
  last_ok_at     TIMESTAMP,
  last_error     TEXT,
  created_at     TIMESTAMP NOT NULL,
  updated_at     TIMESTAMP NOT NULL
);

INSERT INTO model_providers_new
  SELECT id, name, protocol, endpoint, model, api_key, context_tokens,
         is_local, enabled, last_ok_at, last_error, created_at, updated_at
  FROM model_providers;

DROP TABLE model_providers;
ALTER TABLE model_providers_new RENAME TO model_providers;

CREATE UNIQUE INDEX idx_model_providers_name ON model_providers(name);
