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

package modelprobe

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The model list comes from the CONTROL PLANE host, not the runtime one the
// user configured. Deriving it wrong means listing works in testing (same
// machine) and 404s against real AWS.
func TestControlPlaneHostIsDerivedFromTheRuntimeOne(t *testing.T) {
	cases := map[string]string{
		"https://bedrock-runtime.us-east-1.amazonaws.com": "https://bedrock.us-east-1.amazonaws.com",
		"https://bedrock-runtime.eu-west-2.amazonaws.com": "https://bedrock.eu-west-2.amazonaws.com",
		// Not ours to rewrite: a gateway or PrivateLink name is left alone, so
		// a wrong guess cannot turn into a confusing failure.
		"https://llm.internal.example": "https://llm.internal.example",
	}
	for in, want := range cases {
		if got := controlPlaneHost(in); got != want {
			t.Errorf("controlPlaneHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// Listing must sign, and must drop models that would fail if selected.
func TestBedrockListSignsAndFiltersUnusableModels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(`{"modelSummaries":[
		  {"modelId":"anthropic.claude-3-5-sonnet-20241022-v2:0","modelName":"Claude 3.5 Sonnet",
		   "providerName":"Anthropic","outputModalities":["TEXT"],"inferenceTypesSupported":["ON_DEMAND"]},
		  {"modelId":"stability.stable-diffusion-xl-v1","modelName":"SDXL",
		   "providerName":"Stability AI","outputModalities":["IMAGE"],"inferenceTypesSupported":["ON_DEMAND"]},
		  {"modelId":"meta.llama3-70b-provisioned","modelName":"Llama 3 70B",
		   "providerName":"Meta","outputModalities":["TEXT"],"inferenceTypesSupported":["PROVISIONED"]}
		]}`))
	}))
	defer srv.Close()

	got, err := ListModels(t.Context(), srv.Client(), Target{
		Protocol: "bedrock", Endpoint: srv.URL,
		APIKey: `{"access_key_id":"AK","secret_access_key":"SK","region":"us-east-1"}`,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if gotPath != "/foundation-models" {
		t.Fatalf("asked %s, want /foundation-models", gotPath)
	}
	if !strings.Contains(gotAuth, "AWS4-HMAC-SHA256 Credential=AK/") {
		t.Fatalf("the list request was not signed: %q", gotAuth)
	}
	if len(got) != 1 {
		t.Fatalf("got %d models, want 1 — an IMAGE model and a PROVISIONED-only one must be dropped: %+v", len(got), got)
	}
	if got[0].ID != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Fatalf("wrong model survived: %+v", got[0])
	}
	if got[0].Family != "Anthropic" {
		t.Fatalf("vendor lost: %+v", got[0])
	}
}

// A rejected credential must say what permission is missing, not "403".
func TestBedrockListExplainsARejectedCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"User is not authorized"}`))
	}))
	defer srv.Close()

	_, err := ListModels(t.Context(), srv.Client(), Target{
		Protocol: "bedrock", Endpoint: srv.URL,
		APIKey: `{"access_key_id":"AK","secret_access_key":"SK","region":"eu-west-1"}`,
	})
	if err == nil {
		t.Fatal("a 403 was reported as success")
	}
	if !strings.Contains(err.Error(), "ListFoundationModels") || !strings.Contains(err.Error(), "eu-west-1") {
		t.Fatalf("error should name the permission and region, got: %v", err)
	}
	// AWS's own words must survive: a signature it could not verify and a
	// credential lacking permission are both 403s that send you to opposite
	// places, and only the body tells them apart.
	if !strings.Contains(err.Error(), "User is not authorized") {
		t.Fatalf("AWS's own message was swallowed: %v", err)
	}
}
