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
// Chat with tools, per provider, against servers shaped like the real APIs.
// Each test sends the same conversation — a task, a tool call the model made,
// that call's result — and checks the provider saw it in its own native form,
// then reads the provider's recorded reply back into neutral tool calls.
package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var chatConversation = ChatRequest{
	System: "You fix bugs.",
	Messages: []ChatMessage{
		{Role: "user", Text: "Fix the md5 finding."},
		{Role: "assistant", Text: "Reading it.", ToolCalls: []ToolCall{
			{ID: "t1", Name: "read_file", Args: json.RawMessage(`{"path":"hash.go"}`)}}},
		{Role: "tool", ToolResults: []ToolResult{{CallID: "t1", Name: "read_file", Content: "1\tpackage hash"}}},
	},
	Tools: []ToolSpec{{Name: "read_file", Description: "Read a file.", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}},
}

// chatServer records the request path and body and answers with reply.
func chatServer(t *testing.T, reply string) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	path, body := new(string), new(map[string]any)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		json.NewDecoder(r.Body).Decode(body)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, path, body
}

// js renders part of a request for a substring check, so each test states the
// native form it expects in the provider's own words.
func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func wantCall(t *testing.T, got *ChatResponse, id, name, args string) {
	t.Helper()
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v, want one", got.ToolCalls)
	}
	tc := got.ToolCalls[0]
	if (id != "" && tc.ID != id) || tc.Name != name || strings.ReplaceAll(string(tc.Args), " ", "") != args {
		t.Fatalf("call = %s %s %s, want %s %s %s", tc.ID, tc.Name, tc.Args, id, name, args)
	}
}

func TestChatAnthropic(t *testing.T) {
	srv, path, body := chatServer(t, `{"content":[{"type":"text","text":"Now the edit."},
		{"type":"tool_use","id":"toolu_2","name":"edit_file","input":{"path":"hash.go"}}],
		"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":900}}`)
	c := New(WithEndpoint(srv.URL), WithModel("claude-x"), WithProtocol(ProtocolAnthropic), WithAPIKey("k"))
	got, err := c.Chat(context.Background(), chatConversation)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/messages" {
		t.Errorf("path = %s", *path)
	}
	b := *body
	msgs := js(b["messages"])
	for _, want := range []string{
		`{"id":"t1","input":{"path":"hash.go"},"name":"read_file","type":"tool_use"}],"role":"assistant"`,
		`{"content":[{"content":"1\tpackage hash","is_error":false,"tool_use_id":"t1","type":"tool_result"}],"role":"user"}`,
	} {
		if !strings.Contains(msgs, want) {
			t.Errorf("messages lack %s: %s", want, msgs)
		}
	}
	if !strings.Contains(js(b["system"]), `"cache_control"`) || !strings.Contains(js(b["tools"]), `"input_schema"`) {
		t.Errorf("system/tools not in native form: %s %s", js(b["system"]), js(b["tools"]))
	}
	wantCall(t, got, "toolu_2", "edit_file", `{"path":"hash.go"}`)
	if got.Text != "Now the edit." || got.Usage.CacheRead != 900 || got.Stop != "tool_use" {
		t.Errorf("response = %+v", got)
	}
}

func TestChatOpenAI(t *testing.T) {
	srv, path, body := chatServer(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_9","type":"function",
		"function":{"name":"edit_file","arguments":"{\"path\":\"hash.go\"}"}}]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":120,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":100}}}`)
	c := New(WithEndpoint(srv.URL), WithModel("gpt-x"), WithProtocol(ProtocolOpenAI), WithAPIKey("k"))
	got, err := c.Chat(context.Background(), chatConversation)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/v1/chat/completions" {
		t.Errorf("path = %s", *path)
	}
	msgs := js((*body)["messages"])
	for _, want := range []string{`"role":"system"`, `"tool_calls":[{"function":{"arguments":"{\"path\":\"hash.go\"}","name":"read_file"},"id":"t1"`,
		`"role":"tool","tool_call_id":"t1"`} {
		if !strings.Contains(msgs, want) {
			t.Errorf("messages lack %s: %s", want, msgs)
		}
	}
	if !strings.Contains(js((*body)["tools"]), `"type":"function"`) {
		t.Errorf("tools = %s", js((*body)["tools"]))
	}
	wantCall(t, got, "call_9", "edit_file", `{"path":"hash.go"}`)
	if got.Usage.In != 20 || got.Usage.CacheRead != 100 {
		t.Errorf("usage = %+v", got.Usage)
	}
}

func TestChatAzureUsesTheDeploymentURL(t *testing.T) {
	srv, path, _ := chatServer(t, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	c := New(WithEndpoint(srv.URL), WithModel("my-deploy"), WithProtocol(ProtocolAzure), WithAPIKey("k"))
	got, err := c.Chat(context.Background(), chatConversation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*path, "/my-deploy/chat/completions") {
		t.Errorf("path = %s", *path)
	}
	if got.Text != "done" || len(got.ToolCalls) != 0 {
		t.Errorf("response = %+v", got)
	}
}

// Ollama goes through its native /api/chat because only that takes num_ctx.
func TestChatOllamaNativeWithContextSize(t *testing.T) {
	srv, path, body := chatServer(t, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":
		{"name":"edit_file","arguments":{"path":"hash.go"}}}]},"done_reason":"stop","prompt_eval_count":50,"eval_count":9}`)
	c := New(WithEndpoint(srv.URL), WithModel("qwen"), WithProtocol(ProtocolOllama), WithContextTokens(32768))
	got, err := c.Chat(context.Background(), chatConversation)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/api/chat" {
		t.Errorf("path = %s", *path)
	}
	b := *body
	if !strings.Contains(js(b["options"]), `"num_ctx":32768`) || b["stream"] != false {
		t.Errorf("options/stream = %s %v", js(b["options"]), b["stream"])
	}
	if msgs := js(b["messages"]); !strings.Contains(msgs, `"role":"tool","tool_name":"read_file"`) {
		t.Errorf("messages = %s", msgs)
	}
	wantCall(t, got, "call_0", "edit_file", `{"path":"hash.go"}`)
}

