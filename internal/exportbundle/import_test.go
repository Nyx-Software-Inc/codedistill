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

package exportbundle

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Round-trip: bundle a project, import it into the same store, verify the
// resulting "(imported)" project has all the right child entities with
// remapped IDs.
func TestRoundTrip(t *testing.T) {
	store, ids := seed(t)
	ctx := context.Background()

	b := &Bundler{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
	}
	data, err := b.Project(ctx, ids.projectID)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}

	// Deterministic ID generator so we can assert on entity shape.
	var counter int
	newID := func() string {
		counter++
		return "n" + itoa(counter)
	}
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	res, err := Import(ctx, store, data, newID, now)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if res.Project.Name != "Apollo (imported)" {
		t.Errorf("project name = %q", res.Project.Name)
	}
	if res.Project.RepoRoot != "" {
		t.Errorf("imported repo_root should be blank, got %q", res.Project.RepoRoot)
	}
	if res.Project.ID == ids.projectID {
		t.Errorf("imported project must get a fresh ID")
	}
	wantCounts := Counts{Scratchpads: 1, Items: 2, Todos: 1, Bugs: 0, KB: 0, Anchors: 1}
	if res.Counts != wantCounts {
		t.Errorf("counts = %+v, want %+v", res.Counts, wantCounts)
	}

	scratchpads, err := store.ListScratchpads(ctx, res.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scratchpads) != 1 {
		t.Fatalf("imported scratchpads = %d", len(scratchpads))
	}
	importedSp := scratchpads[0]
	if importedSp.ID == ids.scratchpadID {
		t.Error("imported scratchpad must have a fresh ID")
	}
	importedItems, err := store.ListScratchpadItems(ctx, importedSp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(importedItems) != 2 {
		t.Fatalf("imported items = %d", len(importedItems))
	}
	gotContent := map[string]bool{}
	for _, item := range importedItems {
		gotContent[item.Content] = true
		if item.ScratchpadID != importedSp.ID {
			t.Errorf("item.ScratchpadID = %q, want imported sp %q",
				item.ScratchpadID, importedSp.ID)
		}
	}
	if !gotContent["fix the login crash"] || !gotContent["research Go 1.23 features"] {
		t.Errorf("imported content lost: %v", gotContent)
	}

	importedTodos, err := store.ListTodoItems(ctx, res.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(importedTodos) != 1 {
		t.Fatalf("imported todos = %d", len(importedTodos))
	}
	importedTodo := importedTodos[0]
	if importedTodo.ProjectID != res.Project.ID {
		t.Errorf("todo.ProjectID = %q", importedTodo.ProjectID)
	}
	if importedTodo.SourceItemID == ids.item1ID {
		t.Errorf("todo.SourceItemID still points at original item")
	}

	importedAnchors, err := store.ListCodeAnchors(ctx, "todo_item", importedTodo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(importedAnchors) != 1 {
		t.Fatalf("imported anchors on todo = %d", len(importedAnchors))
	}
	if importedAnchors[0].Path != "internal/auth/login.go" {
		t.Errorf("anchor.Path = %q (lost on import)", importedAnchors[0].Path)
	}

	// Original project still intact (non-destructive).
	origScratchpads, _ := store.ListScratchpads(ctx, ids.projectID)
	if len(origScratchpads) != 1 || origScratchpads[0].ID != ids.scratchpadID {
		t.Errorf("original project disturbed: %+v", origScratchpads)
	}
}

func TestImportRejectsNewerSchema(t *testing.T) {
	store, _ := seed(t)
	ctx := context.Background()

	b := &Bundler{Store: store, Now: time.Now}
	data, err := b.Project(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	patched := repackWithBumpedSchema(t, data, SchemaVersion+1)

	_, err = Import(ctx, store, patched, func() string { return "x" }, time.Now())
	if err == nil {
		t.Fatal("expected error for newer schema_version")
	}
	var schemaErr *ErrSchemaTooNew
	if !errors.As(err, &schemaErr) {
		t.Errorf("expected ErrSchemaTooNew, got %T: %v", err, err)
	} else if schemaErr.Bundle != SchemaVersion+1 {
		t.Errorf("ErrSchemaTooNew.Bundle = %d, want %d",
			schemaErr.Bundle, SchemaVersion+1)
	}
}

func TestImportRejectsMissingManifest(t *testing.T) {
	store, _ := seed(t)
	ctx := context.Background()

	bogus := writeZip(t, map[string][]byte{})
	_, err := Import(ctx, store, bogus, func() string { return "x" }, time.Now())
	if err == nil || !strings.Contains(err.Error(), "missing manifest.json") {
		t.Errorf("expected missing-manifest error, got %v", err)
	}
}

func TestImportTwiceCreatesDistinctProjects(t *testing.T) {
	store, ids := seed(t)
	ctx := context.Background()
	b := &Bundler{Store: store}
	data, err := b.Project(ctx, ids.projectID)
	if err != nil {
		t.Fatal(err)
	}
	var counter int
	newID := func() string {
		counter++
		return "n" + itoa(counter)
	}
	r1, err := Import(ctx, store, data, newID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Import(ctx, store, data, newID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r1.Project.ID == r2.Project.ID {
		t.Errorf("both imports landed on project ID %q", r1.Project.ID)
	}
}

// --- test helpers ---

// writeZip packs name->bytes into a zip suitable for Import.
func writeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

// repackWithBumpedSchema reads `data`, bumps manifest.schema_version to v,
// and writes a new zip with the same contents. Used to fabricate
// "future-version" bundles without otherwise corrupting the format.
func repackWithBumpedSchema(t *testing.T, data []byte, v int) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = body
	}
	files["manifest.json"] = bumpManifestSchema(t, files["manifest.json"], v)
	return writeZip(t, files)
}

// bumpManifestSchema rewrites the schema_version field in raw JSON.
// Avoids round-tripping through Manifest so we can express versions the
// runtime doesn't know about.
func bumpManifestSchema(t *testing.T, manifest []byte, v int) []byte {
	t.Helper()
	const key = `"schema_version":`
	s := string(manifest)
	idx := strings.Index(s, key)
	if idx < 0 {
		t.Fatal("manifest missing schema_version")
	}
	after := idx + len(key)
	// Walk past optional whitespace, then the digit run; that's the slice
	// to replace.
	i := after
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return []byte(s[:after] + " " + itoa(v) + s[i:])
}

// itoa for tests — avoid pulling strconv just for this.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
