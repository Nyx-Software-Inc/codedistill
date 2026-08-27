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

package sqlite

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Compile-time assertion that *Store satisfies the storage.Storage interface.
var _ storage.Storage = (*Store)(nil)

// newTestStore returns a migrated, isolated Store. By default it's in-memory
// SQLite. When $CODEDISTILL_TEST_PG is set (a postgres:// DSN), it instead runs
// the SAME tests against Postgres in a per-test schema — this is the conformance
// harness that proves both backends behave identically (Phase 4 of the Postgres
// backend; docs/design/postgres-backend.md).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	if dsn := os.Getenv("CODEDISTILL_TEST_PG"); dsn != "" {
		return newTestStorePG(t, dsn)
	}
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// newTestStorePG (the Postgres conformance path) lives in storage_pg_test.go
// behind //go:build !oss, since it pulls in the paid pgx driver. The CE build
// supplies a skip-stub in storage_pg_oss_test.go.

// fixedTime returns a time with sub-second precision stripped so round-trip
// equality isn't broken by sqlite's timestamp format.
func fixedTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
}

// seedProject creates and returns a project for tests that need a parent.
func seedProject(t *testing.T, s *Store) *domain.Project {
	t.Helper()
	p := &domain.Project{ID: "p1", Name: "Default", CreatedAt: fixedTime(t)}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return p
}

// seedScratchpad creates and returns a scratchpad under a seeded project.
func seedScratchpad(t *testing.T, s *Store) *domain.Scratchpad {
	t.Helper()
	seedProject(t, s)
	sp := &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: fixedTime(t),
	}
	if err := s.CreateScratchpad(context.Background(), sp); err != nil {
		t.Fatalf("seed scratchpad: %v", err)
	}
	return sp
}

// seedScratchpadItem creates and returns an item under a seeded scratchpad.
func seedScratchpadItem(t *testing.T, s *Store) *domain.ScratchpadItem {
	t.Helper()
	seedScratchpad(t, s)
	item := &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "hello",
		ClassificationState: "unprocessed",
		CreatedAt:           fixedTime(t),
		UpdatedAt:           fixedTime(t),
	}
	if err := s.CreateScratchpadItem(context.Background(), item); err != nil {
		t.Fatalf("seed scratchpad item: %v", err)
	}
	return item
}

// --- Project round-trip ---

func TestProjectRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := &domain.Project{ID: "p1", Name: "My Project", CreatedAt: fixedTime(t)}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, p)
	}

	list, err := s.ListProjects(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "p1" {
		t.Fatalf("list: err=%v list=%+v", err, list)
	}

	if err := s.DeleteProject(ctx, "p1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetProject(ctx, "p1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	if err := s.DeleteProject(ctx, "p1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}

// --- Scratchpad round-trip ---

func TestScratchpadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s)

	sp := &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: fixedTime(t),
	}
	if err := s.CreateScratchpad(ctx, sp); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetScratchpad(ctx, "sp1")
	if err != nil || !reflect.DeepEqual(got, sp) {
		t.Fatalf("round-trip: got %+v, err %v", got, err)
	}

	sp.Name = "renamed"
	sp.ClassificationMode = "strict"
	if err := s.UpdateScratchpad(ctx, sp); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetScratchpad(ctx, "sp1")
	if got.Name != "renamed" || got.ClassificationMode != "strict" {
		t.Errorf("update not applied: %+v", got)
	}

	list, err := s.ListScratchpads(ctx, "p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := s.DeleteScratchpad(ctx, "sp1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetScratchpad(ctx, "sp1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- ScratchpadItem round-trip (with all optional fields) ---

func TestScratchpadItemRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpad(t, s)

	// Unprocessed item: nullable fields empty.
	// Tags explicitly set to []string{} — storage always round-trips as empty slice (not nil)
	// so JSON responses stay stable as "[]" instead of "null".
	i := &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1",
		ContentType:         "text",
		Content:             "clicking login throws 500",
		ClassificationState: "unprocessed",
		Tags:                []string{},
		CreatedAt:           fixedTime(t),
		UpdatedAt:           fixedTime(t),
	}
	if err := s.CreateScratchpadItem(ctx, i); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetScratchpadItem(ctx, "si1")
	if err != nil || !reflect.DeepEqual(got, i) {
		t.Fatalf("unprocessed round-trip: got %+v err %v", got, err)
	}

	// Classified item: all fields populated.
	i.ClassificationState = "classified"
	i.ProposedCategory = "bug"
	i.ClassificationConfidence = 0.92
	i.ClassificationReasoning = "stack trace + keyword 'crash'"
	i.DerivedItemID = "b1"
	i.UpdatedAt = fixedTime(t).Add(time.Minute)
	if err := s.UpdateScratchpadItem(ctx, i); err != nil {
		t.Fatalf("update classified: %v", err)
	}
	got, _ = s.GetScratchpadItem(ctx, "si1")
	if !reflect.DeepEqual(got, i) {
		t.Errorf("classified round-trip:\n got:  %+v\n want: %+v", got, i)
	}

	// Skipped via override: CHECK constraints exercised.
	i.ClassificationState = "skipped"
	i.SkippedReason = "override_skip"
	i.ClassificationOverride = "skip"
	if err := s.UpdateScratchpadItem(ctx, i); err != nil {
		t.Fatalf("update skipped: %v", err)
	}
	got, _ = s.GetScratchpadItem(ctx, "si1")
	if got.SkippedReason != "override_skip" || got.ClassificationOverride != "skip" {
		t.Errorf("skipped round-trip: got %+v", got)
	}

	// Stage 4: Name field round-trips. Empty default + UPDATE persists.
	if got.Name != "" {
		t.Errorf("expected empty default name, got %q", got.Name)
	}
	i.Name = "login crash investigation"
	if err := s.UpdateScratchpadItem(ctx, i); err != nil {
		t.Fatalf("update name: %v", err)
	}
	got, _ = s.GetScratchpadItem(ctx, "si1")
	if got.Name != "login crash investigation" {
		t.Errorf("name round-trip: got %q", got.Name)
	}

	list, err := s.ListScratchpadItems(ctx, "sp1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := s.DeleteScratchpadItem(ctx, "si1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetScratchpadItem(ctx, "si1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// TestScratchpadItemBlobFieldsRoundTrip covers the migration 0022
// columns: blob_sha, mime_type, file_name, byte_size, width, height.
// All NULL by default for text items; populated for image items.
func TestScratchpadItemBlobFieldsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpad(t, s)

	img := &domain.ScratchpadItem{
		ID: "siBlob", ScratchpadID: "sp1",
		ContentType: "image", Content: "",
		ClassificationState: "unprocessed",
		Tags:                []string{},
		BlobSHA:             "abc123def456",
		MimeType:            "image/png",
		FileName:            "screenshot.png",
		ByteSize:            204800,
		Width:               1280,
		Height:              720,
		CreatedAt:           fixedTime(t),
		UpdatedAt:           fixedTime(t),
	}
	if err := s.CreateScratchpadItem(ctx, img); err != nil {
		t.Fatalf("create image: %v", err)
	}
	got, err := s.GetScratchpadItem(ctx, "siBlob")
	if err != nil || !reflect.DeepEqual(got, img) {
		t.Fatalf("image round-trip: got %+v err %v", got, err)
	}
}

// TestListLiveBlobShas verifies the GC sweeper's "what's live" query
// returns only distinct, non-NULL blob_sha values from any
// scratchpad_item.
func TestListLiveBlobShas(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpad(t, s)

	mk := func(id, sha string) {
		t.Helper()
		i := &domain.ScratchpadItem{
			ID: id, ScratchpadID: "sp1",
			ContentType: "image", Content: "",
			ClassificationState: "unprocessed",
			Tags:                []string{},
			BlobSHA:             sha, MimeType: "image/png",
			ByteSize: 1, Width: 1, Height: 1,
			CreatedAt: fixedTime(t), UpdatedAt: fixedTime(t),
		}
		if err := s.CreateScratchpadItem(ctx, i); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// Empty store first.
	live, err := s.ListLiveBlobShas(ctx)
	if err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("empty list: got %v, want []", live)
	}

	// Two items share sha "aaa", one has sha "bbb", one has no blob.
	mk("siA", "aaa")
	mk("siB", "aaa")
	mk("siC", "bbb")
	if err := s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "siText", ScratchpadID: "sp1",
		ContentType: "text", Content: "plain text",
		ClassificationState: "unprocessed",
		Tags:                []string{},
		CreatedAt:           fixedTime(t), UpdatedAt: fixedTime(t),
	}); err != nil {
		t.Fatalf("seed text: %v", err)
	}
	live, err = s.ListLiveBlobShas(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := map[string]bool{}
	for _, sha := range live {
		seen[sha] = true
	}
	if !seen["aaa"] || !seen["bbb"] || len(seen) != 2 {
		t.Errorf("ListLiveBlobShas = %v, want exactly {aaa, bbb}", live)
	}
}

// --- Todo round-trip ---

func TestTodoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	todo := &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1",
		Subject:   "update Go to 1.23",
		Priority:  "high",
		Status:    "incomplete",
		Origin:    "agent-derived",
		CreatedAt: fixedTime(t),
	}
	if err := s.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetTodoItem(ctx, "t1")
	if err != nil || !reflect.DeepEqual(got, todo) {
		t.Fatalf("round-trip: got %+v err %v", got, err)
	}

	// Mark complete.
	done := fixedTime(t).Add(time.Hour)
	todo.Status = "complete"
	todo.CompletedAt = &done
	if err := s.UpdateTodoItem(ctx, todo); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetTodoItem(ctx, "t1")
	if got.Status != "complete" || got.CompletedAt == nil || !got.CompletedAt.Equal(done) {
		t.Errorf("completion round-trip: got %+v", got)
	}

	list, err := s.ListTodoItems(ctx, "p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := s.DeleteTodoItem(ctx, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetTodoItem(ctx, "t1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- Bug round-trip ---

func TestBugRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	bug := &domain.BugItem{
		ID: "b1", ProjectID: "p1", SourceItemID: "si1",
		Subject:           "login 500 after password change",
		Severity:          "major",
		Status:            "open",
		StepsToReproduce:  "change password, then click login",
		ExpectedBehavior:  "login succeeds",
		ActualBehavior:    "server returns 500",
		Environment:       "prod, Chrome 120",
		AffectedComponent: "auth-service",
		Origin:            "agent-derived",
		CreatedAt:         fixedTime(t),
	}
	if err := s.CreateBugItem(ctx, bug); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetBugItem(ctx, "b1")
	if err != nil || !reflect.DeepEqual(got, bug) {
		t.Fatalf("round-trip:\n got: %+v\n err: %v", got, err)
	}

	// Transition through workflow states.
	for _, next := range []string{"investigating", "in-progress", "fixed", "verified", "closed"} {
		bug.Status = next
		if err := s.UpdateBugItem(ctx, bug); err != nil {
			t.Fatalf("update to %q: %v", next, err)
		}
		got, _ := s.GetBugItem(ctx, "b1")
		if got.Status != next {
			t.Errorf("status not updated: got %q want %q", got.Status, next)
		}
	}

	list, err := s.ListBugItems(ctx, "p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := s.DeleteBugItem(ctx, "b1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetBugItem(ctx, "b1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- KnowledgeEntry round-trip ---

func TestKnowledgeEntryRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	kb := &domain.KnowledgeEntry{
		ID: "k1", ProjectID: "p1", SourceItemID: "si1",
		Title:     "gofmt emits tabs",
		Content:   "gofmt always emits tabs for indentation regardless of source",
		CreatedAt: fixedTime(t),
	}
	if err := s.CreateKnowledgeEntry(ctx, kb); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetKnowledgeEntry(ctx, "k1")
	if err != nil || !reflect.DeepEqual(got, kb) {
		t.Fatalf("round-trip:\n got: %+v\n err: %v", got, err)
	}

	kb.Title = "gofmt formatting"
	if err := s.UpdateKnowledgeEntry(ctx, kb); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetKnowledgeEntry(ctx, "k1")
	if got.Title != "gofmt formatting" {
		t.Errorf("update not applied: %+v", got)
	}

	list, err := s.ListKnowledgeEntries(ctx, "p1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := s.DeleteKnowledgeEntry(ctx, "k1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetKnowledgeEntry(ctx, "k1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- Cascade behavior ---

func TestProjectCascadeDeletesScratchpad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpad(t, s) // creates p1 + sp1

	if err := s.DeleteProject(ctx, "p1"); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := s.GetScratchpad(ctx, "sp1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected scratchpad to cascade-delete, got err=%v", err)
	}
}

func TestScratchpadCascadeDeletesItems(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	if err := s.DeleteScratchpad(ctx, "sp1"); err != nil {
		t.Fatalf("delete scratchpad: %v", err)
	}
	if _, err := s.GetScratchpadItem(ctx, "si1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected scratchpad item to cascade-delete, got err=%v", err)
	}
}

func TestDeletingScratchpadItemSetsSourceNull(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s) // p1 + sp1 + si1

	todo := &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "derived", Priority: "none", Status: "incomplete",
		Origin: "agent-derived", CreatedAt: fixedTime(t),
	}
	if err := s.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("create todo: %v", err)
	}

	if err := s.DeleteScratchpadItem(ctx, "si1"); err != nil {
		t.Fatalf("delete scratchpad item: %v", err)
	}

	got, err := s.GetTodoItem(ctx, "t1")
	if err != nil {
		t.Fatalf("get todo after source delete: %v", err)
	}
	if got.SourceItemID != "" {
		t.Errorf("expected SourceItemID to be nulled, got %q", got.SourceItemID)
	}
}
