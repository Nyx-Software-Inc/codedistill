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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The OpenAI-compatible protocol posts to /v1/chat/completions, asks for a JSON
// response_format, and returns the assistant message content.
func TestGenerateJSON_OpenAIProtocol(t *testing.T) {
	var gotPath, gotModel string
	var gotJSONFormat bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		gotJSONFormat = req.ResponseFormat["type"] == "json_object"
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{{Message: chatMessage{Role: "assistant", Content: `{"ok":true}`}}},
		})
	}))
	defer ts.Close()

	c := New(WithEndpoint(ts.URL), WithProtocol(ProtocolOpenAI), WithModel("local-qwen"))
	out, err := c.GenerateJSON(context.Background(), "do x")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if out != `{"ok":true}` {
		t.Errorf("content = %q, want JSON body", out)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotModel != "local-qwen" {
		t.Errorf("model = %q, want local-qwen", gotModel)
	}
	if !gotJSONFormat {
		t.Error("expected response_format json_object")
	}
}

// ListModels over the OpenAI protocol reads /v1/models → data[].id.
func TestListModels_OpenAIProtocol(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/models") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"qwen2.5:14b"},{"id":"deepseek-coder"}]}`)
	}))
	defer ts.Close()

	c := New(WithEndpoint(ts.URL), WithProtocol(ProtocolOpenAI))
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 2 || models[0] != "qwen2.5:14b" || models[1] != "deepseek-coder" {
		t.Errorf("models = %v", models)
	}
}
