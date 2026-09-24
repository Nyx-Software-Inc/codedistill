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

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// newTestServer builds a server over an in-memory store. Key material goes to
// a temp HOME so a test never touches the developer's real keyring.
func newTestServer(t *testing.T) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv := httptest.NewServer(NewServer(store, nil, BuildInfo{}, nil, nil, nil, nil, nil).Handler())
	t.Cleanup(srv.Close)
	return srv, store
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A key must never come back out. Not in the create response, not in the list.
// The store keeps it so a client can be built; the wire never sees it.
func TestProviderAPINeverReturnsTheKey(t *testing.T) {
	srv, _ := newTestServer(t)

	var created domain.ModelProvider
	doJSON(t, srv, "POST", "/api/v1/model-providers", map[string]any{
		"name": "cloud", "protocol": "openai", "endpoint": "https://api.example.com/v1",
		"model": "big", "api_key": "sk-super-secret-value", "context_tokens": 200000,
	}, 201, &created)

	if !created.HasAPIKey {
		t.Error("HasAPIKey false for a provider created with one")
	}

	// Raw bytes, because a struct round-trip could hide a leak the wire has.
	resp, err := srv.Client().Get(srv.URL + "/api/v1/model-providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp)

	// The secret itself, and the marker that would reveal a stored ciphertext.
	for _, forbidden := range []string{"sk-super-secret-value", "cdenc1:"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the response carries %q:\n%s", forbidden, body)
		}
	}
	// And no field named api_key at all. Checked on the decoded keys rather
	// than by substring, because "has_api_key" legitimately contains it — the
	// first version of this test failed on exactly that.
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, row := range rows {
		if _, bad := row["api_key"]; bad {
			t.Fatalf("the wire form has an api_key field: %v", row)
		}
		if _, want := row["has_api_key"]; !want {
			t.Error("has_api_key missing, so a UI cannot tell whether a key is set")
		}
	}
}

// An unrelated edit must not destroy the credential. The UI is never shown a
// key, so it sends back an empty one on every save.
func TestProviderEditKeepsTheKey(t *testing.T) {
	srv, store := newTestServer(t)

	var created domain.ModelProvider
	doJSON(t, srv, "POST", "/api/v1/model-providers", map[string]any{
		"name": "cloud", "protocol": "openai", "endpoint": "https://x/v1",
		"model": "big", "api_key": "sk-keepme", "context_tokens": 1000,
	}, 201, &created)

	before, _ := store.GetModelProvider(context.Background(), created.ID)
	if before.APIKey == "" {
		t.Fatal("precondition: no key stored")
	}

	doJSON(t, srv, "PATCH", "/api/v1/model-providers/"+created.ID, map[string]any{
		"name": "cloud renamed", "protocol": "openai", "endpoint": "https://x/v1",
		"model": "big", "api_key": "", "context_tokens": 1000, "enabled": true,
	}, 204, nil)

	after, _ := store.GetModelProvider(context.Background(), created.ID)
	if after.APIKey != before.APIKey {
		t.Fatalf("a rename destroyed the key: %q -> %q", before.APIKey, after.APIKey)
	}
	if after.Name != "cloud renamed" {
		t.Errorf("the edit did not apply: %q", after.Name)
	}
}

// The probe must record what it found, so the UI can show which connections
// actually work rather than only which are configured.
func TestTestProviderRecordsHealth(t *testing.T) {
	srv, store := newTestServer(t)

	// A model server that is reachable but refuses to generate.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()

	var created domain.ModelProvider
	doJSON(t, srv, "POST", "/api/v1/model-providers", map[string]any{
		"name": "local", "protocol": "ollama", "endpoint": fake.URL,
		"model": "missing:7b", "context_tokens": 4096, "is_local": true,
	}, 201, &created)

	var res map[string]any
	doJSON(t, srv, "POST", "/api/v1/model-providers/"+created.ID+"/test", nil, 200, &res)

	if res["outcome"] != "model_unusable" {
		t.Fatalf("outcome = %v, want model_unusable", res["outcome"])
	}
	if res["reachable"] != true {
		t.Error("it WAS reachable — reporting otherwise hides the distinction that matters")
	}
	got, _ := store.GetModelProvider(context.Background(), created.ID)
	if got.LastError == "" {
		t.Error("the failure was not recorded, so the UI cannot show it")
	}
	if got.LastOKAt != nil {
		t.Error("a failed probe stamped a success time")
	}
}

// Two roles on the same weights share blind spots. A warning, not a block.
func TestWorkersReportSharedEpistemicPair(t *testing.T) {
	srv, _ := newTestServer(t)
	var p domain.ModelProvider
	doJSON(t, srv, "POST", "/api/v1/model-providers", map[string]any{
		"name": "only one", "protocol": "ollama", "endpoint": "http://x:11434",
		"model": "m", "context_tokens": 8192, "is_local": true,
	}, 201, &p)

	for _, role := range []string{domain.WorkerSolutioner, domain.WorkerChallenger} {
		doJSON(t, srv, "PUT", "/api/v1/workflow-workers/"+role,
			map[string]any{"provider_id": p.ID}, 204, nil)
	}

	var out map[string]any
	doJSON(t, srv, "GET", "/api/v1/workflow-workers", nil, 200, &out)
	if out["epistemic_pair_shared"] != true {
		t.Fatal("one provider for both epistemic roles was not flagged")
	}

	// Clearing one must drop the warning.
	doJSON(t, srv, "PUT", "/api/v1/workflow-workers/"+domain.WorkerChallenger,
		map[string]any{"provider_id": ""}, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/workflow-workers", nil, 200, &out)
	if out["epistemic_pair_shared"] != false {
		t.Error("the warning survived clearing the binding")
	}
}

// Configuring a model on a workflow step must actually take effect. It
// persisted correctly and was ignored: decompose resolved through the global
// worker binding and never looked at the step, so the UI offered a setting that
// silently did nothing — worse than offering none.
func TestStepProviderIsWhatActuallyGetsUsed(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	global := &domain.ModelProvider{
		ID: "p-global", Name: "the global default", Protocol: "ollama",
		Endpoint: "http://localhost:11434", Model: "small", ContextTokens: 4096,
		IsLocal: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	onStep := &domain.ModelProvider{
		ID: "p-step", Name: "chosen for this workflow", Protocol: "ollama",
		Endpoint: "http://localhost:11434", Model: "big", ContextTokens: 32768,
		IsLocal: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	for _, p := range []*domain.ModelProvider{global, onStep} {
		if err := store.CreateModelProvider(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetWorkerModel(ctx, domain.WorkerDecomposer, "", global.ID, now); err != nil {
		t.Fatal(err)
	}

	// With no step provider, the global binding applies.
	_, got, err := StepClient(ctx, store, domain.JobDecompose, 0, domain.WorkerDecomposer, "p1", time.Minute)
	if err != nil || got == nil || got.ID != global.ID {
		t.Fatalf("fallback = %+v, %v; want the global binding", got, err)
	}

	// Setting one through the API must change which model actually runs.
	doJSON(t, srv, "PUT", "/api/v1/workflows/decompose/steps/0/provider",
		map[string]any{"provider_id": onStep.ID}, 204, nil)

	_, got, err = StepClient(ctx, store, domain.JobDecompose, 0, domain.WorkerDecomposer, "p1", time.Minute)
	if err != nil || got == nil {
		t.Fatalf("StepClient = %+v, %v", got, err)
	}
	if got.ID != onStep.ID {
		t.Fatalf("the run would use %q; the workflow was configured to use %q — "+
			"the setting persisted and did nothing", got.Name, onStep.Name)
	}
}
