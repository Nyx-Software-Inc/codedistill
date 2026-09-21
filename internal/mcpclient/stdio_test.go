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

package mcpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// roundTrip marshals and unmarshals an Endpoint the way user_settings does.
func roundTrip(t *testing.T, e Endpoint) Endpoint {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var out Endpoint
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Endpoints persisted before stdio existed have no `transport` field. They must
// keep meaning HTTP, or every configured destination breaks on upgrade.
func TestEndpoint_EmptyTransportIsHTTP(t *testing.T) {
	if (Endpoint{URL: "https://example.invalid"}).IsStdio() {
		t.Error("an endpoint with no transport must be treated as HTTP")
	}
	if !(Endpoint{Transport: TransportStdio, Command: "x"}).IsStdio() {
		t.Error("TransportStdio must report IsStdio")
	}
}

// Each transport requires a different field, and the error must say which.
func TestNew_RequiredFieldPerTransport(t *testing.T) {
	ctx := context.Background()

	if _, err := New(ctx, Endpoint{}); err == nil {
		t.Error("http endpoint with no URL should fail")
	} else if !strings.Contains(err.Error(), "URL required") {
		t.Errorf("error = %v, want it to name the missing URL", err)
	}

	if _, err := New(ctx, Endpoint{Transport: TransportStdio}); err == nil {
		t.Error("stdio endpoint with no command should fail")
	} else if !strings.Contains(err.Error(), "command required") {
		t.Errorf("error = %v, want it to name the missing command", err)
	}
}

// A command that cannot be started must fail with the command named, not with a
// generic transport error — a typo'd binary is the likely mistake.
func TestNew_StdioReportsUnstartableCommand(t *testing.T) {
	_, err := New(context.Background(), Endpoint{
		Transport: TransportStdio,
		Command:   "/nonexistent/definitely-not-a-real-mcp-server",
	})
	if err == nil {
		t.Fatal("expected an error for a command that cannot start")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-mcp-server") {
		t.Errorf("error = %v, want it to name the command", err)
	}
}

// Round-trips through the settings JSON, which is how these are stored.
func TestEndpoint_StdioJSONRoundTrip(t *testing.T) {
	in := Endpoint{
		Transport: TransportStdio,
		Command:   "/usr/local/bin/linear-mcp",
		Args:      []string{"--stdio", "--workspace", "acme"},
		Env:       []string{"LINEAR_TOKEN=secret"},
	}
	out := roundTrip(t, in)
	if !out.IsStdio() || out.Command != in.Command ||
		len(out.Args) != 3 || len(out.Env) != 1 {
		t.Errorf("round trip lost fields: %+v", out)
	}
}
