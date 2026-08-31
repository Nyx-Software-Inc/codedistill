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

//go:build oss

package api

import (
	"context"
	"errors"
	"net/http"
)

// Community Edition stubs for the multi-user surface (OIDC sign-in, sessions,
// membership/admin, per-user tokens). The real implementations
// (oidc/session/membership/admin_members/tokens.go) are paid and stripped from
// the CE; these keep the api package + cmd/codedistill compiling under
// -tags oss. The CE is single-user: identity is always the local user (see
// currentuser.go), and the shared api token from auth.go is the only auth.
//
// Signatures must mirror the stripped files exactly.

// oidcAuth is an empty placeholder so the Server.auth field type resolves. It
// is never constructed in the CE (NewOIDCAuth always errors), so s.auth stays
// nil and /me reports multi_user:false.
type oidcAuth struct{}

// NewOIDCAuth always fails in the CE — multi-user is a paid feature. main.go
// treats the error as "auth disabled, stay single-user".
func NewOIDCAuth(_ context.Context, _, _, _, _ string) (*oidcAuth, error) {
	return nil, errors.New("multi-user/OIDC sign-in is a paid feature")
}

// WithAuth / WithMembershipPolicy are no-ops in the CE (never reached, since
// NewOIDCAuth errors first) but must exist for main.go to compile.
func (s *Server) WithAuth(_ *oidcAuth) *Server                      { return s }
func (s *Server) WithMembershipPolicy(_ string, _ []string) *Server { return s }

// resolveIdentity is a pass-through in the CE: no OIDC session or per-user
// token to resolve, so currentUser falls back to the local user.
func (s *Server) resolveIdentity(next http.Handler) http.Handler { return next }

// Routed handlers — all report the feature unavailable. The login routes
// mirror the real "not configured" 503; the rest are 403 paid-feature.
func (s *Server) authLogin(w http.ResponseWriter, _ *http.Request)    { paidAuth(w) }
func (s *Server) authCallback(w http.ResponseWriter, _ *http.Request) { paidAuth(w) }
func (s *Server) authLogout(w http.ResponseWriter, _ *http.Request)   { paidAuth(w) }

func (s *Server) listMembers(w http.ResponseWriter, _ *http.Request)      { paidMU(w) }
func (s *Server) deactivateMember(w http.ResponseWriter, _ *http.Request) { paidMU(w) }
func (s *Server) reactivateMember(w http.ResponseWriter, _ *http.Request) { paidMU(w) }
func (s *Server) setMemberRole(w http.ResponseWriter, _ *http.Request)    { paidMU(w) }

func (s *Server) createAPIToken(w http.ResponseWriter, _ *http.Request) { paidMU(w) }
func (s *Server) listAPITokens(w http.ResponseWriter, _ *http.Request)  { paidMU(w) }
func (s *Server) deleteAPIToken(w http.ResponseWriter, _ *http.Request) { paidMU(w) }

func paidAuth(w http.ResponseWriter) {
	writeMsg(w, http.StatusServiceUnavailable, "sign-in is not configured (multi-user is a paid feature)")
}
func paidMU(w http.ResponseWriter) {
	writeMsg(w, http.StatusForbidden, "multi-user is a paid feature")
}
