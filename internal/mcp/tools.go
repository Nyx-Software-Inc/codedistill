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

// Package mcp implements CodeDistill's MCP server tool surface, shared
// between the stdio subcommand (`codedistill mcp`) and the HTTP /mcp
// adapter on `codedistill serve`. Tool handlers are methods on *Tools
// taking a storage.Storage; transport adapters wrap them thin.
//
// v1 surface — read-mostly + 2 writes:
//
//	list_projects                list_use_cases
//	list_scratchpads             read_item
//	list_scratchpad_items        create_anchor   *(write)*
//	list_todos                   mark_implemented *(write)*
//	list_bugs
//	list_kb
//
// Per the locked design (memory: project_mcp_design.md), v1 keeps the
// surface narrow. list_projects + list_scratchpads were added on top of
// the original 8-tool list because without them the agent has no way to
// discover ids — they're trivial reads with no design tradeoff.
package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"codedistill/internal/blobstore"
	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// resolveDefaultWorkspace handles the no-workspace_name case. When
// exactly one workspace exists (the dogfood-of-one path), use it. When
// more than one exists, the agent must pass workspace_name — silently
// picking would be wrong. Matches "names as identity" intent: don't
// depend on the seeded name "Local" being the right default; depend on
// "there's only one, you know which one I mean".
func (t *Tools) resolveDefaultWorkspace(ctx context.Context) (*domain.Workspace, error) {
	all, err := t.Store.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	switch len(all) {
	case 0:
		return nil, fmt.Errorf("no workspaces exist; run `codedistill init`")
	case 1:
		return all[0], nil
	default:
		names := make([]string, 0, len(all))
		for _, w := range all {
			names = append(names, w.Name)
		}
		return nil, fmt.Errorf("workspace_name is required: multiple workspaces exist (%v)", names)
	}
}

const (
	// ServerName / ServerVersion identify this server during the MCP
	// initialize handshake. Version stays in lockstep with the binary's
	// build-time version (passed in by the caller).
	ServerName = "codedistill"
)

// Tools owns the dependencies every tool needs. Construct one per server
// instance — it's a pointer to the underlying storage; no per-call state.
//
// Enqueuer is optional. When provided (serve mode wires *agent.Agent),
// create_scratchpad_item routes new items through the classification
// queue immediately. When nil (stdio mode without serve), the create
// tool still registers — items land in 'unprocessed' and get picked up
// the next time `codedistill serve` runs, or stay parked indefinitely
// if the caller passed classification_override.
//
// Blobs is optional. When provided (serve mode), read_item enriches
// scratchpad_item responses with blob_url + blob_url_expires_at so the
// LLM can fetch image/file bytes directly. When nil (stdio mode),
// those fields are simply omitted — items still return their metadata.
// BlobURLTTL controls the presigning lifetime; ignored by the
// LocalStore impl, honored by S3 (Slice 5).
type Tools struct {
	Store      storage.Storage
	Enqueuer   Enqueuer
	Verifier   Verifier
	Blobs      blobstore.BlobStore
	BlobURLTTL time.Duration
	// Notify is optional. When wired (serve mode), it's called after a
	// successful mutating write tool so SSE subscribers (the SPA) refresh
	// immediately. When nil (stdio mode — no HTTP/SSE), writes are silent.
	Notify func()
}

// New returns a Tools value bound to store. The returned value is safe to
// share across goroutines as long as store is.
func New(store storage.Storage) *Tools {
	return &Tools{Store: store}
}

// Enqueuer is the subset of *agent.Agent that MCP needs. Tools accept it
// as an interface to avoid a circular import (mcp ↔ agent). Defined here
// (shared) because the Tools field + WithEnqueuer reference it even in the
// oss build, where the create_scratchpad_item write tool that USES it is
// stripped.
type Enqueuer interface {
	Enqueue(itemID string)
}

// WithEnqueuer fluent-style attaches the classification queue dependency
// so create_scratchpad_item enqueues new items for the classifier.
// Returns the receiver to allow chained construction.
func (t *Tools) WithEnqueuer(e Enqueuer) *Tools {
	t.Enqueuer = e
	return t
}

// Verifier is the subset of *verify.Service that MCP needs. Tools accept it
// as an interface (mirroring Enqueuer) so the shared package doesn't pull in
// the verify package, and so the field is harmless in the oss build where the
// record_implementation write tool that USES it is stripped. When provided
// (serve mode), record_implementation kicks off deterministic verification at
// the recorded commit; when nil (stdio mode), recording just skips it.
type Verifier interface {
	Trigger(ctx context.Context, ownerType, ownerID, commitSHA string) ([]*domain.VerificationResult, error)
}

// WithVerifier fluent-style attaches the verification service so
// record_implementation auto-runs the deterministic layer after capturing
// provenance. Returns the receiver to allow chained construction.
func (t *Tools) WithVerifier(v Verifier) *Tools {
	t.Verifier = v
	return t
}

// WithBlobs fluent-style attaches the blob store + URL TTL so
// read_item can hand back fetchable URLs for binary items. Returns
// the receiver to allow chained construction.
func (t *Tools) WithBlobs(bs blobstore.BlobStore, ttl time.Duration) *Tools {
	t.Blobs = bs
	t.BlobURLTTL = ttl
	return t
}

// WithNotifier fluent-style attaches a change-notification callback fired
// after every successful mutating write tool. In serve mode it's wired to
// publish events.ItemsChanged so the SPA's SSE stream refreshes immediately;
// in stdio mode it stays nil (no HTTP/SSE subscribers to notify). Returns the
// receiver to allow chained construction.
func (t *Tools) WithNotifier(notify func()) *Tools {
	t.Notify = notify
	return t
}

