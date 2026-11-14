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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/awssig"
)

// Chat with tools: what CodeDistill's own agent loop runs on
// (docs/design/agent-loop.md). One neutral conversation, put into each
// provider's native tool-calling form and read back out of it, so the loop
// never knows which provider it is talking to.

// ChatMessage is one turn. A user turn carries text; an assistant turn
// carries text and/or tool calls; a tool turn carries the results of the
// previous assistant turn's calls.
type ChatMessage struct {
	Role        string // "user" | "assistant" | "tool"
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

// ToolCall is the model asking for a tool. ID is the provider's own (one is
// made up where the provider has none, so results can always be matched).
type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage // a JSON object
	// Signature is opaque provider state that must go back with the call on
	// the next turn: Gemini's thoughtSignature, without which a thinking
	// model refuses the conversation.
	Signature string
}

// ToolResult answers one call.
type ToolResult struct {
	CallID  string
	Name    string
	Content string
	IsError bool
}

// ToolSpec declares a tool: its name, what it is for, and its arguments as a
// JSON Schema object (type, properties, required — kept to the subset every
// provider accepts).
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}

// ChatRequest is one turn's request.
type ChatRequest struct {
	System    string
	Messages  []ChatMessage
	Tools     []ToolSpec
	MaxTokens int // the answer's cap; 0: a generous default
}

// ChatUsage is what the turn cost, as the provider reported it.
type ChatUsage struct {
	In, Out, CacheRead, CacheWrite int64
}

// ChatResponse is the model's turn.
type ChatResponse struct {
	Text      string
	ToolCalls []ToolCall
	Usage     ChatUsage
	Stop      string // the provider's own stop reason, for the log
}

const chatMaxTokens = 8192

// ErrNoToolCalling: this provider has no tool-calling path here yet.
var ErrNoToolCalling = fmt.Errorf("tool calling is not available for this provider yet")

// Chat sends one turn and returns the model's answer.
func (c *Client) Chat(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	if r.MaxTokens <= 0 {
		r.MaxTokens = chatMaxTokens
	}
	switch c.proto {
	case ProtocolAnthropic:
		return c.chatAnthropic(ctx, r)
	case ProtocolOpenAI:
		return c.chatOpenAI(ctx, r, c.endpoint+"/v1/chat/completions")
	case ProtocolAzure:
		u, err := azureURL(c.endpoint, c.model, "chat/completions")
		if err != nil {
			return nil, err
		}
		return c.chatOpenAI(ctx, r, u)
	case ProtocolGemini:
		return c.chatGemini(ctx, r)
	case ProtocolOllama:
		return c.chatOllama(ctx, r)
	case ProtocolBedrock:
		return c.chatBedrock(ctx, r)
	}
	return nil, fmt.Errorf("%s: %w", c.proto, ErrNoToolCalling)
}

// post sends a JSON body and returns the raw reply, with the status checked
// after the provider's own error object (several report failures in a 200).
func (c *Client) post(ctx context.Context, url string, body any, headers map[string]string, decodeErr func([]byte) string) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s chat: %w", c.proto, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if msg := decodeErr(raw); msg != "" {
		return nil, fmt.Errorf("%s: %s", c.proto, msg)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s status %d: %.300s", c.proto, resp.StatusCode, raw)
	}
	return raw, nil
}

func argsObject(a json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(a)) == 0 || string(bytes.TrimSpace(a)) == "null" {
		return json.RawMessage("{}")
	}
	return a
}

// ---- Anthropic Messages API ------------------------------------------------

