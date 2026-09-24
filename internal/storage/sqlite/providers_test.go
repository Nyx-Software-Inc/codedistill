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
	"strings"
	"testing"

	"codedistill/internal/domain"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func seedProjectNamed(t *testing.T, s *Store, name string) *domain.Project {
	t.Helper()
	p := &domain.Project{ID: name, Name: name, CreatedAt: fixedTime(t)}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p
}

func seedProvider(t *testing.T, s *Store, id, name string, local bool) *domain.ModelProvider {
	t.Helper()
	now := fixedTime(t)
	p := &domain.ModelProvider{
		ID: id, Name: name, Protocol: "ollama",
		Endpoint: "http://localhost:11434", Model: "qwen2.5:7b",
		ContextTokens: 16384, IsLocal: local, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateModelProvider(context.Background(), p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return p
}

// Project binding wins over the global default, and an unbound role resolves to
// nothing — which is what lets an existing install keep working with nothing
// configured at all.
func TestWorkerModelResolutionCascades(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	proj := seedProject(t, s)
	local := seedProvider(t, s, "p-local", "local ollama", true)
	big := seedProvider(t, s, "p-big", "workstation 70b", false)
	now := fixedTime(t)

	if err := s.SetWorkerModel(ctx, domain.WorkerDecomposer, "", local.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveWorkerModel(ctx, domain.WorkerDecomposer, proj.ID)
	if err != nil || got == nil || got.ID != local.ID {
		t.Fatalf("global default did not apply to a project: %+v, %v", got, err)
	}

	// A project override beats it.
	if err := s.SetWorkerModel(ctx, domain.WorkerDecomposer, proj.ID, big.ID, now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ResolveWorkerModel(ctx, domain.WorkerDecomposer, proj.ID)
	if got == nil || got.ID != big.ID {
		t.Fatalf("project binding lost to the global default: %+v", got)
	}
	// ...and only for that project.
	other := seedProjectNamed(t, s, "other")
	got, _ = s.ResolveWorkerModel(ctx, domain.WorkerDecomposer, other.ID)
	if got == nil || got.ID != local.ID {
		t.Fatalf("a project override leaked to another project: %+v", got)
	}

	// Clearing falls back rather than erroring.
	if err := s.ClearWorkerModel(ctx, domain.WorkerDecomposer, proj.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ResolveWorkerModel(ctx, domain.WorkerDecomposer, proj.ID)
	if got == nil || got.ID != local.ID {
		t.Fatalf("cleared binding did not fall back: %+v", got)
	}

	// An unbound role resolves to nothing, not an error.
	got, err = s.ResolveWorkerModel(ctx, domain.WorkerChallenger, proj.ID)
	if err != nil || got != nil {
		t.Fatalf("unbound role = %+v, %v; want nil, nil", got, err)
	}
}

// Rebinding replaces rather than accumulating, or a role would resolve
// arbitrarily between two rows.
func TestSetWorkerModelIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedProvider(t, s, "p-a", "a", true)
	b := seedProvider(t, s, "p-b", "b", true)
	now := fixedTime(t)

	for _, id := range []string{a.ID, b.ID, a.ID} {
		if err := s.SetWorkerModel(ctx, domain.WorkerClassifier, "", id, now); err != nil {
			t.Fatal(err)
		}
	}
	workers, err := s.ListWorkerModels(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if workers[domain.WorkerClassifier] != a.ID {
		t.Fatalf("role = %q, want the last binding %q", workers[domain.WorkerClassifier], a.ID)
	}
	got, _ := s.ResolveWorkerModel(ctx, domain.WorkerClassifier, "")
	if got == nil || got.ID != a.ID {
		t.Fatalf("resolved to %+v", got)
	}
}

// The key must never appear in a value destined for the wire.
func TestAPIKeyNeverSerialises(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	p := &domain.ModelProvider{
		ID: "p1", Name: "cloud", Protocol: "openai",
		Endpoint: "https://api.example.com/v1", Model: "big",
		APIKey: "cdenc1:AAAAsomeciphertext", ContextTokens: 200000,
		CreatedAt: now, UpdatedAt: now, Enabled: true,
	}
	if err := s.CreateModelProvider(ctx, p); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListModelProviders(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d, %v", len(list), err)
	}
	if !list[0].HasAPIKey {
		t.Error("HasAPIKey false for a provider that has one")
	}
	// The struct tag is `json:"-"`, so the key cannot ride out in a response.
	blob := mustJSON(t, list[0])
	if containsAny(blob, "someciphertext", "cdenc1") {
		t.Fatalf("the stored key appears in the serialised form: %s", blob)
	}

	// The code that BUILDS a client still needs it.
	full, err := s.GetModelProvider(ctx, "p1")
	if err != nil || full.APIKey == "" {
		t.Fatalf("GetModelProvider dropped the key: %+v, %v", full, err)
	}
}

// The UI is never shown a key, so it sends back an empty one. Treating that as
// "clear it" would wipe the credential on every unrelated edit.
func TestEmptyKeyOnUpdateLeavesTheStoredOne(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	p := &domain.ModelProvider{
		ID: "p1", Name: "cloud", Protocol: "openai", Endpoint: "https://x/v1",
		Model: "big", APIKey: "cdenc1:secret", ContextTokens: 1000,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateModelProvider(ctx, p); err != nil {
		t.Fatal(err)
	}

	p.Name = "cloud renamed"
	p.APIKey = "" // what the UI sends back
	if err := s.UpdateModelProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetModelProvider(ctx, "p1")
	if got.APIKey != "cdenc1:secret" {
		t.Fatalf("an unrelated edit destroyed the key: %q", got.APIKey)
	}
	if got.Name != "cloud renamed" {
		t.Errorf("the edit did not apply: %q", got.Name)
	}

	// Clearing is explicit.
	if err := s.ClearProviderAPIKey(ctx, "p1", now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetModelProvider(ctx, "p1")
	if got.APIKey != "" {
		t.Errorf("explicit clear left %q", got.APIKey)
	}
}

func TestCreateRejectsNonsense(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	for name, p := range map[string]*domain.ModelProvider{
		"unknown protocol": {ID: "a", Name: "x", Protocol: "telepathy", Endpoint: "e", Model: "m"},
		"no endpoint":      {ID: "b", Name: "x", Protocol: "ollama", Model: "m"},
		"no model":         {ID: "c", Name: "x", Protocol: "ollama", Endpoint: "e"},
		"no name":          {ID: "d", Protocol: "ollama", Endpoint: "e", Model: "m"},
	} {
		p.CreatedAt, p.UpdatedAt = now, now
		if err := s.CreateModelProvider(ctx, p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Two roles on the same weights share the same blind spots, whatever the
// prompt says.
func TestEpistemicPairSharedIsDetected(t *testing.T) {
	same := map[string]string{domain.WorkerSolutioner: "p1", domain.WorkerChallenger: "p1"}
	diff := map[string]string{domain.WorkerSolutioner: "p1", domain.WorkerChallenger: "p2"}
	partial := map[string]string{domain.WorkerSolutioner: "p1"}

	if !domain.EpistemicPairShared(same) {
		t.Error("one provider for both epistemic roles was not flagged")
	}
	if domain.EpistemicPairShared(diff) {
		t.Error("distinct providers were flagged")
	}
	if domain.EpistemicPairShared(partial) {
		t.Error("an incomplete pair was flagged")
	}
}