// touch fires the change-notification callback if one is wired. Best-effort
// and nil-safe. Called (via notifyMiddleware) after a mutating write succeeds
// so serve-mode SSE subscribers refresh without waiting for the SPA's
// disconnected-only fallback poll.
func (t *Tools) touch() {
	if t.Notify != nil {
		t.Notify()
	}
}

// mutatingWriteTools are the write tools that change item state and therefore
// must push a live update to serve-mode SSE subscribers. Read tools (which an
// agent calls frequently) are excluded so they don't spam the bus.
var mutatingWriteTools = map[string]bool{
	"create_anchor":             true,
	"record_implementation":     true,
	"complete_item":             true,
	"update_item":               true,
	"map_criterion_test":        true,
	"record_decision":           true,
	"mark_implemented":          true,
	"claim_item":                true,
	"create_scratchpad_item":    true,
	"update_scratchpad_item":    true,
	"append_to_scratchpad_item": true,
	"move_scratchpad_item":      true,
	"ingest_findings":           true,
}

// notifyMiddleware fires the change notification after a mutating write tool
// returns successfully. Wrapping at this single chokepoint keeps the ~13 tool
// handlers free of notification calls and guarantees none is forgotten.
func (t *Tools) notifyMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := next(ctx, req)
		if err == nil && res != nil && !res.IsError && mutatingWriteTools[req.Params.Name] {
			t.touch()
		}
		return res, err
	}
}

// Register attaches every v1 tool to the given MCP server. Idempotent in
// the sense that mcp-go errors loudly on duplicate registration; the
// caller should call Register exactly once per server.
func (t *Tools) Register(srv *server.MCPServer, includeWrites bool) {
	// Read tools are ALWAYS available — "MCP read free" is the funnel hook,
	// so a free/unlicensed install can still wire an agent to pull intent.
	t.registerListProjects(srv)
	t.registerListScratchpads(srv)
	t.registerListScratchpadItems(srv)
	t.registerListTodos(srv)
	t.registerListBugs(srv)
	t.registerListKB(srv)
	t.registerListUseCases(srv)
	t.registerReadItem(srv)
	t.registerGetProjectBrain(srv)
	t.registerGetCommitChanges(srv)
	// Write tools are PAID: registered only when licensed (commercial
	// build), physically stripped from the open-source build (the oss
	// registerWriteTools is a no-op — see tools_write_oss.go), and gated by
	// the API token at call time over HTTP (auth.go).
	if includeWrites {
		t.registerWriteTools(srv)
	}
}

// NewServer is a convenience wrapper that builds an *MCPServer with our
// tools registered. enqueuer can be nil — create_scratchpad_item is
// still registered but newly created items won't be enqueued for
// classification until `codedistill serve` picks them up. blobs can be
// nil — read_item will simply omit blob_url for binary items, which
// is the correct behavior for stdio MCP where the LLM client has no
// HTTP path to the API server anyway.
// verifier can be nil — record_implementation then just skips the auto-run of
// the deterministic verification layer (the correct behavior for stdio MCP,
// which has no long-lived process to host the async run).
// notify can be nil (stdio MCP): writes then publish nothing, which is correct
// since there are no SSE subscribers. In serve mode it's wired to
// bus.Publish(events.ItemsChanged) so MCP-driven item changes (e.g. an LLM
// closing an item) refresh the SPA immediately instead of relying on its
// disconnected-only fallback poll.
func NewServer(store storage.Storage, version string, enqueuer Enqueuer, verifier Verifier, blobs blobstore.BlobStore, blobURLTTL time.Duration, includeWrites bool, notify func()) *server.MCPServer {
	tools := New(store).WithEnqueuer(enqueuer).WithVerifier(verifier).WithBlobs(blobs, blobURLTTL).WithNotifier(notify)
	opts := []server.ServerOption{server.WithToolCapabilities(true)}
	if notify != nil {
		opts = append(opts, server.WithToolHandlerMiddleware(tools.notifyMiddleware))
	}
	srv := server.NewMCPServer(ServerName, version, opts...)
	tools.Register(srv, includeWrites)
	return srv
}

// resolveProject looks up a project by name. When workspaceName is
// empty, falls back to the single-workspace default (errors clearly if
// multiple workspaces exist). Used by list_* tools so the agent can
// address projects by human name rather than opaque id.
//
// All error messages are caller-actionable: the agent should be able to
// recover by listing workspaces / projects after a miss.
func (t *Tools) resolveProject(ctx context.Context, workspaceName, projectName string) (*domain.Project, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project_name is required")
	}
	var ws *domain.Workspace
	if workspaceName == "" {
		w, err := t.resolveDefaultWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		ws = w
	} else {
		w, err := t.Store.GetWorkspaceByName(ctx, workspaceName)
		if err != nil {
			return nil, fmt.Errorf("workspace %q: %w", workspaceName, err)
		}
		ws = w
	}
	p, err := t.Store.GetProjectByName(ctx, ws.ID, projectName)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// resolveScratchpad chains resolveProject + GetScratchpadByName. Used by
// list_scratchpad_items so the agent can address a pad by
// (workspace_name?, project_name, scratchpad_name) without first
// listing pads to find an id.
func (t *Tools) resolveScratchpad(ctx context.Context, workspaceName, projectName, scratchpadName string) (*domain.Scratchpad, error) {
	if scratchpadName == "" {
		return nil, fmt.Errorf("scratchpad_name is required")
	}
	p, err := t.resolveProject(ctx, workspaceName, projectName)
	if err != nil {
		return nil, err
	}
	sp, err := t.Store.GetScratchpadByName(ctx, p.ID, scratchpadName)
	if err != nil {
		return nil, err
	}
	return sp, nil
}
