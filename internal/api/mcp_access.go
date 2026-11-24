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

	"codedistill/internal/domain"
)

// Who may use /mcp, decided as for the API (authz.go). MCP's tools read and
// write project content, so both need "use the product".
//
//   - The desktop, listening on this machine alone: reads are free (any agent
//     on the machine may pull intent); writes need the server's token.
//   - Reached from another machine without sign-in: both need the token.
//   - A multi-user server: the server's token, or a person's API token, whose
//     role must allow using the product. An agent with someone's token acts as
//     them and may do no more than they may.

// mcpPerms is what an MCP request's caller may do, and whether it presented a
// credential at all.
func (s *Server) mcpPerms(r *http.Request) (domain.PermSet, bool) {
	if s.validToken(r) {
		return domain.AllPermissions(), true
	}
	if uid, ok := s.apiTokenUser(r); ok {
		p, err := s.store.MemberPermissions(r.Context(), localWorkspaceID, uid)
		if err != nil {
			return domain.PermSet{}, true
		}
		return p, true
	}
	return domain.PermSet{}, false
}

// MCPWriteAllowed reports whether an MCP request may call write tools.
func (s *Server) MCPWriteAllowed(r *http.Request) bool {
	if s.auth == nil && s.TokenValue() == "" {
		return true // -no-auth
	}
	p, _ := s.mcpPerms(r)
	return p.Has(domain.PermUseProduct)
}

// GuardMCP refuses an MCP request that may not even read: on a multi-user
// server or one reached from another machine, a caller without a credential
// that allows using the product.
func (s *Server) GuardMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil && !s.readAuth {
			next.ServeHTTP(w, r) // the desktop: reads are free
			return
		}
		if s.auth == nil && s.TokenValue() == "" {
			next.ServeHTTP(w, r)
			return
		}
		p, presented := s.mcpPerms(r)
		if !presented {
			writeMsg(w, http.StatusUnauthorized, "MCP on this server needs a token: Authorization: Bearer <your API token>")
			return
		}
		if !p.Has(domain.PermUseProduct) {
			writeMsg(w, http.StatusForbidden, "your role does not allow this: MCP needs "+permissionName(domain.PermUseProduct))
			return
		}
		next.ServeHTTP(w, r)
	})
}
