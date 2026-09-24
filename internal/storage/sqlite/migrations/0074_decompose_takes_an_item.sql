-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Decompose takes a scratchpad ITEM, not a path.
--
-- The `file` parameter was a path read by the SERVER. That only works because
-- the server currently runs on the same machine as the person typing, and it is
-- wrong the moment it does not: on a hosted install it reads a filesystem that
-- is nobody's document. A browser cannot supply a path anyway — only bytes.
--
-- Both honest ways in already produce a scratchpad item: dropping a document on
-- a scratchpad uploads it as one, and so does the file picker. So the input was
-- never really a file. Making the parameter an item also removes the separate
-- `scratchpad` question (an item knows which pad it is in) and a duplicate:
-- decompose used to CREATE an item for the document even when the document was
-- already sitting there, leaving two copies with only one of them cited.
--
-- The CLI keeps -file. A path is the right input for a command line, and it
-- creates the item itself before running.

DELETE FROM workflow_params WHERE workflow_id = 'decompose';

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, multiple) VALUES
  ('decompose', 0, 'source_item', 'Document',
   'Pick a document already on a scratchpad, or upload one. Supported: .md, .txt, .odt, .docx.',
   'scratchpad_item', 1, '', 0),
  ('decompose', 1, 'again', 'Read it again from scratch',
   'Off, a re-run reuses the reading passes already completed for this document. On, it re-reads everything — which is what you want when comparing one model against another.',
   'bool', 0, '', 0);
