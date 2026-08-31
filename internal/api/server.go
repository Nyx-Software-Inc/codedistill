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

// Package api is the HTTP layer. Request/response bodies are JSON; paths are
// versioned under /api/v1/. Handlers are methods on *Server so they share the
// storage layer, agent pipeline, and logger.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"codedistill/internal/agent"
	"codedistill/internal/blobstore"
	"codedistill/internal/events"
	"codedistill/internal/features"
	"codedistill/internal/git"
	"codedistill/internal/mcpmap"
	"codedistill/internal/mcpworker"
	"codedistill/internal/power"
	"codedistill/internal/storage"
)

// BuildInfo carries the build-time identity of the running binary (the
// version vars are package-private to main, so they're injected in here).
// Surfaced to the SPA via GET /api/v1/version.
type BuildInfo struct {
	Version   string `json:"version"`
	GitSHA    string `json:"git_sha"`
	BuildDate string `json:"build_date"`
}

type Server struct {
	store storage.Storage
	agent *agent.Agent
	build BuildInfo
	log   *slog.Logger
	// auth is the OIDC login provider (multi-user). nil = single-user mode:
	// the login routes report "not configured" and currentUser stays 'local'.
	auth *oidcAuth
	// bootstrapAdminEmail: the first login with this email becomes workspace
	// owner. allowedDomains: optional email-domain allowlist (empty = any).
	bootstrapAdminEmail string
	allowedDomains      []string
	// secureCookies forces the Secure attribute on auth/session cookies
	// regardless of r.TLS — set it when serving over HTTPS, including behind
	// a TLS-terminating proxy (where r.TLS is nil). Wired via
	// WithSecureCookies from -secure-cookies / $CODEDISTILL_SECURE_COOKIES.
	secureCookies bool
	// export is the per-write hook that enqueues outbound MCP pushes
	// (v0.8.2 #9 client). May be nil — handlers check before calling.
	export *mcpworker.Hook
	// suggester is the LLM client used by the mcp-export wizard's
	// suggest-mapping endpoint. Satisfied by *ollama.Client (any
	// GenerateJSON impl). May be nil — endpoint returns 503.
	suggester mcpmap.Suggester
	// embedder is the same Ollama embed client the agent uses, threaded
	// here so the search endpoint can embed the query at request time.
	// May be nil — search endpoint returns 503 when missing.
	embedder Embedder
	// generator is the LLM-text client used by the Q&A endpoint.
	// Satisfied by *ollama.Client's Generate method. May be nil — Q&A
	// returns 503 when missing.
	generator Generator
	// powerSource returns the current AC/Battery state for the GET
	// /api/v1/power endpoint. May be nil — endpoint returns Unknown.
	powerSource func() power.Source
	// blobs is the rich-canvas binary storage. May be nil when the
	// server runs in a mode that doesn't need blobs (e.g. tests for
	// non-blob endpoints) — blob upload/download return 503 in that
	// case. Wired in serve mode via WithBlobStore.
	blobs blobstore.BlobStore
	// blobConfig is the upload + serve policy (max size, MIME
	// whitelist, URL TTL). Zero value uses the defaults from
	// DefaultBlobConfig(). Task #13 will replace this with a
	// settings-cascade lookup; for now it's a static struct set at
	// construction.
	blobConfig BlobConfig
	// bus is the pub/sub for change notifications driving SSE
	// subscribers. May be nil — handlers publish defensively via
	// the typed nil-receiver path on *events.Bus, so callers never
	// need to check. Wired in serve mode via WithEventBus.
	bus *events.Bus
	// archJobs tracks the per-project background architecture-draft
	// enrichment jobs (async draft; one active job per project). Guarded
	// by archJobsMu; entries persist after completion so the panel can
	// show the final done/total until the next draft.
	archJobs   map[string]*archJob
	archJobsMu sync.Mutex
	// criteriaJobs tracks in-flight "Draft with AI" acceptance-criteria runs by
	// owner (type+id). The draft runs as a background job on context.Background
	// so closing the modal / reloading / closing the tab can't cancel it; the
	// set drives the "drafting…" indicator on the item card + in the modal.
	criteriaJobs   map[string]bool
	criteriaJobsMu sync.Mutex
	// Review-queue git caches (audit H15): immutable commit metrics keyed by
	// repoRoot+SHA, and the per-repo revert scan with a short TTL.
	metricsCache map[string]git.CommitMetrics
	metricsMu    sync.Mutex
	revertCache  map[string]revertCacheEntry
	revertMu     sync.Mutex
	// grouper is the paid duplicate-intelligence impl backing the
	// on-demand /dedup/scan endpoint. nil in the free build / unlicensed
	// commercial build — the route is feature-gated and the handler
	// guards defensively. Wired via WithDuplicateGrouper.
	grouper agent.DuplicateGrouper
	// verifier runs the deterministic verification layer (glass-box Phase 3)
	// behind the manual-trigger endpoint. Shares the instance the MCP server
	// uses so a manual re-run and an auto-run respect the same in-flight guard.
	// nil → the verify endpoint returns 503. Wired via WithVerifier.
	verifier Verifier
	// redeem configures in-app license redemption against the activation
	// service. nil → the redeem endpoints return 503. Wired via
	// WithLicenseRedeem (serve mode).
	redeem *redeemConfig
	// models lists installed local (Ollama) models for the reviewer-model
	// picker. nil → GET /ollama/models returns an empty list. Wired via
	// WithModelLister from ollama.Client.ListModels.
	models func(ctx context.Context) ([]string, error)
	// authToken gates write requests (see auth.go). Empty = auth disabled
	// (the -no-auth path / tests). Wired via WithAuthToken. Guarded by
	// authMu because the regenerate endpoint swaps it while request
	// handlers read it.
	authMu      sync.RWMutex
	authToken   string
	authPersist func(string) error // persists a rotated token; nil = in-memory only
}

