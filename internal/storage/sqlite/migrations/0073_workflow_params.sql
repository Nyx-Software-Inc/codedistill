-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- What a workflow needs to be asked before it can run.
--
-- Submission has to work the same way for a workflow nobody has written yet, so
-- the questions are rows rather than a hand-built form per workflow. The dialog
-- reads this table and renders it; adding a workflow adds no UI.
--
-- The alternative — a form per workflow in the front end — makes the
-- user-defined workflow that this product is heading towards unsubmittable
-- without a release, which defeats the point of having made workflows data.

CREATE TABLE workflow_params (
  workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  ordinal     INTEGER NOT NULL,      -- display order
  key         TEXT NOT NULL,         -- what the runner reads it by
  label       TEXT NOT NULL,         -- what the person is asked
  help        TEXT NOT NULL DEFAULT '',

  -- OPEN vocabulary, validated in Go rather than by a CHECK.
  --
  --   file            a path on the machine running CodeDistill
  --   directory       likewise, a folder
  --   scratchpad      pick one of this project's scratchpads
  --   scratchpad_item pick an item within one
  --   project         pick a project
  --   text            free text
  --   bool            a checkbox
  --   select          one of `options`
  --
  -- A type the UI does not recognise renders as text rather than vanishing: a
  -- param silently dropped is a job submitted with a missing argument.
  type        TEXT NOT NULL,

  required    INTEGER NOT NULL DEFAULT 1,
  default_val TEXT NOT NULL DEFAULT '',

  -- For `select`: newline-separated "value|label" pairs.
  options     TEXT NOT NULL DEFAULT '',

  -- A param that accepts MANY values. This is how batching arrives: "run this
  -- over the twelve bugs in that scratchpad" is a scratchpad_item param marked
  -- multiple, not a separate batch feature.
  multiple    INTEGER NOT NULL DEFAULT 0,

  PRIMARY KEY (workflow_id, key)
);

-- What the run was actually asked for, kept with the job.
--
-- Stored rather than reconstructed: a workflow's parameters can be edited after
-- a run, and a history entry that reports today's declaration instead of what
-- was submitted is a quiet lie about what happened.
CREATE TABLE job_params (
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  key    TEXT NOT NULL,
  value  TEXT NOT NULL,
  PRIMARY KEY (job_id, key)
);

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, multiple) VALUES
  ('decompose', 0, 'file', 'Document',
   'A specification to read: .md, .txt, .odt or .docx.', 'file', 1, '', 0),
  ('decompose', 1, 'scratchpad', 'Put the document in',
   'The document itself is stored as an item here, because every proposal cites its lines.',
   'scratchpad', 1, '', 0),
  ('decompose', 2, 'again', 'Read it again from scratch',
   'Off, a re-run reuses the passes already completed for this document. On, it re-reads everything.',
   'bool', 0, '', 0),
  ('arch_draft', 0, 'scope', 'Limit to one directory',
   'Leave empty to describe every component that still needs it.', 'text', 0, '', 0);
