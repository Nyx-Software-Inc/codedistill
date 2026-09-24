-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Document decomposition: one document in, many PROPOSED work items out.
--
-- Proposals are not items. Nothing exists in a scratchpad until a human accepts
-- one, which is the whole contract of the review surface — "nothing exists yet"
-- has to be literally true or the review is theatre. So they live here, keyed
-- by the job that produced them, and graduate into real items on accept.
--
-- This is the output table the `jobs` row points at through
-- (scope_kind='scratchpad_item', scope_id=<the document>). Jobs deliberately
-- has no result column: decomposition produces proposals, architecture drafting
-- produces node descriptions, an agent will produce a commit, and a shared blob
-- would grow a case per job type.

-- One row per decomposition, holding what the run learned ABOUT the document.
-- Separate from `jobs` because these are facts about a document, not about a
-- run, and the job table must not grow columns per job type.
CREATE TABLE decompose_runs (
  job_id            TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_item_id    TEXT NOT NULL,          -- the scratchpad item holding the document
  source_label      TEXT,                   -- filename, for the dashboard

  -- Document identity, so the same file is not read twice for 15 minutes.
  -- The HASH is the identity: same bytes, same document, exactly. Size and
  -- modified time are for the human deciding about a revision — they are poor
  -- identity on their own (two edits of equal length collide; a copy has a
  -- fresh mtime with identical content) but they are exactly what someone
  -- wants to see when told "you already ran this".
  source_hash       TEXT NOT NULL DEFAULT '',
  source_bytes      INTEGER NOT NULL DEFAULT 0,
  source_modified   TIMESTAMP,

  sentences         INTEGER NOT NULL DEFAULT 0,
  excluded_lines    INTEGER NOT NULL DEFAULT 0,  -- code fences + tables, not prose
  tables_read       INTEGER NOT NULL DEFAULT 0,  -- record tables imported
  tables_declined   INTEGER NOT NULL DEFAULT 0,  -- no subject column: matrices, layout

  -- Reconciliation between the two readings of the same document. The
  -- table_only count is the honest answer to "how would I know something was
  -- missed", which coverage alone cannot give.
  corroborated      INTEGER NOT NULL DEFAULT 0,
  table_only        INTEGER NOT NULL DEFAULT 0,
  prose_only        INTEGER NOT NULL DEFAULT 0,

  declined_nodes    INTEGER NOT NULL DEFAULT 0,  -- layer 4 said "not work"
  rejected_moves    INTEGER NOT NULL DEFAULT 0,  -- layer 2 claims the document could not support
  inferred_links    INTEGER NOT NULL DEFAULT 0,  -- elaborations attached without the model naming a target
  model             TEXT,
  created_at        TIMESTAMP NOT NULL
);

-- "Have I read this exact document before?" — the check that stops a 15-minute
-- re-read, and that distinguishes an identical re-drop from a revision worth
-- reading again.
CREATE INDEX idx_decompose_runs_hash ON decompose_runs(project_id, source_hash);
CREATE INDEX idx_decompose_runs_label ON decompose_runs(project_id, source_label);

CREATE TABLE decompose_proposals (
  id                TEXT PRIMARY KEY,
  job_id            TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

  kind              TEXT NOT NULL CHECK (kind IN ('todo','bug','kb','use_case')),
  subject           TEXT NOT NULL,
  body              TEXT,

  -- Where it came from. 'table' means a row was READ, fields and all; 'prose'
  -- means it was inferred from sentences. A table row is the stronger evidence
  -- — the author wrote the requirement down as a requirement — and the review
  -- surface ranks on it.
  origin            TEXT NOT NULL CHECK (origin IN ('prose','table')),
  corroborated      INTEGER NOT NULL DEFAULT 0,  -- both readings found it
  inferred_link     INTEGER NOT NULL DEFAULT 0,  -- built on an elaboration whose target was guessed
  external_ref      TEXT,                        -- the author's own id, e.g. UC-07
  priority          TEXT,                        -- as written, never normalised

  -- Citation, computed at layer 1 before any model ran. JSON [[start,end],...]
  -- because a proposal built from scattered sentences cites several ranges, and
  -- a citation that cannot express that would have to lie or truncate.
  lines             TEXT NOT NULL DEFAULT '[]',

  -- accepted -> created_item_id; linked -> linked_item_id; rejected ->
  -- reject_reason. Three outcomes, not two: "already tracked as BUG-18" is a
  -- duplicate, not a rejection, and treating it as one throws the citation away
  -- instead of attaching it to the work already there.
  status            TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','accepted','rejected','linked')),
  created_item_id   TEXT,
  linked_item_id    TEXT,
  reject_reason     TEXT,
  decided_at        TIMESTAMP,
  decided_by        TEXT,

  created_at        TIMESTAMP NOT NULL
);

-- The review surface reads one job's proposals grouped by kind.
CREATE INDEX idx_decompose_proposals_job ON decompose_proposals(job_id, kind, status);
-- Rejection memory is project-scoped, not document-scoped: "out of scope for
-- Q4" is a judgement about the work and should outlive the draft it was made
-- against.
CREATE INDEX idx_decompose_proposals_rejected ON decompose_proposals(project_id, status);