// BlobConfig captures the runtime policy applied to blob uploads and
// downloads. Defaults come from DefaultBlobConfig(); per-installation
// overrides land via settings (task #13).
type BlobConfig struct {
	// MaxBytesPerItem is the per-upload cap. Larger uploads are
	// rejected with 413. Zero falls back to the default.
	MaxBytesPerItem int64
	// AllowedMimes is the whitelist of MIME types accepted by the
	// upload endpoint. Anything else is rejected with 415. Empty
	// falls back to the default.
	AllowedMimes []string
	// URLTTL is the presigned-URL lifetime passed to BlobStore.URL.
	// The LocalStore ignores it; the S3 impl (Slice 5) honors it.
	URLTTL time.Duration
}

// DefaultBlobConfig returns the conservative defaults shipped with
// the rich-canvas slices: 10 MiB per item, a safe-by-default MIME
// allowlist (images + common doc/archive types — no executables),
// 15-minute URL TTL.
//
// WebP is deliberately omitted to keep the dependency surface to
// stdlib (would require golang.org/x/image); a later patch can add
// it. Executable types (sh, JS, exe, msi, dmg, etc.) are deliberately
// excluded from the default — admins who need them can override via
// the blob.allowed_mimes setting at any cascade scope.
func DefaultBlobConfig() BlobConfig {
	return BlobConfig{
		MaxBytesPerItem: 10 << 20, // 10 MiB
		AllowedMimes: []string{
			// Slice 1: images
			"image/png", "image/jpeg", "image/gif", "image/webp",
			// Slice 2: common doc / data / archive types
			"application/pdf",
			"text/plain", "text/csv", "text/markdown",
			"application/json",
			"application/zip",
			// Slice 2 (v0.9.2): video. Lands as content_type='file'
			// and renders as a downloadable card. Note: the default
			// 10MiB cap means most real videos won't fit until an
			// admin raises blob.max_bytes_per_item via settings.
			"video/mp4", "video/webm",
		},
		URLTTL: 15 * time.Minute,
	}
}

func NewServer(store storage.Storage, a *agent.Agent, build BuildInfo, log *slog.Logger, export *mcpworker.Hook, suggester mcpmap.Suggester, embedder Embedder, generator Generator) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: store, agent: a, build: build, log: log, export: export, suggester: suggester, embedder: embedder, generator: generator}
}

// WithAuthToken sets the API token that gates write requests and the
// persist callback used when the token is rotated (nil = in-memory only).
// Empty token leaves auth disabled. Returns the receiver for chaining.
func (s *Server) WithAuthToken(token string, persist func(string) error) *Server {
	s.authToken = token
	s.authPersist = persist
	return s
}

// WithSecureCookies forces the Secure attribute on auth/session cookies.
// Enable it for any HTTPS deployment (including behind a TLS-terminating
// proxy, where r.TLS is nil). Default off so plain-http localhost works.
func (s *Server) WithSecureCookies(v bool) *Server {
	s.secureCookies = v
	return s
}

// cookieSecure reports whether the Secure attribute should be set on a
// cookie for this request: true when the server is configured for HTTPS
// (WithSecureCookies) or the request itself arrived over TLS.
func (s *Server) cookieSecure(r *http.Request) bool {
	return s.secureCookies || (r != nil && r.TLS != nil)
}

// TokenValue returns the CURRENT write-auth token under the lock. Exported
// because the /mcp transport and the SPA cookie wrapper must resolve the token
// per request rather than capture it at wiring time: regenerateAuthToken swaps
// s.authToken, and a captured copy goes stale the moment an admin rotates —
// leaving the revoked token working and the new one refused. Pass this method
// as the getter; never pass its result.
func (s *Server) TokenValue() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.authToken
}

func (s *Server) setToken(t string) {
	s.authMu.Lock()
	s.authToken = t
	s.authMu.Unlock()
}

// WithPowerSource attaches the function used to answer the GET
// /api/v1/power endpoint. Typically wired to power.Watcher.Current.
// Returns the receiver so callers can chain construction.
func (s *Server) WithPowerSource(src func() power.Source) *Server {
	s.powerSource = src
	return s
}

// WithBlobStore wires the rich-canvas binary storage and its policy
// config. Both arguments are required for blob endpoints to function;
// without WithBlobStore, the upload/download routes return 503.
// Pass DefaultBlobConfig() unless you specifically need to override
// the limits.
func (s *Server) WithBlobStore(bs blobstore.BlobStore, cfg BlobConfig) *Server {
	s.blobs = bs
	s.blobConfig = cfg
	return s
}

// WithEventBus wires the pub/sub bus that mutation handlers
// publish change notifications to and the SSE endpoint subscribes
// from. Without it, the events endpoint returns 503 and mutation
// handlers' publish calls are nil-safe no-ops (so we don't have
// to gate every publish with a nil check).
func (s *Server) WithEventBus(b *events.Bus) *Server {
	s.bus = b
	return s
}

