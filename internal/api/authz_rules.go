// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

package api

import (
	"net/http"
	"strings"

	"codedistill/internal/domain"
)

// The checks only a handler can make: whose settings, which setting, whose
// job, whose comment. The route itself was already allowed (authz.go).

// serverWideUserKeys are user settings that are really the installation's:
// the server reads them from the built-in local user, whoever is signed in.
// Changing one needs "administer the installation"; on a multi-user server
// they are the same for everyone.
var serverWideUserKeys = []string{
	"conductor.step_limit", "jobs.", "model.classifier", "setup.", "security.",
	"indexing.battery.", "blob.", "mcp.export.",
}

// secretUserKeys are server-wide settings that carry credentials: reading
// them needs "administer the installation" too.
var secretUserKeys = []string{"mcp.export."}

func keyIn(key string, list []string) bool {
	for _, k := range list {
		if key == k || (strings.HasSuffix(k, ".") && strings.HasPrefix(key, k)) {
			return true
		}
	}
	return false
}

// settingsOwner decides whose settings a request under /users/{uid}/settings
// reaches, and whether the caller may. It returns the user id to read or
// write, or writes the refusal and returns "".
//
// Server-wide keys live with the built-in local user. Every other key is
// personal: on a multi-user server a request naming the local user for one
// reaches the caller's own (the app names "local" for its own preferences,
// and must not have one person's layout overwrite everyone's), and a request
// naming someone else is refused.
func (s *Server) settingsOwner(w http.ResponseWriter, r *http.Request, key string, write bool) string {
	uid := r.PathValue("uid")
	if s.auth == nil {
		return uid // the desktop: one person
	}
	if key != "" && keyIn(key, serverWideUserKeys) {
		if uid != localUserID {
			writeMsg(w, http.StatusBadRequest, "this is a server-wide setting: it belongs to the installation, not a person")
			return ""
		}
		if (write || keyIn(key, secretUserKeys)) && !s.can(r, domain.PermAdminister) {
			writeMsg(w, http.StatusForbidden, "your role does not allow this: it needs "+permissionName(domain.PermAdminister))
			return ""
		}
		return localUserID
	}
	me := s.currentUser(r)
	if uid == localUserID || uid == me {
		return me
	}
	writeMsg(w, http.StatusForbidden, "these are someone else's settings")
	return ""
}

// projectKeyPermission is what changing a project setting needs. Settings that
// hold a command the server runs, a path on the server, or a governance level
// are the installation's: a member who could set them could run anything on
// the server, read any directory, or switch off the gates. Settings that shape
// how agents work belong to whoever designs workflows. The rest are the
// project's own.
func projectKeyPermission(key string) string {
	switch {
	case key == "conductor.fixer_command",
		strings.HasPrefix(key, "verify.") && strings.HasSuffix(key, "_command"),
		key == "analysis.watch_dir",
		strings.HasPrefix(key, "governance."),
		strings.HasPrefix(key, "blob."):
		return domain.PermAdminister
	case strings.HasPrefix(key, "conductor."),
		key == "verify.reviewer_model", key == "verify.ai_review":
		return domain.PermDesignWorkflows
	}
	return domain.PermUseProduct
}

// allowProjectKey refuses a project setting change the caller's role does not
// allow, and reports whether it was allowed.
func (s *Server) allowProjectKey(w http.ResponseWriter, r *http.Request, key string) bool {
	need := projectKeyPermission(key)
	if s.can(r, need) {
		return true
	}
	writeMsg(w, http.StatusForbidden, "your role does not allow changing "+key+": it needs "+permissionName(need))
	return false
}

// allowRepoPath refuses setting where a project's code lives on the server to
// anyone who may not administer the installation: the path is a directory on
// the server, and the Files panel and the agents read under it.
func (s *Server) allowRepoPath(w http.ResponseWriter, r *http.Request) bool {
	if s.can(r, domain.PermAdminister) {
		return true
	}
	writeMsg(w, http.StatusForbidden,
		"where a project's code lives is a folder on the server, so setting it needs "+permissionName(domain.PermAdminister)+" — ask an admin to set it")
	return false
}

// allowJobControl refuses pausing, resuming, cancelling or reprioritising a
// job the caller may not manage: their own with "manage their own jobs",
// anyone's with "manage everyone's jobs".
func (s *Server) allowJobControl(w http.ResponseWriter, r *http.Request, j *domain.Job) bool {
	if s.can(r, domain.PermManageAllJobs) {
		return true
	}
	if j.CreatedBy == s.currentUser(r) && s.can(r, domain.PermManageOwnJobs) {
		return true
	}
	if j.CreatedBy == s.currentUser(r) {
		writeMsg(w, http.StatusForbidden, "your role does not allow this: it needs "+permissionName(domain.PermManageOwnJobs))
	} else {
		writeMsg(w, http.StatusForbidden, "this is someone else's job: managing it needs "+permissionName(domain.PermManageAllJobs))
	}
	return false
}
