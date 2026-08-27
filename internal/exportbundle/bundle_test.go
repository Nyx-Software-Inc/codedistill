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
	"encoding/json"
	"io"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// seed populates a small project with one scratchpad, two items, one derived
// todo (linked to item #1), and one anchor on the todo. Returns IDs that
// tests can assert against.
type seedIDs struct {
	projectID, scratchpadID string
	item1ID, item2ID        string
	todoID                  string
	anchorID                string
}

func seed(t *testing.T) (*sqlite.Store, seedIDs) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

	ids := seedIDs{
		projectID:    "p1",
		scratchpadID: "s1",
		item1ID:      "i1",
		item2ID:      "i2",
		todoID:       "t1",
		anchorID:     "a1",
	}
	if err := store.CreateProject(ctx, &domain.Project{
		ID: ids.projectID, Name: "Apollo", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: ids.scratchpadID, ProjectID: ids.projectID, Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: ids.item1ID, ScratchpadID: ids.scratchpadID,
		ContentType: "text", Content: "fix the login crash",
		ClassificationState: "classified", ProposedCategory: "todo",
		Tags: []string{"urgent"}, GridW: 12, GridH: 4,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: ids.item2ID, ScratchpadID: ids.scratchpadID,
		ContentType: "text", Content: "research Go 1.23 features",
		ClassificationState: "unprocessed", GridW: 12, GridH: 4,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: ids.todoID, ProjectID: ids.projectID, SourceItemID: ids.item1ID,
		Subject: "fix the login crash", Priority: "high", Status: "incomplete",
		Origin: "agent-derived", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCodeAnchor(ctx, &domain.CodeAnchor{
		ID: ids.anchorID, OwnerType: "todo_item", OwnerID: ids.todoID,
		Kind: "file", Path: "internal/auth/login.go", LineStart: 42,
		Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return store, ids
}

// readZipNames + readZipFile are small helpers to assert on bundle layout.
func readZipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func readZipFile(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer rc.Close()
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return body
	}
	t.Fatalf("not found in zip: %s", name)
	return nil
}

func TestBundleProject(t *testing.T) {
	store, ids := seed(t)
	b := &Bundler{
		Store:           store,
		ExporterVersion: "0.6.0-test",
		ExporterSHA:     "deadbeef",
		Now:             func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
	}
	data, err := b.Project(context.Background(), ids.projectID)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	names := readZipNames(t, data)

	want := []string{
		"manifest.json",
		"project.json",
		"scratchpads/" + ids.scratchpadID + ".json",
		"scratchpads/" + ids.scratchpadID + "/items/" + ids.item1ID + ".md",
		"scratchpads/" + ids.scratchpadID + "/items/" + ids.item1ID + ".json",
		"scratchpads/" + ids.scratchpadID + "/items/" + ids.item2ID + ".md",
		"scratchpads/" + ids.scratchpadID + "/items/" + ids.item2ID + ".json",
		"derived/todos/" + ids.todoID + ".json",
		"code-anchors.json",
	}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing entry %q (have %v)", w, names)
		}
	}

	// Manifest correctness.
	var m Manifest
	if err := json.Unmarshal(readZipFile(t, data, "manifest.json"), &m); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if m.SchemaVersion != SchemaVersion || m.BundleKind != "project" {
		t.Errorf("manifest = %+v", m)
	}
	if m.ProjectName != "Apollo" || m.ProjectID != ids.projectID {
		t.Errorf("manifest project = %s/%s", m.ProjectID, m.ProjectName)
	}
	if m.ExporterVersion != "0.6.0-test" || m.ExporterSHA != "deadbeef" {
		t.Errorf("manifest exporter = %s/%s", m.ExporterVersion, m.ExporterSHA)
	}
	wantCounts := Counts{Scratchpads: 1, Items: 2, Todos: 1, Bugs: 0, KB: 0, Anchors: 1}
	if m.Counts != wantCounts {
		t.Errorf("counts = %+v, want %+v", m.Counts, wantCounts)
	}

	// Item .md holds raw content; sidecar .json blanks Content.
	mdBody := string(readZipFile(t, data, "scratchpads/"+ids.scratchpadID+"/items/"+ids.item1ID+".md"))
	if mdBody != "fix the login crash" {
		t.Errorf("item.md = %q", mdBody)
	}
	var sidecar domain.ScratchpadItem
	if err := json.Unmarshal(
		readZipFile(t, data, "scratchpads/"+ids.scratchpadID+"/items/"+ids.item1ID+".json"),
		&sidecar,
	); err != nil {
		t.Fatalf("sidecar unmarshal: %v", err)
	}
	if sidecar.Content != "" {
		t.Errorf("sidecar.Content should be empty (lives in .md), got %q", sidecar.Content)
	}
	if sidecar.ID != ids.item1ID || sidecar.ProposedCategory != "todo" {
		t.Errorf("sidecar = %+v", sidecar)
	}
	if len(sidecar.Tags) != 1 || sidecar.Tags[0] != "urgent" {
		t.Errorf("sidecar tags = %v", sidecar.Tags)
	}

	// Anchors round-trip.
	var anchors []*domain.CodeAnchor
	if err := json.Unmarshal(readZipFile(t, data, "code-anchors.json"), &anchors); err != nil {
		t.Fatalf("anchors unmarshal: %v", err)
	}
	if len(anchors) != 1 || anchors[0].ID != ids.anchorID {
		t.Errorf("anchors = %+v", anchors)
	}
}

func TestBundleScratchpad(t *testing.T) {
	store, ids := seed(t)
	b := &Bundler{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
	}
	data, err := b.Scratchpad(context.Background(), ids.scratchpadID)
	if err != nil {
		t.Fatalf("Scratchpad: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(readZipFile(t, data, "manifest.json"), &m); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if m.BundleKind != "scratchpad" {
		t.Errorf("BundleKind = %q, want scratchpad", m.BundleKind)
	}
	if m.ScratchpadID != ids.scratchpadID || m.ScratchpadName != "main" {
		t.Errorf("manifest scratchpad = %s/%s", m.ScratchpadID, m.ScratchpadName)
	}
	if m.Counts.Scratchpads != 1 || m.Counts.Items != 2 || m.Counts.Todos != 1 {
		t.Errorf("counts = %+v", m.Counts)
	}
	// Project record is included for naming purposes on import.
	var p domain.Project
	if err := json.Unmarshal(readZipFile(t, data, "project.json"), &p); err != nil {
		t.Fatalf("project unmarshal: %v", err)
	}
	if p.Name != "Apollo" {
		t.Errorf("project name = %q", p.Name)
	}
}

func TestBundleEmptyProject(t *testing.T) {
	store, _ := seed(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{
		ID: "p2", Name: "Empty", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	b := &Bundler{Store: store}
	data, err := b.Project(ctx, "p2")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// Empty project still produces manifest, project.json, code-anchors.json
	// (with []) and nothing else.
	names := readZipNames(t, data)
	if len(names) != 3 {
		t.Errorf("expected 3 entries for empty project, got %v", names)
	}
}
