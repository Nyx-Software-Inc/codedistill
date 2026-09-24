-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Workflows as DATA.
--
-- A workflow is a kind of job — decompose a document, draft an architecture —
-- and it runs workers. Until now that was hardcoded Go: jobs.type held a string
-- and the steps lived in a function. Making the definition a row is what lets a
-- user eventually build their own, which is the stated direction, and it is
-- cheaper to do before the UI exists than to retrofit after.
--
-- Built-ins are seeded rows rather than a separate concept. They carry
-- builtin=1 so the UI can refuse to delete them, and nothing else about them is
-- special — which is the test of whether user-defined workflows will actually
-- work.

CREATE TABLE workflows (
  id           TEXT PRIMARY KEY,   -- matches jobs.type: 'decompose', 'arch_draft', or a user's own
  name         TEXT NOT NULL,      -- "Decompose a document"
  description  TEXT NOT NULL DEFAULT '',

  -- Seeded by CodeDistill. Not deletable, and its steps are not editable:
  -- decompose's four layers are code, and pretending otherwise in the UI would
  -- offer an edit that silently does nothing.
  builtin      INTEGER NOT NULL DEFAULT 0,

  -- Whether a stopped run can continue where it left off. Decompose checkpoints
  -- every reading pass and architecture drafting derives its remaining work
  -- from its own output, so both resume; an agent step that has already changed
  -- files may not. Declared per workflow because the UI must not offer a
  -- Resume button that quietly restarts from zero.
  resumable    INTEGER NOT NULL DEFAULT 0,

  enabled      INTEGER NOT NULL DEFAULT 1,
  created_at   TIMESTAMP NOT NULL,
  updated_at   TIMESTAMP NOT NULL
);

-- The workers a workflow needs, in order.
--
-- The provider lives HERE, on the step, because "this workflow needs a
-- decomposer and it runs on Claude" is one fact, not two. A NULL provider falls
-- back to the global binding in workflow_workers, then to the model the app was
-- started with — so an install with nothing configured still runs.
CREATE TABLE workflow_steps (
  workflow_id   TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  ordinal       INTEGER NOT NULL,
  worker_type   TEXT NOT NULL,
  label         TEXT NOT NULL DEFAULT '',   -- what this step does, in the user's terms

  -- What this step needs to read. Checked against the provider's context
  -- window so a mismatch is caught at configuration time rather than as a
  -- silently truncated answer forty minutes into a run.
  needs_context INTEGER NOT NULL DEFAULT 0,

  provider_id   TEXT REFERENCES model_providers(id) ON DELETE SET NULL,

  PRIMARY KEY (workflow_id, ordinal)
);

CREATE INDEX idx_workflow_steps_worker ON workflow_steps(worker_type);

-- Built-ins, seeded. needs_context comes from measurement: decomposition
-- carries the whole document in every reading pass (~7k tokens for a 159-line
-- spec), while characterising one component is a fraction of that.
INSERT INTO workflows (id, name, description, builtin, resumable, enabled, created_at, updated_at) VALUES
  ('decompose', 'Decompose a document',
   'Read a specification and derive the use cases, todos, bugs and knowledge it implies. Every proposal cites the lines it came from.',
   1, 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('arch_draft', 'Draft the architecture',
   'Describe each component of the repository. The structure is free and exact; the descriptions are written by a model, one component at a time.',
   1, 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT INTO workflow_steps (workflow_id, ordinal, worker_type, label, needs_context) VALUES
  ('decompose', 0, 'decomposer',
   'Reads the whole document and works out what it implies', 16384),
  ('arch_draft', 0, 'reviewer',
   'Describes one component at a time from its files', 8192);
