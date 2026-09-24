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

package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Three things Azure needs that OpenAI does not, each a different failure if
// missed: the api-key header (a Bearer token is a 401), the deployment in the
// path (a model id there is a 404), and api-version (absent is a 400).
func TestAzureSendsKeyDeploymentAndAPIVersion(t *testing.T) {
	var gotPath, gotQuery, gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotKey, gotAuth = r.Header.Get("api-key"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithProtocol(ProtocolAzure),
		WithModel("gpt4o-prod"), WithAPIKey("azure-secret"))
	out, err := c.GenerateJSON(context.Background(), "classify this")
	if err != nil {
		t.Fatalf("azure generate failed: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("content not returned: %q", out)
	}
	if gotKey != "azure-secret" {
		t.Fatalf("api-key header was %q — without it Azure answers 401", gotKey)
	}
	if gotAuth != "" {
		t.Fatalf("a Bearer token was sent as well (%q); Azure rejects that form", gotAuth)
	}
	if gotPath != "/openai/deployments/gpt4o-prod/chat/completions" {
		t.Fatalf("path %q — the deployment name belongs in the path", gotPath)
	}
	if !strings.Contains(gotQuery, "api-version=") {
		t.Fatalf("no api-version (%q) — Azure answers 400 without it", gotQuery)
	}
}

// An endpoint copied from the portal may already end in /openai. Appending a
// second one produces a 404 that looks like a missing deployment.
func TestAzureURLToleratesBothEndpointForms(t *testing.T) {
	for _, base := range []string{
		"https://r.openai.azure.com",
		"https://r.openai.azure.com/",
		"https://r.openai.azure.com/openai",
	} {
		got, err := azureURL(base, "dep", "chat/completions")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "https://r.openai.azure.com/openai/deployments/dep/chat/completions?api-version=") {
			t.Errorf("%s -> %s", base, got)
		}
		if strings.Contains(got, "/openai/openai/") {
			t.Errorf("%s produced a doubled /openai: %s", base, got)
		}
	}
}

// A pinned api-version on the endpoint must win: Azure's versions change
// request and response shapes, so someone who pinned one meant it.
func TestAzureRespectsAPinnedAPIVersion(t *testing.T) {
	got, err := azureURL("https://r.openai.azure.com?api-version=2023-05-15", "dep", "chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "api-version=2023-05-15") {
		t.Fatalf("the pinned version was replaced: %s", got)
	}
}

// The regression this whole seam exists for: a hosted OpenAI-compatible
// provider was never sent its key, so OpenAI, OpenRouter, Groq and Together
// would all have answered 401 at the first call.
func TestOpenAIActuallySendsItsKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()
	c := New(WithEndpoint(srv.URL), WithProtocol(ProtocolOpenAI),
		WithModel("gpt-4o"), WithAPIKey("sk-secret"))
	if _, err := c.GenerateJSON(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-secret" {
		t.Fatalf("Authorization was %q — a hosted provider would answer 401", auth)
	}
}