// WithDuplicateGrouper wires the paid duplicate-intelligence impl that
// backs the on-demand /dedup/scan endpoint. nil leaves the endpoint
// answering 503 (and the route is feature-gated regardless).
func (s *Server) WithDuplicateGrouper(g agent.DuplicateGrouper) *Server {
	s.grouper = g
	return s
}

// WithVerifier wires the verification service that backs the manual
// re-run endpoint (glass-box Phase 3). Pass the same instance the MCP
// server uses so manual and auto runs share one in-flight guard. nil
// leaves POST .../verify answering 503.
func (s *Server) WithVerifier(v Verifier) *Server {
	s.verifier = v
	return s
}

// WithModelLister wires the installed-local-models lookup behind
// GET /api/v1/ollama/models (the reviewer-model picker). nil leaves the
// endpoint returning an empty list. Returns the receiver.
func (s *Server) WithModelLister(f func(ctx context.Context) ([]string, error)) *Server {
	s.models = f
	return s
}

// notifyExport is a thin wrapper over s.export.Notify that no-ops when
// the hook isn't installed (tests, or when the server runs without
// MCP-export wired). Keeps the per-handler call sites to one line.
func (s *Server) notifyExport(ctx context.Context, ownerType, ownerID, op string, payload any) {
	if s.export == nil {
		return
	}
	s.export.Notify(ctx, ownerType, ownerID, op, payload)
}

// exportOp picks the export operation for an item update: a status transition
// is a status_change so destinations can route it specially (the worker falls
// back to the update tool if the destination didn't map it); anything else is a
// plain update.
func exportOp(statusChanged bool) string {
	if statusChanged {
		return mcpworker.OpStatusChange
	}
	return mcpworker.OpUpdate
}

// captureRemoteID reads the remote_id of a soon-to-be-deleted item so
// the delete-op queue payload can target the right remote row. Returns
// "" silently on any error — best-effort; absent remote_id just means
// the item was never synced (or storage failed to read).
func (s *Server) captureRemoteID(ctx context.Context, ownerType, ownerID string) string {
	if s.export == nil {
		return ""
	}
	rid, _, _, _ := s.store.GetItemSyncState(ctx, ownerType, ownerID)
	return rid
}

