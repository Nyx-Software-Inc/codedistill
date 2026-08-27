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

// Package mcpclient is CodeDistill's outbound MCP client — used to push
// items to a remote MCP server (Linear, Jira, GitHub Issues, Notion, …)
// per the v0.8.2 design (see project_mcp_design.md).
//
// The package wraps github.com/mark3labs/mcp-go's client transport behind
// a CodeDistill-shaped surface so the rest of the codebase doesn't import
// mcp-go types. v1 supports HTTP (Streamable-HTTP) only; stdio is a
// straightforward addition once a real destination needs it.
//
// Lifecycle:
//
//	cli, err := mcpclient.New(ctx, ep)
//	if err != nil { ... }
//	defer cli.Close()
//
//	tools, err := cli.ListTools(ctx)             // discovery
//	res,   err := cli.CallTool(ctx, "create_issue", args)  // invocation
//
// Endpoint + Credentials are plain values so they round-trip cleanly
// through user_settings.mcp.export.<type> JSON.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpcli "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// defaultTimeout caps a single HTTP request + stream wait. Conservative
// because the worker is async — we'd rather fail fast and retry than
// block the queue on a slow remote.
const defaultTimeout = 30 * time.Second

// Endpoint addresses a remote MCP server. URL is required. Credentials
// is optional (nil = unauthenticated; correct for our own MCP server and
// for testing, rare in practice for production destinations).
type Endpoint struct {
	URL         string         `json:"url"`
	Credentials *Credentials   `json:"credentials,omitempty"`
	Timeout     time.Duration  `json:"timeout_ns,omitempty"`
	// Headers is for non-credential headers the destination may require
	// (e.g. a tenant id). Credentials are kept separate so they can be
	// scrubbed from logs / settings exports independently.
	Headers map[string]string `json:"headers,omitempty"`
}

// Credentials is a discriminated union over the auth styles real MCP
// servers actually use today. We deliberately don't model OAuth flows
// here — those need user-interactive consent that the CodeDistill UI
// doesn't yet surface. v1 is bearer / custom header only.
//
// Type=="bearer":   adds "Authorization: Bearer <Token>"
// Type=="header":   adds "<Name>: <Token>"
// Type=="" or nil:  no auth header
type Credentials struct {
	Type  string `json:"type"`            // "bearer" | "header"
	Name  string `json:"name,omitempty"`  // header name when Type=="header"
	Token string `json:"token,omitempty"` // secret value
}

// Tool is a CodeDistill-shaped view of an MCP tool definition returned
// by tools/list. We keep the input schema as raw JSON because both the
// LLM mapping prompt (Ollama) and any future arg validator want to see
// the original JSON Schema, not a remarshalled approximation.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// CallResult is the post-invocation view of a remote tool call.
//
// IsError is true when the server marked the response itself as an
// error (CallToolResult.IsError) — distinct from a transport error
// returned alongside an empty result. Worker code branches on both:
// transport errors trigger retry; IsError=true is logged with payload
// and surfaced via the per-item sync_failed indicator without retry
// (the destination has authoritatively rejected the request).
type CallResult struct {
	Content []ContentBlock  `json:"content"`
	IsError bool            `json:"is_error"`
	Raw     json.RawMessage `json:"raw"` // full server response for debugging / queue payload preservation
}

// ContentBlock is the per-part view of a tool's response. Most server
// tools return one or more text blocks; some return resource pointers.
// We surface Type + Text for the common case; richer types (image,
// embedded resource) are accessible via CallResult.Raw.
type ContentBlock struct {
	Type string `json:"type"`           // "text" | "image" | "resource" | ...
	Text string `json:"text,omitempty"` // populated when Type == "text"
}

// Client is one live connection to a destination MCP server.
//
// Not safe for concurrent CallTool/ListTools by multiple goroutines —
// the underlying mcp-go client serializes requests, but we don't want
// to depend on that. The export queue worker uses one Client per
// (user, type) destination at a time; that's the intended pattern.
type Client struct {
	inner  *mcpcli.Client
	closed bool
}

