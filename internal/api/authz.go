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
	"context"
	"net/http"
	"strings"

	"codedistill/internal/domain"
)

// Authorization (docs/design/workflows-and-roles.md, Part 2).
//
// Every request to the API passes here, after its caller has been identified
// and before its handler runs. Two questions, in order:
//
//  1. Who is calling? On the desktop (no sign-in configured) the caller is the
//     local owner, as it always was: reads are open on this machine, writes
//     need the server's token (the app carries it in a cookie). Served beyond
//     this machine, reads need the token too. On a multi-user server every
//     request needs a signed-in person (session or personal API token) or the
//     server's own token; nothing is open but signing in.
//  2. May they? Each route needs one permission, from routeRules below; a route
//     not listed there needs "use the product". The table is the single place
//     this is decided, so a new route is protected by default.
//
// A few handlers then check what only they can know: whose job it is, whose
// settings, whose comment.

// routeRule is what a route needs.
type routeRule struct {
	// public: no caller needed at all. Only what signing in needs.
	public bool
	// perm: the permission it needs; "" means any signed-in person (the
	// handler may check more).
	perm string
}

// routeRules maps a route, as registered, to what it needs. Not listed: any
// route needs "use the product", which is right for everything to do with
// projects and their contents.
var routeRules = map[string]routeRule{
	// Signing in, and what the app needs to show the sign-in screen.
	"GET /api/v1/auth/login":          {public: true},
	"GET /api/v1/auth/callback":       {public: true},
	"GET /api/v1/me":                  {public: true},
	"GET /api/v1/version":             {public: true},
	"POST /api/v1/auth/token-session": {public: true},

	// Any signed-in person: themselves, their tokens, their own settings
	// (settings.go checks whose), and what the app reads to draw itself.
	"PATCH /api/v1/me":                          {},
	"POST /api/v1/auth/logout":                  {},
	"GET /api/v1/tokens":                        {},
	"POST /api/v1/tokens":                       {},
	"DELETE /api/v1/tokens/{id}":                {},
	"GET /api/v1/users/{id}":                    {},
	"GET /api/v1/users/{uid}/settings":          {},
	"GET /api/v1/users/{uid}/settings/{key}":    {},
	"PUT /api/v1/users/{uid}/settings/{key}":    {},
	"DELETE /api/v1/users/{uid}/settings/{key}": {},
	"GET /api/v1/license":                       {},
	"GET /api/v1/power":                         {},
	"GET /api/v1/roles":                         {},
	"GET /api/v1/workflows":                     {},
	"GET /api/v1/workflow-workers":              {},
	"GET /api/v1/model-providers":               {},
	"GET /api/v1/model-vendors":                 {},
	"GET /api/v1/security-knowledge":            {},
	"GET /api/v1/jobs/slots":                    {},
	"GET /api/v1/agents":                        {},
	"GET /api/v1/setup/status":                  {},

	// Jobs: jobs.go and job_queue.go check whose job it is.
	"POST /api/v1/jobs/{id}/pause":   {},
	"POST /api/v1/jobs/{id}/cancel":  {},
	"POST /api/v1/jobs/{id}/resume":  {},
	"PUT /api/v1/jobs/{id}/priority": {},

	// Workflows: which models their steps run on.
	"PUT /api/v1/workflows/{id}/steps/{ordinal}/provider": {perm: domain.PermDesignWorkflows},
	"PUT /api/v1/workflow-workers/{type}":                 {perm: domain.PermDesignWorkflows},

	// Members and roles.
	"GET /api/v1/members":                  {perm: domain.PermManageMembers},
	"GET /api/v1/members/seats":            {perm: domain.PermManageMembers},
	"POST /api/v1/members/{id}/deactivate": {perm: domain.PermManageMembers},
	"POST /api/v1/members/{id}/reactivate": {perm: domain.PermManageMembers},
	"PUT /api/v1/members/{id}/role":        {perm: domain.PermManageMembers},
	"PUT /api/v1/members/{id}/roles":       {perm: domain.PermManageMembers},
	"PUT /api/v1/roles/{id}":               {perm: domain.PermManageMembers},
	"DELETE /api/v1/roles/{id}":            {perm: domain.PermManageMembers},

	// The installation.
	"GET /api/v1/auth/token":                  {perm: domain.PermAdminister},
	"POST /api/v1/auth/token/regenerate":      {perm: domain.PermAdminister},
	"POST /api/v1/license/redeem":             {perm: domain.PermAdminister},
	"POST /api/v1/license/refresh":            {perm: domain.PermAdminister},
	"GET /api/v1/workspaces":                  {perm: domain.PermAdminister},
	"GET /api/v1/workspaces/{id}":             {perm: domain.PermAdminister},
	"POST /api/v1/workspaces":                 {perm: domain.PermAdminister},
	"PATCH /api/v1/workspaces/{id}":           {perm: domain.PermAdminister},
	"DELETE /api/v1/workspaces/{id}":          {perm: domain.PermAdminister},
	"POST /api/v1/model-providers":            {perm: domain.PermAdminister},
	"PATCH /api/v1/model-providers/{id}":      {perm: domain.PermAdminister},
	"DELETE /api/v1/model-providers/{id}":     {perm: domain.PermAdminister},
	"POST /api/v1/model-providers/{id}/test":  {perm: domain.PermAdminister},
	"POST /api/v1/model-providers/discover":   {perm: domain.PermAdminister},
	"POST /api/v1/setup/ollama/start":         {perm: domain.PermAdminister},
	"POST /api/v1/setup/pull":                 {perm: domain.PermAdminister},
	"POST /api/v1/setup/complete":             {perm: domain.PermAdminister},
	"POST /api/v1/security-knowledge/refresh": {perm: domain.PermAdminister},
	"GET /api/v1/system":                      {perm: domain.PermAdminister},
	"GET /api/v1/fs/browse":                   {perm: domain.PermAdminister},
	// Where MCP export pushes data, and with which credentials.
	"POST /api/v1/mcp-export/discover":        {perm: domain.PermAdminister},
	"POST /api/v1/mcp-export/suggest-mapping": {perm: domain.PermAdminister},
	"POST /api/v1/mcp-export/migrate":         {perm: domain.PermAdminister},
}

