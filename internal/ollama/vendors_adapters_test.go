// =============================================================================
//
//	Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//	CodeDistill
//
//	Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//	Public License v3.0 (see the LICENSE file) and, separately, a commercial
//	license available from Nyx Software, Inc. Use outside the terms of one of those
//	licenses is prohibited.
//
//	SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
//
// =============================================================================
// Anthropic and Gemini adapters, against servers shaped like the real APIs.
// Each assertion is a difference that breaks a naive reuse of the OpenAI path.
package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Claude has no JSON mode. Asking politely in the prompt works most of the
// time, which is the worst property a parser dependency can have, so output is
// forced with a pinned tool and the tool's arguments ARE the answer.
func TestAnthropicForcesJSONWithATool(t *testing.T) {
	var got struct {
		Model      string           `json:"model"`
		MaxTokens  int              `json:"max_tokens"`
		Tools      []map[string]any `json:"tools"`
		ToolChoice map[string]any   `json:"tool_choice"`
	}
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"content":[{"type":"tool_use","name":"emit_json",
			"input":{"kind":"bug","subject":"it broke"}}],
			"stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":5}}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("claude-x"),
		WithProtocol(ProtocolAnthropic), WithAPIKey("sk-ant-test"))
	out, err := c.GenerateJSON(context.Background(), "classify this")
	if err != nil {
		t.Fatal(err)
	}

	// The arguments are returned verbatim, so a caller decodes structure rather
	// than prose that resembles it.
	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("result is not JSON: %q", out)
	}
	if parsed["kind"] != "bug" {
		t.Errorf("tool arguments lost: %v", parsed)
	}

	// x-api-key, NOT Authorization: Bearer. A Bearer token is a 401 whose
	// message does not say why.
	if hdr.Get("x-api-key") != "sk-ant-test" {
		t.Errorf("key sent as %q / auth %q", hdr.Get("x-api-key"), hdr.Get("Authorization"))
	}
	if hdr.Get("anthropic-version") == "" {
		t.Error("anthropic-version missing; the API rejects the request without it")
	}
	if got.MaxTokens <= 0 {
		t.Error("max_tokens missing; it is required, not optional")
	}
	if len(got.Tools) != 1 || got.ToolChoice["name"] != "emit_json" {
		t.Errorf("the tool was not pinned, so JSON is a hope: tools=%v choice=%v", got.Tools, got.ToolChoice)
	}
}

// A truncated structured answer decodes as malformed JSON three layers away
// with nothing pointing at the cause, so the cause is named here.
func TestAnthropicNamesATruncatedAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content":[],"stop_reason":"max_tokens"}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("claude-x"), WithProtocol(ProtocolAnthropic))
	_, err := c.GenerateJSON(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "cut off") {
		t.Fatalf("err = %v; want it to name the token limit", err)
	}
}

// Gemini puts the model in the PATH and has a real JSON mode, which is why it
// cannot ride the OpenAI adapter with a different base URL.
func TestGeminiUsesPathModelAndNativeJSON(t *testing.T) {
	var path string
	var hdr http.Header
	var body struct {
		GenerationConfig struct {
			ResponseMIMEType string `json:"responseMimeType"`
		} `json:"generationConfig"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		hdr = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}],
			"usageMetadata":{"promptTokenCount":9}}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("models/gemini-2.0-flash"),
		WithProtocol(ProtocolGemini), WithAPIKey("goog-test"))
	out, err := c.GenerateJSON(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"ok":true}` {
		t.Errorf("text part not unwrapped: %q", out)
	}
	if !strings.Contains(path, "/models/gemini-2.0-flash:generateContent") {
		t.Errorf("model not in the path: %q", path)
	}
	// The "models/" prefix must be tolerated: the list endpoint returns it,
	// a human types it without.
	if strings.Contains(path, "models/models/") {
		t.Errorf("prefix doubled: %q", path)
	}
	if body.GenerationConfig.ResponseMIMEType != "application/json" {
		t.Error("native JSON mode not requested; structured output would be a prompt instruction and a hope")
	}
	// Header, not ?key=: a credential in a URL lands in access and proxy logs.
	if hdr.Get("x-goog-api-key") != "goog-test" {
		t.Errorf("key header = %q", hdr.Get("x-goog-api-key"))
	}
}

// Gemini reports some failures in the body with a 200, so the error object is
// checked before the status code.
func TestGeminiReportsABlockedAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`))
	}))
	defer srv.Close()

	c := New(WithEndpoint(srv.URL), WithModel("g"), WithProtocol(ProtocolGemini))
	_, err := c.GenerateJSON(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Fatalf("err = %v; want the finish reason surfaced", err)
	}
}

func TestParseProtocolKnowsAllFour(t *testing.T) {
	for in, want := range map[string]Protocol{
		"ollama": ProtocolOllama, "openai": ProtocolOpenAI,
		"anthropic": ProtocolAnthropic, "gemini": ProtocolGemini,
		// Unknown defaults rather than erroring: this is read from config, and
		// a typo should land on the local model, not take the process down.
		"telepathy": ProtocolOllama, "": ProtocolOllama,
	} {
		if got := ParseProtocol(in); got != want {
			t.Errorf("ParseProtocol(%q) = %q, want %q", in, got, want)
		}
	}
}
