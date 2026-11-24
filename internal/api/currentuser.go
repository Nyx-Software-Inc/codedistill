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
	"codedistill/internal/domain"
	"context"
	"net/http"
)

// localUserID is the implicit single-user identity (seeded in migration 0006).
// Every action attributes to it until per-user auth lands.
const localUserID = "local"

// localWorkspaceID is the implicit single workspace a self-hosted server uses.
// Build-neutral (read by the free /me endpoint), so it lives here rather than
// in the paid membership.go which the CE strips.
const localWorkspaceID = "local"

// isAdmin reports whether the acting user is an owner/admin of the workspace.
// In single-user mode the 'local' user is the seeded owner, so the localhost SPA
// is always admin. Build-neutral (used by the free file browser's access gate),
// so it lives here rather than in the CE-stripped admin_members.go — otherwise
// the oss build can't compile fs_browse.go's admin check.
//
// Kept for what still asks the old question; new code asks for a permission
// (authz.go). An anonymous request on a multi-user server holds none (audit C2).
func (s *Server) isAdmin(r *http.Request) bool {
	return s.can(r, domain.PermManageMembers) || s.can(r, domain.PermAdminister)
}

type userCtxKey struct{}

// withUser returns a context carrying the authenticated user id. Today nothing
// sets it (single-user, localhost-only), so userFromContext falls back to the
// local user — attribution works now and "just works" once the future auth
// middleware starts populating this.
func withUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userCtxKey{}, userID)
}

// userFromContext resolves WHO is acting — the authenticated user id, or the
// local user in single-user mode. This is the ONE seam per-user auth changes:
// auth middleware sets the context value, everything else reads it through here.
func userFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userCtxKey{}).(string); ok && v != "" {
		return v
	}
	return localUserID
}

// currentUser resolves the acting user for an HTTP request.
func (s *Server) currentUser(r *http.Request) string {
	return userFromContext(r.Context())
}

type authedCtxKey struct{}

// withAuthenticated marks a request as carrying a verified identity (a valid
// per-user token or session) — so the write-auth gate can authorize it.
func withAuthenticated(ctx context.Context) context.Context {
	return context.WithValue(ctx, authedCtxKey{}, true)
}

func isAuthenticated(ctx context.Context) bool {
	v, _ := ctx.Value(authedCtxKey{}).(bool)
	return v
}