// ruleFor is what a route needs: its entry, else "use the product".
func ruleFor(pattern string) routeRule {
	if r, ok := routeRules[pattern]; ok {
		return r
	}
	return routeRule{perm: domain.PermUseProduct}
}

type permsKey struct{}

type denialKey struct{}

// denial is why a caller who presented an identity is still not let in (a
// proxy vouched for them, but they are deactivated, or out of seats): the gate
// says it instead of "sign in first".
func denial(ctx context.Context) string {
	s, _ := ctx.Value(denialKey{}).(string)
	return s
}

// callerPerms is what the request's caller may do, worked out once per
// request.
func (s *Server) callerPerms(r *http.Request) domain.PermSet {
	if p, ok := r.Context().Value(permsKey{}).(domain.PermSet); ok {
		return p
	}
	return s.permsFor(r)
}

func (s *Server) permsFor(r *http.Request) domain.PermSet {
	// The desktop: one person, the local owner, who may do everything (the
	// token gate still guards writes, as before).
	if s.auth == nil {
		return domain.AllPermissions()
	}
	if isAuthenticated(r.Context()) {
		p, err := s.store.MemberPermissions(r.Context(), localWorkspaceID, s.currentUser(r))
		if err != nil {
			return domain.PermSet{}
		}
		return p
	}
	// The server's own token is its root credential: whoever holds it can
	// read it from the host.
	if s.validToken(r) {
		return domain.AllPermissions()
	}
	return domain.PermSet{}
}

// can reports whether the request's caller holds a permission.
func (s *Server) can(r *http.Request, perm string) bool {
	return s.callerPerms(r).Has(perm)
}

// authorize is the gate in front of the router (see the top of this file).
func (s *Server) authorize(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		rule := ruleFor(pattern)
		if pattern == "" || rule.public {
			mux.ServeHTTP(w, r)
			return
		}
		if s.auth == nil {
			// The desktop's gate: writes need the token; reads too when the
			// server is reachable beyond this machine.
			if s.TokenValue() == "" || isAuthenticated(r.Context()) || s.validToken(r) ||
				(isReadMethod(r.Method) && !s.readAuth) {
				mux.ServeHTTP(w, r)
				return
			}
			writeMsg(w, http.StatusUnauthorized,
				"this needs the API token — send Authorization: Bearer <token> (Settings → Server access)")
			return
		}
		if !isAuthenticated(r.Context()) && !s.validToken(r) {
			switch {
			case denial(r.Context()) != "":
				writeMsg(w, http.StatusForbidden, denial(r.Context()))
			case s.behindProxy():
				writeMsg(w, http.StatusUnauthorized, "this server signs people in through its proxy: open it through the proxy's address")
			default:
				writeMsg(w, http.StatusUnauthorized, "sign in first")
			}
			return
		}
		perms := s.permsFor(r)
		r = r.WithContext(context.WithValue(r.Context(), permsKey{}, perms))
		if rule.perm != "" && !perms.Has(rule.perm) {
			writeMsg(w, http.StatusForbidden, "your role does not allow this: it needs "+permissionName(rule.perm))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// permissionName is a permission as the admin console names it.
func permissionName(p string) string {
	switch p {
	case domain.PermUseProduct:
		return "“use the product”"
	case domain.PermManageOwnJobs:
		return "“manage their own jobs”"
	case domain.PermManageAllJobs:
		return "“manage everyone's jobs”"
	case domain.PermDesignWorkflows:
		return "“design workflows”"
	case domain.PermManageMembers:
		return "“manage members and roles”"
	case domain.PermAdminister:
		return "“administer the installation”"
	}
	return strings.ReplaceAll(p, "_", " ")
}