// Handler returns an http.Handler with every endpoint registered.
// Wrap in middleware (auth, CORS, etc.) at the call site if needed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/version", s.version)
	mux.HandleFunc("GET /api/v1/license", s.licenseStatus)
	// Activation-service slice 2: redeem a purchase claim code / refresh after
	// renewal. POSTs are write-auth gated by the global middleware.
	mux.HandleFunc("POST /api/v1/license/redeem", s.redeemLicense)
	mux.HandleFunc("POST /api/v1/license/refresh", s.refreshLicense)
	mux.HandleFunc("GET /api/v1/me", s.me)
	mux.HandleFunc("PATCH /api/v1/me", s.updateMe)
	mux.HandleFunc("GET /api/v1/users/{id}", s.userIdentity)
	mux.HandleFunc("GET /api/v1/auth/token", s.getAuthToken)
	// OIDC login flow (multi-user). No-ops to 503 in single-user mode.
	mux.HandleFunc("GET /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("GET /api/v1/auth/callback", s.authCallback)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	// Per-user API tokens (agents act as the current user).
	mux.HandleFunc("POST /api/v1/tokens", s.createAPIToken)
	mux.HandleFunc("GET /api/v1/tokens", s.listAPITokens)
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", s.deleteAPIToken)
	// Admin: workspace member management (owner/admin gated in the handlers).
	mux.HandleFunc("GET /api/v1/members", s.listMembers)
	mux.HandleFunc("POST /api/v1/members/{id}/deactivate", s.deactivateMember)
	mux.HandleFunc("POST /api/v1/members/{id}/reactivate", s.reactivateMember)
	mux.HandleFunc("PUT /api/v1/members/{id}/role", s.setMemberRole)
	mux.HandleFunc("POST /api/v1/auth/token/regenerate", s.regenerateAuthToken)
	mux.HandleFunc("GET /api/v1/credits", s.credits)
	mux.HandleFunc("GET /api/v1/power", s.powerStatus)
	mux.HandleFunc("POST /api/v1/search", s.search(s.embedder))
	mux.HandleFunc("POST /api/v1/qa", s.qa(s.embedder, s.generator))

	// Export bundles — zip download streams. See internal/exportbundle.
	mux.HandleFunc("GET /api/v1/projects/{id}/export", s.exportProject)
	mux.HandleFunc("GET /api/v1/scratchpads/{id}/export", s.exportScratchpad)
	// Import bundle — multipart upload of an exported zip. Creates a new
	// project, never merges. See internal/exportbundle.Import.
	mux.HandleFunc("POST /api/v1/import", s.importBundle)

	// Workspaces (Phase 2 multi-tenant root; single-user installs have one "local")
	mux.HandleFunc("GET /api/v1/workspaces", s.listWorkspaces)
	// Creating a second workspace is the multi-user threshold — the
	// implicit "local" workspace always exists and is never gated.
	mux.HandleFunc("POST /api/v1/workspaces", s.requireFeature(features.MultiUser, s.createWorkspace))
	mux.HandleFunc("GET /api/v1/workspaces/{id}", s.getWorkspace)
	mux.HandleFunc("PATCH /api/v1/workspaces/{id}", s.updateWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspaces/{id}", s.deleteWorkspace)

	// Projects
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	// Cross-project usage summary (UC-64 profile Stats modal).
	mux.HandleFunc("GET /api/v1/stats", s.globalStats)
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/projects/{id}", s.getProject)
	mux.HandleFunc("PATCH /api/v1/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/v1/projects/{id}/verify-defaults", s.verifyDefaults)

	// Code Analysis (UC-14). The baseline scan is FREE (the adoption hook) —
	// no feature gate. The paid analysis pipeline (Enterprise SARIF ingest +
	// auto-route) is a later surface that will gate on features.Analysis.
	mux.HandleFunc("POST /api/v1/projects/{id}/analysis/scan", s.runAnalysisScan)
	mux.HandleFunc("GET /api/v1/projects/{id}/analysis/findings", s.listFindings)
	mux.HandleFunc("GET /api/v1/projects/{id}/analysis/scan/latest", s.latestScan)
	mux.HandleFunc("POST /api/v1/analysis/findings/{id}/push", s.pushFinding)
	mux.HandleFunc("POST /api/v1/analysis/findings/{id}/dismiss", s.dismissFinding)
	// Enterprise: ingest a CI/security-pipeline SARIF document into the project
	// (the external ingest surface). Gated by the paid analysis-pipeline feature.
	mux.HandleFunc("POST /api/v1/projects/{id}/analysis/ingest", s.requireFeature(features.Analysis, s.ingestSARIF))

	// Codebases (per-project; replaces the legacy single repo_root field)
	mux.HandleFunc("GET /api/v1/projects/{pid}/codebases", s.listCodebases)
	mux.HandleFunc("POST /api/v1/projects/{pid}/codebases", s.createCodebase)
	mux.HandleFunc("GET /api/v1/codebases/{id}", s.getCodebase)
	mux.HandleFunc("PATCH /api/v1/codebases/{id}", s.updateCodebase)
	mux.HandleFunc("DELETE /api/v1/codebases/{id}", s.deleteCodebase)

	// Settings (Phase 2 Stage 3 — generic per-user / per-project key/value).
	// Single-user installs use uid="local". Cascade lookup is encoded at the
	// call site, not here.
	mux.HandleFunc("GET /api/v1/users/{uid}/settings", s.listUserSettings)
	mux.HandleFunc("GET /api/v1/users/{uid}/settings/{key}", s.getUserSetting)
	mux.HandleFunc("PUT /api/v1/users/{uid}/settings/{key}", s.putUserSetting)
	mux.HandleFunc("DELETE /api/v1/users/{uid}/settings/{key}", s.deleteUserSetting)
	mux.HandleFunc("GET /api/v1/projects/{pid}/settings", s.listProjectSettings)
	mux.HandleFunc("GET /api/v1/projects/{pid}/settings/{key}", s.getProjectSetting)
	mux.HandleFunc("PUT /api/v1/projects/{pid}/settings/{key}", s.putProjectSetting)
	mux.HandleFunc("DELETE /api/v1/projects/{pid}/settings/{key}", s.deleteProjectSetting)

	// Scratchpads
	mux.HandleFunc("GET /api/v1/projects/{pid}/scratchpads", s.listScratchpads)
	mux.HandleFunc("POST /api/v1/projects/{pid}/scratchpads", s.createScratchpad)
	mux.HandleFunc("GET /api/v1/scratchpads/{id}", s.getScratchpad)
	mux.HandleFunc("PATCH /api/v1/scratchpads/{id}", s.updateScratchpad)
	mux.HandleFunc("DELETE /api/v1/scratchpads/{id}", s.deleteScratchpad)
	mux.HandleFunc("POST /api/v1/scratchpads/{id}/restack", s.restackScratchpad)

	// Scratchpad Items
	mux.HandleFunc("GET /api/v1/scratchpads/{sid}/items", s.listItems)
	mux.HandleFunc("POST /api/v1/scratchpads/{sid}/items", s.createItem)
	// Summarize a web page into a scratchpad item (backlog item #6): fetch +
	// extract text + LLM summary + source link. Write (creates an item).
	mux.HandleFunc("POST /api/v1/scratchpads/{sid}/summarize-url", s.summarizeURL)

	// Scratchpad-scoped derived lists (JOIN on source_item_id — manually-created
	// rows and orphans are excluded).
	mux.HandleFunc("GET /api/v1/scratchpads/{sid}/todos", s.listTodosByScratchpad)
	mux.HandleFunc("GET /api/v1/scratchpads/{sid}/bugs", s.listBugsByScratchpad)
	mux.HandleFunc("GET /api/v1/scratchpads/{sid}/kb", s.listKBByScratchpad)
	mux.HandleFunc("GET /api/v1/scratchpads/{sid}/use-cases", s.listUseCasesByScratchpad)
	mux.HandleFunc("GET /api/v1/items/{id}", s.getItem)
	mux.HandleFunc("PATCH /api/v1/items/{id}", s.updateItem)
	mux.HandleFunc("DELETE /api/v1/items/{id}", s.deleteItem)
	mux.HandleFunc("POST /api/v1/items/{id}/move", s.moveItem)
	// Archive / un-archive a scratchpad item (backlog item #19). Write-auth.
	mux.HandleFunc("POST /api/v1/items/{id}/archive", s.archiveItem)
	mux.HandleFunc("POST /api/v1/items/{id}/unarchive", s.unarchiveItem)
	mux.HandleFunc("GET /api/v1/items/{id}/lineage", s.itemLineage)
	mux.HandleFunc("POST /api/v1/items/{id}/dismiss-similarity", s.dismissSimilarity)
	// Accept a "possibly related" banner: group the item with its match.
	mux.HandleFunc("POST /api/v1/items/{id}/group-similar", s.groupSimilar)

	// Inbox (actions on Scratchpad_Items in pending-review state)
	mux.HandleFunc("GET /api/v1/inbox", s.listInbox)
	mux.HandleFunc("POST /api/v1/items/{id}/accept", s.acceptInbox)
	// Re-enqueue a stuck capture for classification (unprocessed/failed/orphaned).
	mux.HandleFunc("POST /api/v1/items/{id}/reprocess", s.reprocessItem)
	mux.HandleFunc("POST /api/v1/items/{id}/reclassify", s.reclassifyInbox)
	mux.HandleFunc("POST /api/v1/items/{id}/reject", s.rejectInbox)

	// Todos
	mux.HandleFunc("GET /api/v1/projects/{pid}/todos", s.listTodos)
	mux.HandleFunc("POST /api/v1/projects/{pid}/todos", s.createTodo)
	mux.HandleFunc("GET /api/v1/todos/{id}", s.getTodo)
	mux.HandleFunc("PATCH /api/v1/todos/{id}", s.updateTodo)
	mux.HandleFunc("DELETE /api/v1/todos/{id}", s.deleteTodo)
	mux.HandleFunc("POST /api/v1/todos/{id}/claim", s.claimTodo)
	mux.HandleFunc("POST /api/v1/todos/{id}/reopen", s.reopenTodo)

	// Bugs
	mux.HandleFunc("GET /api/v1/projects/{pid}/bugs", s.listBugs)
	mux.HandleFunc("POST /api/v1/projects/{pid}/bugs", s.createBug)
	mux.HandleFunc("GET /api/v1/bugs/{id}", s.getBug)
	mux.HandleFunc("PATCH /api/v1/bugs/{id}", s.updateBug)
	mux.HandleFunc("DELETE /api/v1/bugs/{id}", s.deleteBug)
	mux.HandleFunc("POST /api/v1/bugs/{id}/claim", s.claimBug)
	mux.HandleFunc("POST /api/v1/bugs/{id}/reopen", s.reopenBug)

	// Knowledge Base
	mux.HandleFunc("GET /api/v1/projects/{pid}/kb", s.listKB)
	mux.HandleFunc("POST /api/v1/projects/{pid}/kb", s.createKB)
	mux.HandleFunc("GET /api/v1/kb/{id}", s.getKB)
	mux.HandleFunc("PATCH /api/v1/kb/{id}", s.updateKB)
	mux.HandleFunc("DELETE /api/v1/kb/{id}", s.deleteKB)
	mux.HandleFunc("POST /api/v1/kb/{id}/reopen", s.reopenKB)

	// Use Cases — agent-derived only today (no POST create endpoint;
	// use cases originate from classified scratchpad items).
	mux.HandleFunc("GET /api/v1/projects/{pid}/use-cases", s.listUseCases)
	mux.HandleFunc("GET /api/v1/use-cases/{id}", s.getUseCase)
	mux.HandleFunc("PATCH /api/v1/use-cases/{id}", s.updateUseCase)
	mux.HandleFunc("DELETE /api/v1/use-cases/{id}", s.deleteUseCase)
	mux.HandleFunc("POST /api/v1/use-cases/{id}/claim", s.claimUseCase)
	mux.HandleFunc("POST /api/v1/use-cases/{id}/reopen", s.reopenUseCase)

	// Files (project git worktree surface — gated on Project.RepoRoot).
	mux.HandleFunc("GET /api/v1/projects/{pid}/files", s.listFiles)
	mux.HandleFunc("GET /api/v1/projects/{pid}/files/content", s.getFileContent)
	mux.HandleFunc("GET /api/v1/projects/{pid}/files/commits", s.listFileCommits)
	mux.HandleFunc("GET /api/v1/projects/{pid}/files/mtime", s.getFileMtime)
	mux.HandleFunc("GET /api/v1/projects/{pid}/code-anchors", s.listProjectCodeAnchorsByPath)
	mux.HandleFunc("GET /api/v1/scratchpads/{id}/code-anchors", s.listScratchpadCodeAnchors)

	// Code Anchors (per-owner nested list/create + flat patch/delete).
	// Anchor creation is paid (features.CodeAnchors); listing existing
	// anchors stays open — reads are never license-gated.
	mux.HandleFunc("GET /api/v1/items/{id}/code-anchors", s.listCodeAnchorsForOwner(ownerScratchpadItem))
	mux.HandleFunc("POST /api/v1/items/{id}/code-anchors", s.requireFeature(features.CodeAnchors, s.createCodeAnchorForOwner(ownerScratchpadItem)))
	mux.HandleFunc("GET /api/v1/todos/{id}/code-anchors", s.listCodeAnchorsForOwner(ownerTodoItem))
	mux.HandleFunc("POST /api/v1/todos/{id}/code-anchors", s.requireFeature(features.CodeAnchors, s.createCodeAnchorForOwner(ownerTodoItem)))
	mux.HandleFunc("GET /api/v1/bugs/{id}/code-anchors", s.listCodeAnchorsForOwner(ownerBugItem))
	mux.HandleFunc("POST /api/v1/bugs/{id}/code-anchors", s.requireFeature(features.CodeAnchors, s.createCodeAnchorForOwner(ownerBugItem)))
	mux.HandleFunc("GET /api/v1/kb/{id}/code-anchors", s.listCodeAnchorsForOwner(ownerKnowledgeEntry))
	mux.HandleFunc("POST /api/v1/kb/{id}/code-anchors", s.requireFeature(features.CodeAnchors, s.createCodeAnchorForOwner(ownerKnowledgeEntry)))
	mux.HandleFunc("GET /api/v1/use-cases/{id}/code-anchors", s.listCodeAnchorsForOwner(ownerUseCaseItem))
	mux.HandleFunc("POST /api/v1/use-cases/{id}/code-anchors", s.requireFeature(features.CodeAnchors, s.createCodeAnchorForOwner(ownerUseCaseItem)))
	// Gated alongside the ten creation routes above: listing anchors is a free
	// read, but MUTATING one is the paid authoring capability. Ungated, two
	// calls got a free user a hand-authored anchor — GET an item's anchors for
	// an id, then PATCH path/lines/label/provenance (only kind is immutable).
	// No free internal path is affected: url_detect's refresh and the four
	// owner-delete cascades call the store directly, not these routes.
	mux.HandleFunc("PATCH /api/v1/code-anchors/{id}", s.requireFeature(features.CodeAnchors, s.updateCodeAnchor))
	mux.HandleFunc("DELETE /api/v1/code-anchors/{id}", s.requireFeature(features.CodeAnchors, s.deleteCodeAnchor))

	// Code metrics (UC-100): per-item churn + change-complexity from the
	// item's commit anchors. Free read — no feature gate.
	mux.HandleFunc("GET /api/v1/items/{id}/code-metrics", s.codeMetricsForOwner(ownerScratchpadItem))
	mux.HandleFunc("GET /api/v1/todos/{id}/code-metrics", s.codeMetricsForOwner(ownerTodoItem))
	mux.HandleFunc("GET /api/v1/bugs/{id}/code-metrics", s.codeMetricsForOwner(ownerBugItem))
	mux.HandleFunc("GET /api/v1/kb/{id}/code-metrics", s.codeMetricsForOwner(ownerKnowledgeEntry))
	mux.HandleFunc("GET /api/v1/use-cases/{id}/code-metrics", s.codeMetricsForOwner(ownerUseCaseItem))

	// Acceptance criteria (glass-box Phase 2): an item's measurable definition
	// of done. Reads free; writes through the normal write-auth (no feature
	// gate — manual authoring is core).
	mux.HandleFunc("GET /api/v1/todos/{id}/acceptance-criteria", s.listAcceptanceCriteriaForOwner(ownerTodoItem))
	mux.HandleFunc("POST /api/v1/todos/{id}/acceptance-criteria", s.createAcceptanceCriterionForOwner(ownerTodoItem))
	mux.HandleFunc("GET /api/v1/bugs/{id}/acceptance-criteria", s.listAcceptanceCriteriaForOwner(ownerBugItem))
	mux.HandleFunc("POST /api/v1/bugs/{id}/acceptance-criteria", s.createAcceptanceCriterionForOwner(ownerBugItem))
	mux.HandleFunc("GET /api/v1/use-cases/{id}/acceptance-criteria", s.listAcceptanceCriteriaForOwner(ownerUseCaseItem))
	mux.HandleFunc("POST /api/v1/use-cases/{id}/acceptance-criteria", s.createAcceptanceCriterionForOwner(ownerUseCaseItem))
	mux.HandleFunc("POST /api/v1/todos/{id}/acceptance-criteria/draft", s.draftAcceptanceCriteriaForOwner(ownerTodoItem))
	mux.HandleFunc("POST /api/v1/bugs/{id}/acceptance-criteria/draft", s.draftAcceptanceCriteriaForOwner(ownerBugItem))
	mux.HandleFunc("POST /api/v1/use-cases/{id}/acceptance-criteria/draft", s.draftAcceptanceCriteriaForOwner(ownerUseCaseItem))
	// Which items are mid-draft (background "Draft with AI"), for the card + modal indicator.
	mux.HandleFunc("GET /api/v1/acceptance-criteria/drafting", s.listCriteriaDrafting)
	mux.HandleFunc("PATCH /api/v1/acceptance-criteria/{id}", s.updateAcceptanceCriterion)
	mux.HandleFunc("DELETE /api/v1/acceptance-criteria/{id}", s.deleteAcceptanceCriterion)

	// Verification (glass-box Phase 3): the deterministic layer's results +
	// manual re-run. GET is a free read; POST .../verify re-runs the project's
	// configured check at the item's latest recorded commit (write-auth, no
	// feature gate). The auto-run is wired into MCP record_implementation.
	mux.HandleFunc("GET /api/v1/todos/{id}/verification", s.verificationForOwner(ownerTodoItem))
	mux.HandleFunc("POST /api/v1/todos/{id}/verify", s.triggerVerificationForOwner(ownerTodoItem))
	mux.HandleFunc("GET /api/v1/bugs/{id}/verification", s.verificationForOwner(ownerBugItem))
	mux.HandleFunc("POST /api/v1/bugs/{id}/verify", s.triggerVerificationForOwner(ownerBugItem))
	mux.HandleFunc("GET /api/v1/use-cases/{id}/verification", s.verificationForOwner(ownerUseCaseItem))
	mux.HandleFunc("POST /api/v1/use-cases/{id}/verify", s.triggerVerificationForOwner(ownerUseCaseItem))
	// Installed local models for the reviewer-model picker (Settings → Verification).
	mux.HandleFunc("GET /api/v1/ollama/models", s.ollamaModels)
	// Review queue (glass-box Phase 4): implemented items ranked by risk. Free read.
	mux.HandleFunc("GET /api/v1/projects/{id}/review-queue", s.reviewQueue)
	// Drift detection (glass-box Phase 5): untraced code / unbuilt intent / diverged. Free read.
	mux.HandleFunc("GET /api/v1/projects/{id}/drift", s.drift)
	// Governance (glass-box Phase 6): the enforcement-level policy view. Free read;
	// the levels are stored as project settings (governance.*) and clamped to
	// advisory unless the paid Governance feature is licensed.
	mux.HandleFunc("GET /api/v1/projects/{id}/governance", s.getGovernance)

	// Skills (backlog item #16): project-scoped instruction sets served over MCP.
	// Paid — writes gated on the mcp feature (Skills are an MCP capability).
	mux.HandleFunc("GET /api/v1/projects/{id}/skills", s.listSkills)
	mux.HandleFunc("POST /api/v1/projects/{id}/skills", s.requireFeature(features.MCPServer, s.createSkill))
	mux.HandleFunc("PATCH /api/v1/skills/{id}", s.requireFeature(features.MCPServer, s.updateSkill))
	mux.HandleFunc("DELETE /api/v1/skills/{id}", s.requireFeature(features.MCPServer, s.deleteSkill))
	// Skill versioning + evidence-gated throughline tie (Pro). Reads open (like
	// listSkills); the compliance purge is gated with the other skill writes.
	mux.HandleFunc("GET /api/v1/skills/{id}/versions", s.listSkillVersions)
	mux.HandleFunc("GET /api/v1/skills/{id}/versions/{version}", s.getSkillVersion)
	mux.HandleFunc("GET /api/v1/skills/{id}/applications", s.listSkillApplications)
	mux.HandleFunc("GET /api/v1/skills/{id}/retrievals", s.listSkillRetrievals)
	mux.HandleFunc("DELETE /api/v1/skills/{id}/versions/{version}", s.requireFeature(features.MCPServer, s.purgeSkillVersion))
	// Pass 2: remediate the changes made under a flawed skill version (flag for
	// review and/or re-verify). Pro-gated with the other skill writes.
	mux.HandleFunc("POST /api/v1/skills/{id}/versions/{version}/remediate", s.requireFeature(features.MCPServer, s.remediateSkillVersion))

	// Custom fields (backlog item #1): project-scoped typed field definitions
	// (free read; writes gated) + polymorphic per-item values.
	mux.HandleFunc("GET /api/v1/projects/{id}/custom-fields", s.listCustomFieldDefs)
	mux.HandleFunc("POST /api/v1/projects/{id}/custom-fields", s.createCustomFieldDef)
	mux.HandleFunc("PATCH /api/v1/custom-fields/{id}", s.updateCustomFieldDef)
	mux.HandleFunc("DELETE /api/v1/custom-fields/{id}", s.deleteCustomFieldDef)
	for _, oc := range []struct {
		prefix, owner string
	}{
		{"todos", ownerTodoItem}, {"bugs", ownerBugItem},
		{"use-cases", ownerUseCaseItem}, {"kb", ownerKnowledgeEntry},
	} {
		owner := oc.owner
		mux.HandleFunc("GET /api/v1/"+oc.prefix+"/{id}/custom-fields", func(w http.ResponseWriter, r *http.Request) {
			s.listItemCustomFields(w, r, owner)
		})
		mux.HandleFunc("PUT /api/v1/"+oc.prefix+"/{id}/custom-fields/{field}", func(w http.ResponseWriter, r *http.Request) {
			s.setItemCustomField(w, r, owner)
		})
	}
	// Architecture diagram (glass-box Phase 5): LLM-drafted, human-ratified structure.
	mux.HandleFunc("GET /api/v1/projects/{id}/architecture", s.listArchitecture)
	mux.HandleFunc("POST /api/v1/projects/{id}/architecture/draft", s.draftArchitecture)
	mux.HandleFunc("POST /api/v1/projects/{id}/architecture/ratify", s.ratifyArchitecture)
	mux.HandleFunc("POST /api/v1/projects/{id}/architecture/draft/cancel", s.cancelArchDraft)
	mux.HandleFunc("PATCH /api/v1/architecture/nodes/{id}", s.updateArchNode)
	mux.HandleFunc("DELETE /api/v1/architecture/nodes/{id}", s.deleteArchNode)
	// Data-model / ER diagram (UC-45): deterministic, on-demand from the repo's
	// SQL schema (free read; complements the architecture diagram).
	mux.HandleFunc("GET /api/v1/projects/{id}/datamodel", s.getDataModel)
	// Activity log (item-editing redesign): per-work-item timeline (free read) +
	// append a markdown note (write-gated). Notes + system events share the stream.
	for _, lc := range []struct{ prefix, owner string }{
		{"todos", ownerTodoItem}, {"bugs", ownerBugItem},
		{"use-cases", ownerUseCaseItem}, {"kb", ownerKnowledgeEntry},
	} {
		owner := lc.owner
		mux.HandleFunc("GET /api/v1/"+lc.prefix+"/{id}/log", func(w http.ResponseWriter, r *http.Request) {
			s.listActivityLog(w, r, owner)
		})
		mux.HandleFunc("POST /api/v1/"+lc.prefix+"/{id}/notes", func(w http.ResponseWriter, r *http.Request) {
			s.appendNote(w, r, owner)
		})
	}
	// Canvas "pulse": latest note per owner (free read; the client maps it to cards).
	mux.HandleFunc("GET /api/v1/latest-notes", s.getLatestNotes)
	// Human-floor approve/reject on an item's implementation (slice 4.3, write-auth).
	mux.HandleFunc("POST /api/v1/todos/{id}/review", s.recordReviewForOwner(ownerTodoItem))
	mux.HandleFunc("POST /api/v1/bugs/{id}/review", s.recordReviewForOwner(ownerBugItem))
	mux.HandleFunc("POST /api/v1/use-cases/{id}/review", s.recordReviewForOwner(ownerUseCaseItem))
	// Latest recorded decision for an item (read) — lets the sign-off bar show a
	// persisted "signed off" state instead of re-prompting on every mount.
	mux.HandleFunc("GET /api/v1/todos/{id}/review", s.latestReviewForOwner(ownerTodoItem))
	mux.HandleFunc("GET /api/v1/bugs/{id}/review", s.latestReviewForOwner(ownerBugItem))
	mux.HandleFunc("GET /api/v1/use-cases/{id}/review", s.latestReviewForOwner(ownerUseCaseItem))

	// Dashboard / project metrics (Phase A panel #1). Per-day
	// created/completed counts + current open totals for todos, bugs,
	// use_cases over a rolling window (default 30 days).
	mux.HandleFunc("GET /api/v1/projects/{id}/dashboard/throughput", s.dashboardThroughput)
	mux.HandleFunc("GET /api/v1/projects/{id}/dashboard/index-health", s.dashboardIndexHealth)
	mux.HandleFunc("GET /api/v1/projects/{id}/dashboard/lifecycle", s.dashboardLifecycle)

	// Rich canvas Slice 1: binary blob upload + download. Upload
	// returns the new scratchpad_item; download serves the bytes
	// keyed by SHA-256. Both 503 when the server has no BlobStore
	// wired (mode where blobs are disabled or tests).
	mux.HandleFunc("POST /api/v1/scratchpads/{sid}/items/blob", s.uploadBlob)
	mux.HandleFunc("GET /api/v1/blobs/{sha}", s.getBlob)

	// Server-Sent Events for the SPA's real-time change feed.
	// Mutation handlers publish change events to s.bus; this
	// endpoint streams them. Replaces ~10 calls/3sec of polling
	// when connected; SPA falls back to slow polling when
	// disconnected.
	mux.HandleFunc("GET /api/v1/events", s.streamEvents)
	// Standalone blob upload — no item created. Used by SketchEditor
	// preview uploads + future composite-doc image paste flows.
	// Optional ?scratchpad_id= scopes the BlobConfig cascade.
	mux.HandleFunc("POST /api/v1/blobs", s.uploadBlobOnly)

	// MCP export wizard helpers (v0.8.2 #9 client). Per-user mcp.export
	// config itself is read/written through the generic user_settings
	// endpoints above; these two endpoints back the configure flow:
	// probe a candidate destination, and ask the LLM to suggest a
	// mapping from CodeDistill ops to that destination's tools.
	mux.HandleFunc("POST /api/v1/mcp-export/discover", s.requireFeature(features.MCPServer, s.discoverDestination))
	mux.HandleFunc("POST /api/v1/mcp-export/suggest-mapping", s.requireFeature(features.MCPServer, s.suggestMapping))
	// Local-only filesystem browse for the repo-root picker. See the
	// security note in fs_browse.go — gate off for server mode.
	mux.HandleFunc("GET /api/v1/fs/browse", s.fsBrowse)
	mux.HandleFunc("POST /api/v1/mcp-export/migrate", s.requireFeature(features.MCPServer, s.migrateMcpExport))
	// Duplicate intelligence — on-demand cluster + cross-pad report (paid).
	mux.HandleFunc("POST /api/v1/dedup/scan", s.requireFeature(features.Dedup, s.dedupScan))

	// Outer→inner: resolve identity (per-user token / session) into the request
	// context, then gate writes. A resolved identity authorizes writes; the
	// legacy shared token still works on its own for single-user.
	return s.resolveIdentity(s.requireWriteAuth(mux))
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeEmpty(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeMsg(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// statusFor maps a storage error to an appropriate HTTP status.
func statusFor(err error) int {
	if errors.Is(err, storage.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, storage.ErrDuplicate) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.build)
}

// credits backs the hidden "credits" modal — workspace-wide vanity
// totals. Triple-click the version chip in the SPA header to surface it.
func (s *Server) credits(w http.ResponseWriter, r *http.Request) {
	sum, err := s.store.CreditsSummary(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}
