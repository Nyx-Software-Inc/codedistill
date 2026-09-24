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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// End to end against a fake Bedrock: the request must be signed, hit the
// Converse path with the tool pinned, and the tool's arguments must come back
// as the answer.
func TestBedrockConverseReturnsTheToolArguments(t *testing.T) {
	var gotAuth, gotPath, gotAmzDate string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		gotAmzDate = r.Header.Get("X-Amz-Date")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[
			{"toolUse":{"name":"emit_json","input":{"moves":[{"sentence":1}]}}}]}},
			"stopReason":"tool_use","usage":{"inputTokens":10,"outputTokens":5}}`))
	}))
	defer srv.Close()

	// The region rides in the credential: a test server on 127.0.0.1 has no
	// region in its host, which is the same situation as a PrivateLink or
	// gateway hostname in production.
	c := New(WithEndpoint(srv.URL), WithProtocol(ProtocolBedrock),
		WithModel("anthropic.claude-3-5-sonnet-20241022-v2:0"),
		WithAPIKey(`{"access_key_id":"AK","secret_access_key":"SK","region":"us-east-1"}`))

	out, err := c.GenerateJSONWithModel(t.Context(), "", "read this")
	if err != nil {
		t.Fatalf("converse failed: %v", err)
	}
	if out != `{"moves":[{"sentence":1}]}` {
		t.Fatalf("the tool arguments are the answer; got %s", out)
	}

	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AK/") {
		t.Fatalf("request was not signed: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "/us-east-1/bedrock/aws4_request") {
		t.Fatalf("wrong credential scope: %q", gotAuth)
	}
	if gotAmzDate == "" {
		t.Fatal("X-Amz-Date missing — the signature covers it")
	}
	// The colon in the model id must survive onto the wire, whatever the
	// canonical form used for signing.
	if gotPath != "/model/anthropic.claude-3-5-sonnet-20241022-v2:0/converse" {
		t.Fatalf("wrong path: %s", gotPath)
	}

	var sent bedrockRequest
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("body is not a converse request: %v", err)
	}
	if sent.ToolConfig == nil || sent.ToolConfig.ToolChoice == nil {
		t.Fatal("the tool was not pinned — structured output would be 'usually JSON'")
	}
	if sent.InferenceConfig.MaxTokens != bedrockMaxTokens {
		t.Fatalf("maxTokens %d, want %d", sent.InferenceConfig.MaxTokens, bedrockMaxTokens)
	}
}

// A host with no region and no region in the credential must fail loudly
// rather than sign with an empty region and collect a 403 nobody can read.
func TestBedrockRefusesWhenTheRegionIsUnknowable(t *testing.T) {
	c := New(WithEndpoint("http://127.0.0.1:1"), WithProtocol(ProtocolBedrock),
		WithModel("m"), WithAPIKey(`{"access_key_id":"AK","secret_access_key":"SK"}`))
	_, err := c.GenerateJSONWithModel(t.Context(), "", "hi")
	if err == nil {
		t.Fatal("an endpoint with no discoverable region was accepted")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Fatalf("the error should name the region problem, got: %v", err)
	}
}

// Credentials that are not JSON, or are half-filled, must be reported as a
// configuration problem before any request is attempted.
func TestBedrockCredentialErrorsAreLegible(t *testing.T) {
	for _, tc := range []struct{ secret, want string }{
		{"", "credentials"},
		{"sk-not-json", "not readable"},
		{`{"access_key_id":"AK"}`, "incomplete"},
	} {
		_, err := bedrockCreds(tc.secret)
		if err == nil {
			t.Fatalf("%q was accepted", tc.secret)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q -> %v, want a message mentioning %q", tc.secret, err, tc.want)
		}
	}
}
