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

import "testing"

// TestDiscoverComponents: source files roll up to depth-2 components; noise dirs,
// non-code files, and root files are excluded; files-per-component is counted.
func TestDiscoverComponents(t *testing.T) {
	paths := []string{
		"internal/api/server.go", "internal/api/architecture.go",
		"internal/storage/sqlite/store.go", "internal/storage/sqlite/arch.go",
		"internal/domain/model.go",
		"cmd/codedistill/main.go",
		"web/src/app.svelte", "web/src/lib/api.ts",
		"README.md",           // non-code → skip
		"vendor/x/y.go",       // noise dir → skip
		"node_modules/z/a.js", // noise dir → skip
		"main.go",             // root file (1 segment) → skip
		"docs/guide.md",       // non-code → skip
	}
	comps := discoverComponentsFromPaths(paths, 24)

	got := map[string]int{}
	for _, c := range comps {
		got[c.Dir] = len(c.Files)
	}
	for _, want := range []string{"cmd/codedistill", "internal/api", "internal/domain", "internal/storage", "web/src"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing component %q (got %v)", want, got)
		}
	}
	if len(comps) != 5 {
		t.Errorf("component count = %d, want 5 (%v)", len(comps), got)
	}
	if got["internal/storage"] != 2 { // both sqlite files rolled up to depth-2
		t.Errorf("internal/storage files = %d, want 2", got["internal/storage"])
	}
	if got["web/src"] != 2 {
		t.Errorf("web/src files = %d, want 2", got["web/src"])
	}
	for _, bad := range []string{"vendor", "node_modules", "docs", ".", "main.go"} {
		if _, ok := got[bad]; ok {
			t.Errorf("should not include %q", bad)
		}
	}
}

func hasEdge(edges [][2]string, from, to string) bool {
	for _, e := range edges {
		if e[0] == from && e[1] == to {
			return true
		}
	}
	return false
}

// TestComponentEdges_PathBased: path-style imports (Go/TS/Python) resolve to
// edges via the exact directory path, and a component that imports nothing has
// no outgoing edge.
func TestComponentEdges_PathBased(t *testing.T) {
	comps := []component{{Dir: "internal/api"}, {Dir: "internal/storage"}, {Dir: "internal/domain"}}
	bodies := map[string]string{
		"internal/api":     `import "codedistill/internal/storage"` + "\n" + `import "codedistill/internal/domain"`,
		"internal/storage": `import "codedistill/internal/domain"`,
		"internal/domain":  ``,
	}
	edges := componentEdges(comps, func(d string) string { return bodies[d] })

	if !hasEdge(edges, "internal/api", "internal/storage") {
		t.Error("want internal/api -> internal/storage")
	}
	if !hasEdge(edges, "internal/api", "internal/domain") {
		t.Error("want internal/api -> internal/domain")
	}
	if !hasEdge(edges, "internal/storage", "internal/domain") {
		t.Error("want internal/storage -> internal/domain")
	}
	for _, e := range edges {
		if e[0] == "internal/domain" {
			t.Errorf("internal/domain imports nothing — no outgoing edge, got %v", e)
		}
	}
}

// TestComponentEdges_PackageNameFallback: when no dir-path import is found (a
// package-name language like Kotlin), the fallback resolves edges via the last
// path segment appearing as a package token.
func TestComponentEdges_PackageNameFallback(t *testing.T) {
	comps := []component{{Dir: "app/service"}, {Dir: "app/storage"}}
	bodies := map[string]string{
		"app/service": "package app.service\nimport com.acme.storage.Db\n",
		"app/storage": "package app.storage\n",
	}
	edges := componentEdges(comps, func(d string) string { return bodies[d] })

	if !hasEdge(edges, "app/service", "app/storage") {
		t.Errorf("want app/service -> app/storage via package-name fallback, got %v", edges)
	}
	if hasEdge(edges, "app/storage", "app/service") {
		t.Errorf("app/storage does not reference service — no reverse edge, got %v", edges)
	}
}
