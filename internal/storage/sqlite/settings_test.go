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
	"encoding/json"
	"errors"
	"testing"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// User settings round-trip: set, get, list, overwrite, delete, missing.

func TestUserSettingRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// 'local' user is auto-seeded by migration 0006. Use it directly.

	// Initial set + get.
	st := &domain.UserSetting{
		UserID: "local", Key: "ui.drawer.open", Value: json.RawMessage(`true`),
	}
	if err := s.SetUserSetting(ctx, st); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.GetUserSetting(ctx, "local", "ui.drawer.open")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Value) != "true" {
		t.Errorf("value = %q, want true", string(got.Value))
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("updated_at not populated")
	}

	// Overwrite (upsert) with structured value.
	st.Value = json.RawMessage(`{"width": 280, "open": false}`)
	if err := s.SetUserSetting(ctx, st); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ = s.GetUserSetting(ctx, "local", "ui.drawer.open")
	if string(got.Value) != `{"width": 280, "open": false}` {
		t.Errorf("after upsert value = %q", string(got.Value))
	}

	// List.
	if err := s.SetUserSetting(ctx, &domain.UserSetting{
		UserID: "local", Key: "agent.model", Value: json.RawMessage(`"qwen2.5:7b"`),
	}); err != nil {
		t.Fatalf("set 2nd: %v", err)
	}
	all, err := s.ListUserSettings(ctx, "local")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("list len = %d, want 2", len(all))
	}
	// ORDER BY key — so agent.model comes before ui.drawer.open
	if all[0].Key != "agent.model" || all[1].Key != "ui.drawer.open" {
		t.Errorf("list order: %+v", all)
	}

	// Delete.
	if err := s.DeleteUserSetting(ctx, "local", "ui.drawer.open"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetUserSetting(ctx, "local", "ui.drawer.open"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("after delete: expected ErrNotFound, got %v", err)
	}

	// Delete missing.
	if err := s.DeleteUserSetting(ctx, "local", "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("delete-missing: expected ErrNotFound, got %v", err)
	}
}

func TestUserSettingMissingKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.GetUserSetting(ctx, "local", "never.set")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound for unset key, got %v", err)
	}
}

func TestUserSettingsCascadeOnUserDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Add a second user just for this test (don't touch 'local').
	if err := s.CreateUser(ctx, &domain.User{
		ID: "alice", DisplayName: "Alice", Provider: "local", CreatedAt: fixedTime(t),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := s.SetUserSetting(ctx, &domain.UserSetting{
		UserID: "alice", Key: "k", Value: json.RawMessage(`1`),
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.DeleteUser(ctx, "alice"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := s.GetUserSetting(ctx, "alice", "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected setting to cascade-delete, got %v", err)
	}
}

// Project settings round-trip + cascade on project delete.

func TestProjectSettingRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s) // p1

	st := &domain.ProjectSetting{
		ProjectID: "p1", Key: "scratchpad.default_mode", Value: json.RawMessage(`"strict"`),
	}
	if err := s.SetProjectSetting(ctx, st); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.GetProjectSetting(ctx, "p1", "scratchpad.default_mode")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Value) != `"strict"` {
		t.Errorf("value = %q", string(got.Value))
	}

	// Upsert.
	st.Value = json.RawMessage(`"full"`)
	_ = s.SetProjectSetting(ctx, st)
	got, _ = s.GetProjectSetting(ctx, "p1", "scratchpad.default_mode")
	if string(got.Value) != `"full"` {
		t.Errorf("after upsert: %q", string(got.Value))
	}

	// List.
	all, _ := s.ListProjectSettings(ctx, "p1")
	if len(all) != 1 {
		t.Errorf("list len = %d, want 1", len(all))
	}
}

func TestProjectSettingsCascadeOnProjectDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s)

	if err := s.SetProjectSetting(ctx, &domain.ProjectSetting{
		ProjectID: "p1", Key: "k", Value: json.RawMessage(`true`),
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.DeleteProject(ctx, "p1"); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := s.GetProjectSetting(ctx, "p1", "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected setting to cascade-delete, got %v", err)
	}
}
