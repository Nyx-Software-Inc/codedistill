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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codedistill/internal/domain"
	"codedistill/internal/ollama"
	"codedistill/internal/storage/sqlite"
)

// fakeModelServer answers BOTH wire protocols and records what it was actually
// asked. Which path gets hit is the observable that distinguishes the Ollama
// wire API from the OpenAI-compatible one, so the test asserts on real request
// behaviour rather than on the shape of the wiring.
type fakeModelServer struct {
	*httptest.Server
	paths  []string
	models []string
}

func newFakeModelServer(t *testing.T) *fakeModelServer {
	t.Helper()
	f := &fakeModelServer{}
	// A minimal but valid classification the agent can parse.
	const classification = `{"category":"todo","reasoning":"test","confidence":0.9}`

	mux := http.NewServeMux()
	record := func(r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.paths = append(f.paths, r.URL.Path)
		f.models = append(f.models, body.Model)
	}
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": classification})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": classification}}},
		})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// newClassifyDB returns a migrated, seeded database path for cmdClassify to
// reopen, plus a hook to write project settings before the command runs.
func newClassifyDB(t *testing.T, projectSettings map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codedistill.db")
	store, err := sqlite.OpenDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaults(ctx, store); err != nil {
		t.Fatal(err)
	}
	for k, v := range projectSettings {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetProjectSetting(ctx, &domain.ProjectSetting{
			ProjectID: defaultProjectID, Key: k, Value: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// `classify` is advertised in --help as a smoke test. A smoke test that builds a
// DIFFERENT model stack than `serve` is worse than none: it gives false
// confidence when it passes and a false alarm when it diverges.
//
// cmdClassify omitted ollama.WithProtocol — it did not even accept -model-api —
// so an OpenAI-compatible endpoint received Ollama's wire format and 404'd. The
// user concludes their endpoint config is broken and goes off breaking a working
// serve setup chasing it (CE-review item 12).
func TestClassify_HonorsModelAPI(t *testing.T) {
	srv := newFakeModelServer(t)
	db := newClassifyDB(t, nil)

	if err := cmdClassify(db, ollama.DefaultModel, srv.URL, string(ollama.ProtocolOpenAI), "a test item", ""); err != nil {
		t.Fatalf("classify against an OpenAI-compatible endpoint: %v", err)
	}

	if len(srv.paths) == 0 {
		t.Fatal("the model server was never called")
	}
	if got := srv.paths[0]; got != "/v1/chat/completions" {
		t.Errorf("-model-api openai hit %q; want /v1/chat/completions.\n"+
			"The OpenAI wire API was ignored, so a real endpoint would 404.", got)
	}
}

// The Ollama default must keep working — the protocol fix must not flip the
// default wire API for everyone who never passes -model-api.
func TestClassify_DefaultsToOllamaWireAPI(t *testing.T) {
	srv := newFakeModelServer(t)
	db := newClassifyDB(t, nil)

	if err := cmdClassify(db, ollama.DefaultModel, srv.URL, "", "a test item", ""); err != nil {
		t.Fatalf("classify with the default protocol: %v", err)
	}
	if got := srv.paths[0]; got != "/api/generate" {
		t.Errorf("default protocol hit %q; want /api/generate", got)
	}
}

// Per-project model.classifier was never consulted: GenerateModel was nil, so
// OllamaClassifier.gen fell to the model-less path. The CLI therefore classified
// with a different model than the app, and the user reads that divergence as
// model nondeterminism (CE-review item 12).
func TestClassify_UsesPerProjectClassifierModel(t *testing.T) {
	const want = "per-project-classifier-model"
	srv := newFakeModelServer(t)
	db := newClassifyDB(t, map[string]string{"model.classifier": want})

	if err := cmdClassify(db, ollama.DefaultModel, srv.URL, "", "a test item", ""); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if len(srv.models) == 0 {
		t.Fatal("the model server was never called")
	}
	if got := srv.models[0]; got != want {
		t.Errorf("classify ran against model %q; want the project's model.classifier %q.\n"+
			"The CLI is using a different model than serve would.", got, want)
	}
}
