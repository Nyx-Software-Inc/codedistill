-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- workflow_workers.worker_type becomes an OPEN vocabulary.
--
-- 0066 pinned it to the five types that existed, and the rename in 0068 carried
-- that CHECK across. Two reasons to drop it, and the second is the one that
-- matters:
--
--   Built-in worker types are a property of the Go code — a type is only real
--   if something knows how to run it — and the database cannot know that. This
--   is the same reasoning that opened model_providers.protocol in 0067.
--
--   User-defined workflows are the direction. A workflow is a name and an
--   ordered set of worker steps, which is DATA; the moment a user can define
--   one, they can name a worker type this migration never heard of. A CHECK
--   here would make that a schema change, which is the difference between a
--   feature and a request.
--
-- Built-in types stay validated in domain.WorkerTypes, so a typo in CodeDistill's
-- own code still fails a test.

CREATE TABLE workflow_workers_new (
  worker_type    TEXT NOT NULL,
  project_id     TEXT REFERENCES projects(id) ON DELETE CASCADE,
  provider_id    TEXT NOT NULL REFERENCES model_providers(id) ON DELETE CASCADE,
  updated_at     TIMESTAMP NOT NULL
);

INSERT INTO workflow_workers_new
  SELECT worker_type, project_id, provider_id, updated_at FROM workflow_workers;

DROP TABLE workflow_workers;
ALTER TABLE workflow_workers_new RENAME TO workflow_workers;

CREATE UNIQUE INDEX idx_workflow_workers_project ON workflow_workers(worker_type, project_id)
  WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX idx_workflow_workers_global ON workflow_workers(worker_type)
  WHERE project_id IS NULL;