// New connects to ep, performs the MCP initialize handshake, and
// returns a ready-to-use Client. The caller must Close it.
//
// Returns an error if the URL is malformed, the transport can't be
// brought up, or the initialize handshake fails. Any of those mean the
// destination is unreachable / misconfigured; the worker should surface
// the failure and back off rather than retry tightly.
func New(ctx context.Context, ep Endpoint) (*Client, error) {
	if ep.URL == "" {
		return nil, fmt.Errorf("endpoint URL required")
	}
	timeout := ep.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	headers := mergeHeaders(ep.Headers, ep.Credentials)
	opts := []transport.StreamableHTTPCOption{
		transport.WithHTTPTimeout(timeout),
	}
	if len(headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(headers))
	}

	inner, err := mcpcli.NewStreamableHttpClient(ep.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: build transport: %w", err)
	}

	if err := inner.Start(ctx); err != nil {
		return nil, fmt.Errorf("mcpclient: start transport: %w", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "codedistill-export",
		Version: "1",
	}
	if _, err := inner.Initialize(ctx, initReq); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("mcpclient: initialize: %w", err)
	}

	return &Client{inner: inner}, nil
}

// Close releases the transport. Idempotent.
func (c *Client) Close() error {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	return c.inner.Close()
}

// ListTools fetches the destination's tool catalog via MCP tools/list.
// The result is the input to the method-mapping flow (LLM-suggest +
// manual override) that decides which destination tool implements
// create / update / status_change / delete for an item type.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if c == nil || c.closed {
		return nil, fmt.Errorf("mcpclient: client closed")
	}
	resp, err := c.inner.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("mcpclient: list tools: %w", err)
	}
	out := make([]Tool, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcpclient: marshal schema for %q: %w", t.Name, err)
		}
		out = append(out, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out, nil
}

// CallTool invokes a remote tool by name with the given arguments.
// args is shaped per the tool's input schema; the worker constructs
// it from the local item + the user's mapping.
//
// Returns CallResult with IsError set when the server marked the
// response as an error (distinct from a transport / protocol failure
// returned via the err return).
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*CallResult, error) {
	if c == nil || c.closed {
		return nil, fmt.Errorf("mcpclient: client closed")
	}
	if name == "" {
		return nil, fmt.Errorf("mcpclient: tool name required")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	resp, err := c.inner.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: call %q: %w", name, err)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: marshal response: %w", err)
	}

	out := &CallResult{
		IsError: resp.IsError,
		Raw:     raw,
		Content: make([]ContentBlock, 0, len(resp.Content)),
	}
	for _, c := range resp.Content {
		// mcp-go returns mcp.Content interface values; concrete types are
		// TextContent, ImageContent, etc. We only surface text directly;
		// callers that need richer types can decode CallResult.Raw.
		if tc, ok := c.(mcp.TextContent); ok {
			out.Content = append(out.Content, ContentBlock{Type: "text", Text: tc.Text})
			continue
		}
		// Use the embedded type name as a best-effort label so the worker
		// can log "got an image back" without inspecting the raw payload.
		out.Content = append(out.Content, ContentBlock{Type: contentTypeOf(c)})
	}
	return out, nil
}

// mergeHeaders combines user-supplied non-credential headers with the
// auth header derived from creds. Credential headers win on collision —
// users shouldn't override Authorization via the Headers map.
func mergeHeaders(userHeaders map[string]string, creds *Credentials) map[string]string {
	out := make(map[string]string, len(userHeaders)+1)
	for k, v := range userHeaders {
		out[k] = v
	}
	if creds == nil || creds.Token == "" {
		return out
	}
	switch creds.Type {
	case "bearer":
		out["Authorization"] = "Bearer " + creds.Token
	case "header":
		if creds.Name != "" {
			out[creds.Name] = creds.Token
		}
	}
	return out
}

// contentTypeOf returns a short label for non-text mcp.Content values.
// Used only when we receive a content block we don't surface directly,
// so the worker has something to log.
func contentTypeOf(c mcp.Content) string {
	switch c.(type) {
	case mcp.ImageContent:
		return "image"
	case mcp.AudioContent:
		return "audio"
	case mcp.EmbeddedResource:
		return "resource"
	default:
		return "unknown"
	}
}