func (c *Client) chatAnthropic(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	var msgs []map[string]any
	for _, m := range r.Messages {
		var blocks []map[string]any
		switch m.Role {
		case "tool":
			for _, tr := range m.ToolResults {
				blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": tr.CallID,
					"content": tr.Content, "is_error": tr.IsError})
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": blocks})
			continue
		}
		if m.Text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": m.Text})
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": argsObject(tc.Args)})
		}
		msgs = append(msgs, map[string]any{"role": m.Role, "content": blocks})
	}
	var tools []map[string]any
	for _, t := range r.Tools {
		tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
	}
	// The system prompt and the tools are the same every turn of a run: marked
	// for caching, later turns read them back at a fraction of the price.
	if n := len(tools); n > 0 {
		tools[n-1]["cache_control"] = map[string]string{"type": "ephemeral"}
	}
	body := map[string]any{"model": c.model, "max_tokens": r.MaxTokens, "messages": msgs}
	if r.System != "" {
		body["system"] = []map[string]any{{"type": "text", "text": r.System, "cache_control": map[string]string{"type": "ephemeral"}}}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	raw, err := c.post(ctx, strings.TrimRight(c.endpoint, "/")+"/messages", body,
		map[string]string{"anthropic-version": anthropicVersion}, func(b []byte) string {
			var e struct {
				Error *struct{ Type, Message string } `json:"error"`
			}
			if json.Unmarshal(b, &e) == nil && e.Error != nil {
				return e.Error.Message + " (" + e.Error.Type + ")"
			}
			return ""
		})
	if err != nil {
		return nil, err
	}
	var ar struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			In          int64 `json:"input_tokens"`
			Out         int64 `json:"output_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("decode anthropic chat: %w", err)
	}
	out := &ChatResponse{Stop: ar.StopReason,
		Usage: ChatUsage{In: ar.Usage.In, Out: ar.Usage.Out, CacheRead: ar.Usage.CacheRead, CacheWrite: ar.Usage.CacheCreate}}
	var text []string
	for _, b := range ar.Content {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Args: argsObject(b.Input)})
		}
	}
	out.Text = strings.Join(text, "\n")
	return out, nil
}

// ---- OpenAI Chat Completions (OpenAI-compatible servers, Azure) -----------

func openAITools(ts []ToolSpec) []map[string]any {
	var out []map[string]any
	for _, t := range ts {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Schema}})
	}
	return out
}

func (c *Client) chatOpenAI(ctx context.Context, r ChatRequest, url string) (*ChatResponse, error) {
	msgs := []map[string]any{}
	if r.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": r.System})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case "tool":
			for _, tr := range m.ToolResults {
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": tr.CallID, "content": tr.Content})
			}
		case "assistant":
			am := map[string]any{"role": "assistant", "content": m.Text}
			var calls []map[string]any
			for _, tc := range m.ToolCalls {
				calls = append(calls, map[string]any{"id": tc.ID, "type": "function",
					"function": map[string]any{"name": tc.Name, "arguments": string(argsObject(tc.Args))}})
			}
			if len(calls) > 0 {
				am["tool_calls"] = calls
			}
			msgs = append(msgs, am)
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Text})
		}
	}
	// No answer cap: OpenAI's newer models refuse max_tokens and the servers
	// that copy its API do not all know max_completion_tokens, while every one
	// of them defaults to the model's own limit, which is what an agent wants.
	body := map[string]any{"model": c.model, "messages": msgs}
	if len(r.Tools) > 0 {
		body["tools"] = openAITools(r.Tools)
	}
	raw, err := c.post(ctx, url, body, nil, func(b []byte) string {
		var e struct {
			Error *struct{ Message, Type string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			return e.Error.Message
		}
		return ""
	})
	if err != nil {
		return nil, err
	}
	var or struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
			Details    struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &or); err != nil {
		return nil, fmt.Errorf("decode chat completion: %w", err)
	}
	if len(or.Choices) == 0 {
		return nil, fmt.Errorf("%s: the reply had no choices", c.proto)
	}
	ch := or.Choices[0]
	out := &ChatResponse{Text: ch.Message.Content, Stop: ch.FinishReason,
		Usage: ChatUsage{In: or.Usage.Prompt - or.Usage.Details.Cached, Out: or.Usage.Completion, CacheRead: or.Usage.Details.Cached}}
	for i, tc := range ch.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: id, Name: tc.Function.Name, Args: argsObject(json.RawMessage(tc.Function.Arguments))})
	}
	return out, nil
}

// ---- Ollama /api/chat --------------------------------------------------------
// Native rather than Ollama's OpenAI-compatible path, because only the native
// one takes num_ctx: through /v1 a model runs at Ollama's default context
// (4,096 tokens) and an agent's instructions alone do not fit.

func (c *Client) chatOllama(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	msgs := []map[string]any{}
	if r.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": r.System})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case "tool":
			for _, tr := range m.ToolResults {
				msgs = append(msgs, map[string]any{"role": "tool", "content": tr.Content, "tool_name": tr.Name})
			}
		case "assistant":
			am := map[string]any{"role": "assistant", "content": m.Text}
			var calls []map[string]any
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal(argsObject(tc.Args), &args)
				calls = append(calls, map[string]any{"function": map[string]any{"name": tc.Name, "arguments": args}})
			}
			if len(calls) > 0 {
				am["tool_calls"] = calls
			}
			msgs = append(msgs, am)
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Text})
		}
	}
	body := map[string]any{"model": c.model, "messages": msgs, "stream": false, "options": c.options()}
	if len(r.Tools) > 0 {
		body["tools"] = openAITools(r.Tools)
	}
	raw, err := c.post(ctx, c.endpoint+"/api/chat", body, nil, func(b []byte) string {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil {
			return e.Error
		}
		return ""
	})
	if err != nil {
		return nil, err
	}
	var or struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		DoneReason string `json:"done_reason"`
		Prompt     int64  `json:"prompt_eval_count"`
		Eval       int64  `json:"eval_count"`
	}
	if err := json.Unmarshal(raw, &or); err != nil {
		return nil, fmt.Errorf("decode ollama chat: %w", err)
	}
	out := &ChatResponse{Text: or.Message.Content, Stop: or.DoneReason, Usage: ChatUsage{In: or.Prompt, Out: or.Eval}}
	for i, tc := range or.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: fmt.Sprintf("call_%d", i), Name: tc.Function.Name, Args: argsObject(tc.Function.Arguments)})
	}
	return out, nil
}

// ---- Gemini generateContent ------------------------------------------------

func (c *Client) chatGemini(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	var contents []map[string]any
	for _, m := range r.Messages {
		var parts []map[string]any
		role := "user"
		switch m.Role {
		case "tool":
			for _, tr := range m.ToolResults {
				parts = append(parts, map[string]any{"functionResponse": map[string]any{
					"name": tr.Name, "response": map[string]any{"content": tr.Content, "is_error": tr.IsError}}})
			}
		case "assistant":
			role = "model"
			if m.Text != "" {
				parts = append(parts, map[string]any{"text": m.Text})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal(argsObject(tc.Args), &args)
				part := map[string]any{"functionCall": map[string]any{"name": tc.Name, "args": args}}
				if tc.Signature != "" {
					part["thoughtSignature"] = tc.Signature
				}
				parts = append(parts, part)
			}
		default:
			parts = append(parts, map[string]any{"text": m.Text})
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	body := map[string]any{"contents": contents, "generationConfig": map[string]any{"maxOutputTokens": r.MaxTokens}}
	if r.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": r.System}}}
	}
	if len(r.Tools) > 0 {
		var decls []map[string]any
		for _, t := range r.Tools {
			decls = append(decls, map[string]any{"name": t.Name, "description": t.Description, "parameters": geminiSchema(t.Schema)})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	raw, err := c.post(ctx, geminiModelPath(c.endpoint, c.model, "generateContent"), body, nil, func(b []byte) string {
		var e struct {
			Error *struct{ Message, Status string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			return e.Error.Message + " (" + e.Error.Status + ")"
		}
		return ""
	})
	if err != nil {
		return nil, err
	}
	var gr struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					Signature    string `json:"thoughtSignature"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Prompt int64 `json:"promptTokenCount"`
			Out    int64 `json:"candidatesTokenCount"`
			Cached int64 `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("decode gemini chat: %w", err)
	}
	if len(gr.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: the reply had no candidates (it may have been blocked)")
	}
	cand := gr.Candidates[0]
	out := &ChatResponse{Stop: cand.FinishReason,
		Usage: ChatUsage{In: gr.Usage.Prompt - gr.Usage.Cached, Out: gr.Usage.Out, CacheRead: gr.Usage.Cached}}
	var text []string
	for i, p := range cand.Content.Parts {
		if p.FunctionCall != nil {
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: fmt.Sprintf("call_%d", i), Name: p.FunctionCall.Name,
				Args: argsObject(p.FunctionCall.Args), Signature: p.Signature})
		} else if p.Text != "" && !p.Thought {
			text = append(text, p.Text)
		}
	}
	out.Text = strings.Join(text, "\n")
	return out, nil
}

// ---- Bedrock Converse --------------------------------------------------------

func (c *Client) chatBedrock(ctx context.Context, r ChatRequest) (*ChatResponse, error) {
	creds, err := bedrockCreds(c.apiKey)
	if err != nil {
		return nil, err
	}
	var msgs []map[string]any
	for _, m := range r.Messages {
		var content []map[string]any
		role := m.Role
		switch m.Role {
		case "tool":
			role = "user"
			for _, tr := range m.ToolResults {
				status := "success"
				if tr.IsError {
					status = "error"
				}
				content = append(content, map[string]any{"toolResult": map[string]any{"toolUseId": tr.CallID,
					"content": []map[string]any{{"text": tr.Content}}, "status": status}})
			}
		default:
			if m.Text != "" {
				content = append(content, map[string]any{"text": m.Text})
			}
			for _, tc := range m.ToolCalls {
				content = append(content, map[string]any{"toolUse": map[string]any{"toolUseId": tc.ID, "name": tc.Name, "input": argsObject(tc.Args)}})
			}
		}
		msgs = append(msgs, map[string]any{"role": role, "content": content})
	}
	body := map[string]any{"messages": msgs, "inferenceConfig": map[string]any{"maxTokens": r.MaxTokens}}
	if r.System != "" {
		body["system"] = []map[string]any{{"text": r.System}}
	}
	if len(r.Tools) > 0 {
		var tools []map[string]any
		for _, t := range r.Tools {
			tools = append(tools, map[string]any{"toolSpec": map[string]any{"name": t.Name, "description": t.Description,
				"inputSchema": map[string]any{"json": t.Schema}}})
		}
		body["toolConfig"] = map[string]any{"tools": tools}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.endpoint, "/") + "/model/" + c.model + "/converse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	region := creds.Region
	if region == "" {
		region = awssig.RegionFromHost(req.URL.Host)
	}
	if region == "" {
		return nil, fmt.Errorf("cannot tell which AWS region %q is in — set %q in the credentials", req.URL.Host, "region")
	}
	if err := awssig.Sign(req, b, creds, region, "bedrock", time.Now()); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bedrock chat: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = string(raw)
		}
		return nil, fmt.Errorf("bedrock status %d (%s): %.300s", resp.StatusCode, resp.Header.Get("x-amzn-ErrorType"), e.Message)
	}
	var br struct {
		Output struct {
			Message struct {
				Content []struct {
					Text    string `json:"text"`
					ToolUse *struct {
						ID    string          `json:"toolUseId"`
						Name  string          `json:"name"`
						Input json.RawMessage `json:"input"`
					} `json:"toolUse"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		StopReason string `json:"stopReason"`
		Usage      struct {
			In        int64 `json:"inputTokens"`
			Out       int64 `json:"outputTokens"`
			CacheRead int64 `json:"cacheReadInputTokens"`
			CacheWrit int64 `json:"cacheWriteInputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &br); err != nil {
		return nil, fmt.Errorf("decode bedrock chat: %w", err)
	}
	out := &ChatResponse{Stop: br.StopReason,
		Usage: ChatUsage{In: br.Usage.In, Out: br.Usage.Out, CacheRead: br.Usage.CacheRead, CacheWrite: br.Usage.CacheWrit}}
	var text []string
	for _, b := range br.Output.Message.Content {
		if b.ToolUse != nil {
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ToolUse.ID, Name: b.ToolUse.Name, Args: argsObject(b.ToolUse.Input)})
		} else if b.Text != "" {
			text = append(text, b.Text)
		}
	}
	out.Text = strings.Join(text, "\n")
	return out, nil
}

// geminiSchema keeps a tool's schema to what Gemini accepts: it rejects a
// declaration carrying JSON Schema keywords outside its OpenAPI subset
// ($schema, additionalProperties…), which MCP tool schemas often have.
// Property names are left alone: a property may be called "default".
func geminiSchema(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		if l, ok := v.([]any); ok {
			out := make([]any, len(l))
			for i, x := range l {
				out[i] = geminiSchema(x)
			}
			return out
		}
		return v
	}
	out := map[string]any{}
	for k, x := range m {
		switch k {
		case "$schema", "$id", "$defs", "definitions", "additionalProperties", "examples", "const":
			continue
		case "properties":
			if props, ok := x.(map[string]any); ok {
				p := map[string]any{}
				for name, sch := range props {
					p[name] = geminiSchema(sch)
				}
				out[k] = p
				continue
			}
		}
		out[k] = geminiSchema(x)
	}
	return out
}
