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

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// TestReset_NoOpWhenFileMissing — reset against a non-existent path is a
// silent no-op (the binary reports "nothing to do" but exits zero).
func TestReset_NoOpWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.db")
	removed, err := cmdReset(path)
	if err != nil {
		t.Fatalf("reset on missing file should not error: %v", err)
	}
	if removed {
		t.Errorf("removed=true on missing file; want false")
	}
}

// TestReset_DeletesSingleUserDB — fresh DB with only the implicit 'local'
// workspace seeded by migration 0006 is wiped cleanly.
func TestReset_DeletesSingleUserDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Build a pristine single-user DB (Migrate seeds 'local' workspace + user).
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("DB should exist before reset: %v", err)
	}

	removed, err := cmdReset(path)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !removed {
		t.Errorf("removed=false; want true")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("DB should not exist after reset (err=%v)", err)
	}
}

// TestReset_RefusesMultiWorkspace — a second workspace is the canonical
// multi-user signal. Reset must refuse and leave the file intact.
func TestReset_RefusesMultiWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := store.CreateWorkspace(ctx, &domain.Workspace{
		ID: "acme", Name: "Acme", Plan: "team", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed second workspace: %v", err)
	}
	store.Close()

	removed, err := cmdReset(path)
	if err == nil {
		t.Fatal("expected reset to refuse multi-workspace DB")
	}
	if removed {
		t.Errorf("removed=true on refused reset; want false")
	}
	if !strings.Contains(err.Error(), "workspaces present") {
		t.Errorf("error message should mention workspaces; got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("DB should still exist after refusal: %v", err)
	}
}

// TestReset_RefusesNonLocalWorkspace — even a single workspace, if it
// isn't the implicit 'local', is treated as multi-user and refused.
// Defends against the edge case where someone renamed the seeded workspace.
func TestReset_RefusesNonLocalWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Drop the implicit 'local' workspace and replace it with a non-local one.
	if err := store.DeleteWorkspace(ctx, "local"); err != nil {
		t.Fatalf("delete local: %v", err)
	}
	if err := store.CreateWorkspace(ctx, &domain.Workspace{
		ID: "acme", Name: "Acme", Plan: "team", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed acme: %v", err)
	}
	store.Close()

	removed, err := cmdReset(path)
	if err == nil {
		t.Fatal("expected reset to refuse non-local workspace")
	}
	if removed {
		t.Errorf("removed=true on refused reset; want false")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("DB should still exist after refusal: %v", err)
	}
}
