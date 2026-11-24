-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- Roles as named sets of permissions (docs/design/workflows-and-roles.md,
-- Part 2). Code asks for a permission; a role is a set an owner can edit.
--
-- A member may hold several roles; what they may do is the union. The old
-- workspace_members.role column stays, kept in step, for anything reading it
-- before this; the roles below are the truth.

CREATE TABLE roles (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  builtin     INTEGER NOT NULL DEFAULT 0,
  created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE role_permissions (
  role_id    TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission TEXT NOT NULL,
  PRIMARY KEY (role_id, permission)
);

CREATE TABLE member_roles (
  workspace_id TEXT NOT NULL,
  user_id      TEXT NOT NULL,
  role_id      TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY (workspace_id, user_id, role_id),
  FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members(workspace_id, user_id) ON DELETE CASCADE
);
CREATE INDEX idx_member_roles_role ON member_roles(role_id);

-- The four built-in roles. Owner holds everything but using the product,
-- which is what a seat pays for: an owner who also works holds Member too.
INSERT INTO roles (id, name, description, builtin) VALUES
  ('owner',  'Owner',  'Accountable for the installation and its license. Holds every permission except using the product.', 1),
  ('admin',  'Admin',  'Looks after the installation without working in it: members, models, settings. Takes no seat.', 1),
  ('lead',   'Lead',   'Directs the work: designs workflows and manages everyone''s jobs.', 1),
  ('member', 'Member', 'Works in projects and runs the workflows a lead has enabled.', 1);

INSERT INTO role_permissions (role_id, permission) VALUES
  ('owner', 'manage_own_jobs'), ('owner', 'manage_all_jobs'), ('owner', 'design_workflows'),
  ('owner', 'manage_members'), ('owner', 'administer'),
  ('admin', 'manage_members'), ('admin', 'administer'),
  ('lead', 'use_product'), ('lead', 'manage_own_jobs'), ('lead', 'manage_all_jobs'), ('lead', 'design_workflows'),
  ('member', 'use_product'), ('member', 'manage_own_jobs');

-- Nobody loses what they can do today. An owner worked in the product, so
-- keeps Member; an admin did too, and could do everything, so becomes Admin
-- and Lead; a member stays Member.
INSERT INTO member_roles (workspace_id, user_id, role_id)
  SELECT workspace_id, user_id, 'owner' FROM workspace_members WHERE role = 'owner';
INSERT INTO member_roles (workspace_id, user_id, role_id)
  SELECT workspace_id, user_id, 'member' FROM workspace_members WHERE role = 'owner';
INSERT INTO member_roles (workspace_id, user_id, role_id)
  SELECT workspace_id, user_id, 'admin' FROM workspace_members WHERE role = 'admin';
INSERT INTO member_roles (workspace_id, user_id, role_id)
  SELECT workspace_id, user_id, 'lead' FROM workspace_members WHERE role = 'admin';
INSERT INTO member_roles (workspace_id, user_id, role_id)
  SELECT workspace_id, user_id, 'member' FROM workspace_members WHERE role = 'member';
