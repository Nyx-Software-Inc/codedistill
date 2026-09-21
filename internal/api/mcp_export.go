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

package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"codedistill/internal/features"
	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
	"codedistill/internal/mcpworker"
)

// Endpoints backing the SPA's MCP-export configure wizard. Both are
// stateless — they don't persist anything. Persistence happens via the
// generic user_settings endpoints (PUT /api/v1/users/{uid}/settings/
// mcp.export.<short>) once the user clicks save in the wizard.
//
// Route summary:
//   - POST /api/v1/mcp-export/discover         → list_tools probe
//   - POST /api/v1/mcp-export/suggest-mapping  → LLM-suggest mapping

// discoverRequest is the body of POST /api/v1/mcp-export/discover. The
// shape mirrors mcpclient.Endpoint so the SPA can round-trip the same
// JSON it would persist after save.
type discoverRequest struct {
	Endpoint mcpclient.Endpoint `json:"endpoint"`
}

type discoverResponse struct {
	Tools []mcpclient.Tool `json:"tools"`
}

// discoverDestination connects to the candidate MCP destination,
// performs the initialize handshake, and returns its tool catalog.
// 30-second timeout caps slow remotes; user can retry. We never
// persist the credentials here — the wizard holds them in memory
// until save.
func (s *Server) discoverDestination(w http.ResponseWriter, r *http.Request) {
	var req discoverRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Endpoint.URL == "" {
		writeMsg(w, http.StatusBadRequest, "endpoint.url is required")
		return
	}
	// Cap the call so a hung remote can't pin the request goroutine.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := checkStdioPolicy(req.Endpoint); err != nil {
		writeMsg(w, http.StatusForbidden, err.Error())
		return
	}
	cli, err := mcpclient.New(ctx, req.Endpoint)
	if err != nil {
		// Connection / handshake failure — surface the message verbatim
		// so the wizard can show the user what's wrong (typo'd URL,
		// bad token, server expecting a different protocol).
		writeMsg(w, http.StatusBadGateway, err.Error())
		return
	}
	defer cli.Close()

	tools, err := cli.ListTools(ctx)
	if err != nil {
		writeMsg(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, discoverResponse{Tools: tools})
}

// suggestMappingRequest is the body of POST /api/v1/mcp-export/
// suggest-mapping.
type suggestMappingRequest struct {
	ItemType string           `json:"item_type"` // "todo" | "bug" | "kb" | "use_case"
	Catalog  []mcpclient.Tool `json:"catalog"`
}

type suggestMappingResponse struct {
	Mapping mcpmap.Mapping `json:"mapping"`
}

// suggestMapping asks the local Ollama (via the configured Suggester)
// to map CodeDistill operations to the catalog's tools. The wizard
// presents the result with per-operation override dropdowns; nothing
// is persisted until the user clicks save.
//
// Returns 503 when no LLM is configured (built without a Suggester);
// the wizard falls back to manual mapping in that case.
func (s *Server) suggestMapping(w http.ResponseWriter, r *http.Request) {
	if s.suggester == nil {
		writeMsg(w, http.StatusServiceUnavailable, "LLM mapping not available (no Ollama configured)")
		return
	}
	var req suggestMappingRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.ItemType == "" {
		writeMsg(w, http.StatusBadRequest, "item_type is required")
		return
	}
	if len(req.Catalog) == 0 {
		writeMsg(w, http.StatusBadRequest, "catalog is required (run discover first)")
		return
	}
	// Cap LLM call — Ollama 7B is fast but not instant.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	mapping, err := mcpmap.Suggest(ctx, s.suggester, req.ItemType, req.Catalog)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, suggestMappingResponse{Mapping: mapping})
}

// migrateRequest is the body of POST /api/v1/mcp-export/migrate. One
// item type per call — the wizard saves config per type, so the
// migration prompt fires per type too.
type migrateRequest struct {
	ItemType string `json:"item_type"` // "todo" | "bug" | "kb" | "use_case"
}

// migrateResponse reports what the bulk-enqueue did. Skipped counts
// items that already have sync state (pending/synced/failed) — they're
// already on the destination's radar and re-pushing create would
// duplicate them.
type migrateResponse struct {
	Enqueued int `json:"enqueued"`
	Skipped  int `json:"skipped"`
}

// shortToOwnerType is the inverse of mcpworker.OwnerTypeToShort, scoped
// to the four exportable types.
var shortToOwnerType = map[string]string{
	"todo":     "todo_item",
	"bug":      "bug_item",
	"kb":       "knowledge_entry",
	"use_case": "use_case_item",
}

// migrateMcpExport handles POST /api/v1/mcp-export/migrate: bulk-push
// of pre-existing local-only items after the user first saves an
// export config. The cache normally populates only on new writes, so
// without this, anything created before the config stays invisible to
// the destination forever.
//
// Walks every project (config is per-user, not per-project) and routes
// each local-only item through the same notify hook as a fresh create —
// items flip to 'pending' and the background worker drains the queue.
func (s *Server) migrateMcpExport(w http.ResponseWriter, r *http.Request) {
	if s.export == nil {
		writeMsg(w, http.StatusServiceUnavailable, "MCP export is not wired on this server")
		return
	}
	var req migrateRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ownerType, ok := shortToOwnerType[req.ItemType]
	if !ok {
		writeMsg(w, http.StatusBadRequest, "item_type must be one of todo | bug | kb | use_case")
		return
	}
	canExport, err := s.export.CanExport(r.Context(), req.ItemType, mcpworker.OpCreate)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !canExport {
		writeMsg(w, http.StatusConflict,
			"no destination configured for "+req.ItemType+" (or its create operation is unmapped)")
		return
	}

	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var resp migrateResponse
	push := func(id, syncStatus string, item any) {
		if syncStatus != "" && syncStatus != "local-only" {
			resp.Skipped++
			return
		}
		s.notifyExport(r.Context(), ownerType, id, mcpworker.OpCreate, item)
		resp.Enqueued++
	}
	for _, p := range projects {
		switch ownerType {
		case "todo_item":
			items, err := s.store.ListTodoItems(r.Context(), p.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, it := range items {
				push(it.ID, it.SyncStatus, it)
			}
		case "bug_item":
			items, err := s.store.ListBugItems(r.Context(), p.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, it := range items {
				push(it.ID, it.SyncStatus, it)
			}
		case "knowledge_entry":
			items, err := s.store.ListKnowledgeEntries(r.Context(), p.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, it := range items {
				push(it.ID, it.SyncStatus, it)
			}
		case "use_case_item":
			items, err := s.store.ListUseCaseItems(r.Context(), p.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, it := range items {
				push(it.ID, it.SyncStatus, it)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// stdioAllowed reports whether this install may spawn a local MCP child
// process. It may not on a multi-user server: settings are per-user and
// PUT /users/{uid}/settings/{key} is authenticated but NOT admin-gated, while
// the process runs as one service account holding DATABASE_URL and every
// tenant's blobs. Allowing it would turn "set my own preference" into code
// execution as that account.
//
// This is not a restriction users lose anything to — a stdio MCP server is a
// local child process, so on shared infrastructure you would point at an HTTP
// endpoint regardless.
func stdioAllowed() bool { return !features.Enabled(features.MultiUser) }

// checkStdioPolicy returns a non-nil error when ep would spawn a process on an
// install that must not.
func checkStdioPolicy(ep mcpclient.Endpoint) error {
	if ep.IsStdio() && !stdioAllowed() {
		return errors.New("stdio MCP destinations are single-user only — on a multi-user server the command would run as the service account, not as you. Use an http endpoint instead.")
	}
	return nil
}