func TestChatGemini(t *testing.T) {
	srv, path, body := chatServer(t, `{"candidates":[{"content":{"role":"model","parts":[{"text":"Editing."},
		{"functionCall":{"name":"edit_file","args":{"path":"hash.go"}},"thoughtSignature":"sig-2"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":80,"candidatesTokenCount":6}}`)
	c := New(WithEndpoint(srv.URL), WithModel("gemini-x"), WithProtocol(ProtocolGemini), WithAPIKey("k"))
	conv := chatConversation
	conv.Messages = append([]ChatMessage(nil), conv.Messages...)
	conv.Messages[1].ToolCalls = []ToolCall{{ID: "t1", Name: "read_file", Args: json.RawMessage(`{"path":"hash.go"}`), Signature: "sig-1"}}
	got, err := c.Chat(context.Background(), conv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(*path, "gemini-x:generateContent") {
		t.Errorf("path = %s", *path)
	}
	b := *body
	contents := js(b["contents"])
	for _, want := range []string{`"role":"model"`, `"functionCall":{"args":{"path":"hash.go"},"name":"read_file"},"thoughtSignature":"sig-1"`,
		`"functionResponse":{"name":"read_file"`} {
		if !strings.Contains(contents, want) {
			t.Errorf("contents lack %s: %s", want, contents)
		}
	}
	if !strings.Contains(js(b["tools"]), `"functionDeclarations"`) || b["systemInstruction"] == nil {
		t.Errorf("tools/system = %s %v", js(b["tools"]), b["systemInstruction"])
	}
	wantCall(t, got, "", "edit_file", `{"path":"hash.go"}`)
	if got.ToolCalls[0].Signature != "sig-2" {
		t.Errorf("signature not kept: %+v", got.ToolCalls[0])
	}
	if got.Text != "Editing." {
		t.Errorf("text = %q", got.Text)
	}
}

func TestChatBedrock(t *testing.T) {
	srv, path, body := chatServer(t, `{"output":{"message":{"role":"assistant","content":[
		{"toolUse":{"toolUseId":"tu_3","name":"edit_file","input":{"path":"hash.go"}}}]}},
		"stopReason":"tool_use","usage":{"inputTokens":40,"outputTokens":8}}`)
	c := New(WithEndpoint(srv.URL), WithModel("anthropic.claude-x-v1:0"), WithProtocol(ProtocolBedrock),
		WithAPIKey(`{"access_key_id":"AK","secret_access_key":"SK","region":"us-east-1"}`))
	got, err := c.Chat(context.Background(), chatConversation)
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/model/anthropic.claude-x-v1:0/converse" {
		t.Errorf("path = %s", *path)
	}
	b := *body
	msgs := js(b["messages"])
	for _, want := range []string{`"toolUse":{"input":{"path":"hash.go"},"name":"read_file","toolUseId":"t1"}`,
		`"toolResult":{"content":[{"text":"1\tpackage hash"}],"status":"success","toolUseId":"t1"}`} {
		if !strings.Contains(msgs, want) {
			t.Errorf("messages lack %s: %s", want, msgs)
		}
	}
	if !strings.Contains(js(b["toolConfig"]), `"inputSchema":{"json"`) {
		t.Errorf("toolConfig = %s", js(b["toolConfig"]))
	}
	wantCall(t, got, "tu_3", "edit_file", `{"path":"hash.go"}`)
}

// A provider's error object is reported in its own words, not as a decode
// failure of a reply that was never a turn.
func TestChatReportsTheProvidersError(t *testing.T) {
	srv, _, _ := chatServer(t, `{"type":"error","error":{"type":"invalid_request_error","message":"tools: bad schema"}}`)
	c := New(WithEndpoint(srv.URL), WithModel("claude-x"), WithProtocol(ProtocolAnthropic), WithAPIKey("k"))
	_, err := c.Chat(context.Background(), chatConversation)
	if err == nil || !strings.Contains(err.Error(), "tools: bad schema") {
		t.Fatalf("err = %v", err)
	}
}

func TestGeminiSchemaDropsWhatGeminiRejects(t *testing.T) {
	in := map[string]any{"$schema": "x", "type": "object", "additionalProperties": false,
		"properties": map[string]any{"default": map[string]any{"type": "string", "additionalProperties": true},
			"list": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false}}}}
	got := js(geminiSchema(in))
	if got != `{"properties":{"default":{"type":"string"},"list":{"items":{"type":"object"},"type":"array"}},"type":"object"}` {
		t.Fatalf("schema = %s", got)
	}
}
