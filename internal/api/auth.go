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
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// authCookieName carries the API token to the first-party SPA. HttpOnly so
// page JS can't read (or leak) it; the browser sends it on same-origin
// write requests automatically.
const authCookieName = "cd_auth"

// requireWriteAuth gates state-changing requests behind the API token.
// Reads (GET/HEAD/OPTIONS) are always open — on a localhost-bound desktop
// that's the user's own machine, and free agent reads are the funnel hook.
// Writes require the token: the first-party UI presents it via the cd_auth
// cookie (set when the app shell is served); programmatic / agent callers
// present `Authorization: Bearer <token>`. No token configured (the field
// is empty) disables the gate — used by `-no-auth` and by tests.
func (s *Server) requireWriteAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A resolved per-user token or session is an authenticated identity →
		// authorized to write (multi-user). Else fall back to the legacy shared
		// token (single-user); reads are always open.
		if s.tokenValue() == "" || isReadMethod(r.Method) || isAuthenticated(r.Context()) || s.validToken(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeMsg(w, http.StatusUnauthorized,
			"write requests require the API token — send Authorization: Bearer <token> (Settings → Server access)")
	})
}

func isReadMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// validToken reports whether the request carries the API token, via the
// bearer header (programmatic) or the cd_auth cookie (first-party SPA).
// Constant-time compare so a wrong token leaks no timing signal.
func (s *Server) validToken(r *http.Request) bool {
	got := bearerToken(r)
	if got == "" {
		if c, err := r.Cookie(authCookieName); err == nil {
			got = c.Value
		}
	}
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.tokenValue())) == 1
}

// getAuthToken reveals the current API token to an AUTHENTICATED caller —
// backs Settings → Server access. Although it's a GET (and reads are
// otherwise open), it self-enforces auth because it returns a secret; the
// first-party SPA passes via its cd_auth cookie.
func (s *Server) getAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.tokenValue() == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	if !s.validToken(r) {
		writeMsg(w, http.StatusUnauthorized, "authenticate to view the API token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "token": s.tokenValue()})
}

// regenerateAuthToken rotates the token: persists the new one, swaps it in
// memory, and re-sets the first-party cookie so the SPA keeps working —
// while any client still holding the old token is kicked. Write-gated by
// the middleware (caller must present the current credential).
func (s *Server) regenerateAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.tokenValue() == "" {
		writeMsg(w, http.StatusBadRequest, "write-auth is disabled (-no-auth) — nothing to regenerate")
		return
	}
	tok, err := GenerateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if s.authPersist != nil {
		if err := s.authPersist(tok); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.setToken(tok)
	SetAuthCookie(w, tok, s.cookieSecure(r))
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}

func bearerToken(r *http.Request) string {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// SetAuthCookie writes the first-party session cookie carrying the token so
// the served SPA can make write requests without ever exposing the token to
// page JS. HttpOnly + SameSite=Strict (no cross-site sends → basic CSRF
// defense). secure sets the Secure attribute: off for plain-http localhost
// (desktop), on for HTTPS/proxied deployments (-secure-cookies).
func SetAuthCookie(w http.ResponseWriter, token string, secure bool) {
	if token == "" {
		return
	}
	// #nosec G124 -- Secure is deployment-conditional (plain-http localhost
	// can't set it); HttpOnly + SameSite=Strict are always set.
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
}

// GenerateToken returns a fresh 256-bit random token, hex-encoded.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
