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

package mcpclient_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"codedistill/internal/mcpclient"
)

// End-to-end over a REAL child process: CodeDistill's own `mcp` subcommand is a
// stdio MCP server, so it makes an honest destination to talk to. This proves
// spawn, the initialize handshake, tools/list and Close against a live process
// rather than asserting on struct fields.
func TestStdio_AgainstARealServer(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "codedistill")
	build := exec.Command("go", "build", "-o", bin, "./cmd/codedistill")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("could not build a server to talk to: %v\n%s", err, out)
	}

	db := filepath.Join(t.TempDir(), "stdio.db")
	if out, err := exec.Command(bin, "-db", db, "init").CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{
		Transport: mcpclient.TransportStdio,
		Command:   bin,
		Args:      []string{"-db", db, "mcp"},
	})
	if err != nil {
		t.Fatalf("spawn stdio server: %v", err)
	}
	defer cli.Close()

	tools, err := cli.ListTools(ctx)
	if err != nil {
		t.Fatalf("tools/list over stdio: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("no tools returned — the handshake completed but the server said nothing")
	}

	// The read tools are free-tier, so they must be present in any build.
	want := "list_projects"
	found := false
	for _, tl := range tools {
		if tl.Name == want {
			found = true
		}
	}
	if !found {
		names := make([]string, 0, len(tools))
		for _, tl := range tools {
			names = append(names, tl.Name)
		}
		t.Errorf("no %q among %d tools: %v", want, len(tools), names)
	}
}
