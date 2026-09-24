-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Model providers, and the roles that use them.
--
-- TWO CONCEPTS, NOT ONE, because they change independently. A provider is a
-- CONNECTION — where a model lives and how to talk to it. A role is a JOB
-- CodeDistill needs done. Swapping which model classifies captures should not
-- mean re-entering an endpoint and an API key, and pointing two roles at one
-- provider should be one row, not two copies.
--
-- The existing verify.reviewer_model setting named a model on the single
-- configured endpoint. That was enough while everything ran on one local
-- Ollama; it cannot express "classify locally, decompose on a bigger model
-- elsewhere", which measurement showed is what this actually needs — a
-- correctly-configured 7B found 27 of 37 written-down requirements in a real
-- document, and took 46 minutes.

CREATE TABLE model_providers (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,              -- user-facing: "local ollama", "workstation 70b"

  -- How to talk to it. Matches ollama.ParseProtocol.
  protocol       TEXT NOT NULL DEFAULT 'ollama'
                 CHECK (protocol IN ('ollama','openai')),
  endpoint       TEXT NOT NULL,
  model          TEXT NOT NULL,

  -- Encrypted by internal/secrets (the "cdenc1:" marker). NEVER plain text:
  -- the database is a single file users copy, sync and attach to bug reports.
  api_key        TEXT NOT NULL DEFAULT '',

  -- How much prompt this provider will actually read. Not a tuning knob — a
  -- correctness setting. Ollama defaults to 4096 whatever the model supports
  -- and truncates silently rather than erroring, which corrupted every
  -- oversized prompt in this codebase since v1 without a single error. A job
  -- that needs more than this must say so instead of being quietly lied to.
  context_tokens INTEGER NOT NULL DEFAULT 4096,

  -- Whether the provider sends data off this machine. Recorded rather than
  -- inferred from the endpoint: "localhost" is not a reliable signal (an SSH
  -- tunnel is local-looking and remote), and a user deserves a truthful answer
  -- to "does my spec leave my laptop" that does not depend on URL parsing.
  is_local       INTEGER NOT NULL DEFAULT 1,

  enabled        INTEGER NOT NULL DEFAULT 1,
  last_ok_at     TIMESTAMP,                  -- last successful health probe
  last_error     TEXT,
  created_at     TIMESTAMP NOT NULL,
  updated_at     TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_model_providers_name ON model_providers(name);

-- Which provider does which job.
--
-- Scoped to a project, with project_id NULL meaning "the default for every
-- project", mirroring the settings cascade already in the product. A role with
-- no row falls back to the app's configured model, so an existing install keeps
-- working with nothing configured.
CREATE TABLE model_roles (
  role           TEXT NOT NULL
                 CHECK (role IN ('classifier','decomposer','reviewer','solutioner','challenger')),
  project_id     TEXT REFERENCES projects(id) ON DELETE CASCADE,
  provider_id    TEXT NOT NULL REFERENCES model_providers(id) ON DELETE CASCADE,
  updated_at     TIMESTAMP NOT NULL
);

-- One provider per role per scope. The partial index covers the global default
-- (project_id NULL), which a plain UNIQUE would not constrain since NULLs do
-- not compare equal.
CREATE UNIQUE INDEX idx_model_roles_project ON model_roles(role, project_id)
  WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX idx_model_roles_global ON model_roles(role)
  WHERE project_id IS NULL;
