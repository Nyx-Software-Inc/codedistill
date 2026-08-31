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

package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type ctxKey int

const writeAuthKey ctxKey = 1

// mcpAuthCookie mirrors the API's first-party cookie name.
const mcpAuthCookie = "cd_auth"

// AuthContextFunc returns an mcp-go HTTPContextFunc that records, per HTTP
// request, whether the caller may invoke WRITE tools. READS are never gated —
// "MCP read free, MCP write paid." The stdio transport never invokes this,
// so stdio sessions carry no marker and are treated as trusted-local
// (writeAuthorized → true), matching the first-party UI: local channels are
// trusted; the network needs the token.
//
// token is a GETTER, resolved on every request, and that is the whole point:
// it used to be a captured string, which meant an admin rotating the token
// through Settings → Server access left this closure holding the OLD value.
// Measured on a live server: after a rotation the revoked token still
// authorized every MCP write tool while the new token was refused, until the
// process restarted. A nil getter, or one returning "", disables the gate.
func AuthContextFunc(token func() string) func(context.Context, *http.Request) context.Context {
	return func(ctx context.Context, r *http.Request) context.Context {
		want := ""
		if token != nil {
			want = token()
		}
		ok := want == "" ||
			subtle.ConstantTimeCompare([]byte(requestToken(r)), []byte(want)) == 1
		return context.WithValue(ctx, writeAuthKey, ok)
	}
}

func requestToken(r *http.Request) string {
	const p = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	if c, err := r.Cookie(mcpAuthCookie); err == nil {
		return c.Value
	}
	return ""
}

// writeAuthorized reports whether the current MCP request may call write
// tools. No marker (stdio / no HTTP auth layer) = trusted local = allowed.
func writeAuthorized(ctx context.Context) bool {
	v := ctx.Value(writeAuthKey)
	if v == nil {
		return true
	}
	b, _ := v.(bool)
	return b
}

// requireWrite returns an error tool-result when the caller isn't authorized
// for writes, else nil. Write-tool handlers call it first; read tools don't.
func requireWrite(ctx context.Context) *mcp.CallToolResult {
	if writeAuthorized(ctx) {
		return nil
	}
	return mcp.NewToolResultError(
		"this write requires the API token — set Authorization: Bearer <token> in your MCP client (CodeDistill → Settings → Server access). Reads are free.")
}
