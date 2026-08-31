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

// Package storage defines the Storage_Layer interface (Req 11).
// Business logic depends on this interface; implementations (sqlite, later postgres)
// live in subpackages.
package storage

import (
	"context"
	"errors"
	"time"

	"codedistill/internal/domain"
)

// ErrNotFound is returned by Get* and Delete* methods when the target row is missing.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned by Create* / Update* methods when a UNIQUE
// constraint violation occurs (today: workspace name, project name within
// a workspace, scratchpad name within a project, codebase name within a
// project — all per migration 0012). Backends translate driver-specific
// constraint errors to this sentinel so the API layer can map cleanly to
// a 409 Conflict response without parsing error strings.
var ErrDuplicate = errors.New("name already in use")

// ErrConflict is returned by Claim* methods when an item is already
// claimed by a different actor. The caller should look up the
// existing claimer (it's persisted in claimed_by) to compose the
// 409-style "already claimed by X" response.
var ErrConflict = errors.New("conflict")

// EmbeddableTable is the whitelist of tables that carry an embedding +
// embedded_at column pair (see migration 0014). Used by UpdateEmbedding
// and ListUnembedded so the table identifier never reaches a SQL string
// from untrusted input.
type EmbeddableTable string

const (
	TableScratchpadItems  EmbeddableTable = "scratchpad_items"
	TableTodoItems        EmbeddableTable = "todo_items"
	TableBugItems         EmbeddableTable = "bug_items"
	TableKnowledgeEntries EmbeddableTable = "knowledge_entries"
	TableUseCaseItems     EmbeddableTable = "use_case_items"
)

// EmbeddingTarget pairs a row id with the canonical text the embedder
// should consume for that type. ListUnembedded fills both so the caller
// can iterate without re-fetching full rows. Per-table text composition:
//
//	scratchpad_items: content
//	todo_items:       subject + "\n\n" + notes
//	bug_items:        subject + "\n\n" + notes
//	knowledge_entries:title + "\n\n" + content
//	use_case_items:   subject + "\n\n" + description
type EmbeddingTarget struct {
	ID   string
	Text string
}

// SearchCandidate is one project-scoped row with its raw embedding blob
// and enough surface metadata to render in a results list. The caller
// (search endpoint) decodes the blob, computes cosine vs. the query
// vector, sorts descending, and returns the top N. Kind matches
// EmbeddableTable values so the SPA can route clicks per type.
//
// ScratchpadID + ScratchpadName carry the originating pad: required
// for scratchpad_item kind (so the SPA can switch pads before
// highlighting the card) and best-effort for derived items via their
// source_item_id (left-joined; empty when the source was deleted or
// the item was created manually with no source). Title is the best
// short label per type (subject / title / first line of content);
// Snippet is a longer excerpt for the result row body.
type SearchCandidate struct {
	Kind           EmbeddableTable
	ID             string
	Title          string
	Snippet        string
	ScratchpadID   string
	ScratchpadName string
	Embedding      []byte
}

// ItemGridPosition is the payload for RestackScratchpadItems: the
// target item id plus its new (col, row). GridW is optional — zero
// leaves the existing width untouched (the default restack case);
// non-zero updates it in the same atomic write (used by the
// column-layout restack modes). GridH likewise — the fit-to-content
// restack mode sets height FROM the content, which is the one case
// where rewriting height during a layout pass is semantically right.
type ItemGridPosition struct {
	ID      string
	GridCol int
	GridRow int
	GridW   int // 0 = leave existing width unchanged
	GridH   int // 0 = leave existing height unchanged
}

type Storage interface {
	// Lifecycle
	Migrate(ctx context.Context) error
	Close() error

	// Workspaces (multi-tenant root; single-user installs have one "local")
	CreateWorkspace(ctx context.Context, w *domain.Workspace) error
	GetWorkspace(ctx context.Context, id string) (*domain.Workspace, error)
	// GetWorkspaceByName resolves the human-meaningful unique name (enforced
	// by migration 0012's UNIQUE index on workspaces.name). Returns
	// ErrNotFound if no workspace has that name.
	GetWorkspaceByName(ctx context.Context, name string) (*domain.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*domain.Workspace, error)
	UpdateWorkspace(ctx context.Context, w *domain.Workspace) error
	DeleteWorkspace(ctx context.Context, id string) error

	// Users
	CreateUser(ctx context.Context, u *domain.User) error
	GetUser(ctx context.Context, id string) (*domain.User, error)
	GetUserByExternal(ctx context.Context, provider, externalID string) (*domain.User, error)
	UpdateUser(ctx context.Context, u *domain.User) error
	DeleteUser(ctx context.Context, id string) error

	// Members (workspace + project)
	AddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember) error
	// TryAddWorkspaceMember / TryActivateWorkspaceMember are the seat-guarded
	// writes (count-in-the-write; audit M18). seats must be > 0.
	TryAddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember, seats int) (bool, error)
	TryActivateWorkspaceMember(ctx context.Context, workspaceID, userID string, seats int) (bool, error)
	RemoveWorkspaceMember(ctx context.Context, workspaceID, userID string) error
	ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]*domain.WorkspaceMember, error)
	GetWorkspaceMember(ctx context.Context, workspaceID, userID string) (*domain.WorkspaceMember, error)
	SetWorkspaceMemberStatus(ctx context.Context, workspaceID, userID, status string) error
	SetWorkspaceMemberRole(ctx context.Context, workspaceID, userID, role string) error
	CountActiveWorkspaceMembers(ctx context.Context, workspaceID string) (int, error)

	AddProjectMember(ctx context.Context, m *domain.ProjectMember) error
	RemoveProjectMember(ctx context.Context, projectID, userID string) error
	ListProjectMembers(ctx context.Context, projectID string) ([]*domain.ProjectMember, error)

	// Codebases (per-project; replaces the old single repo_root field)
	CreateCodebase(ctx context.Context, cb *domain.Codebase) error
	GetCodebase(ctx context.Context, id string) (*domain.Codebase, error)
	ListCodebases(ctx context.Context, projectID string) ([]*domain.Codebase, error)
	UpdateCodebase(ctx context.Context, cb *domain.Codebase) error
	DeleteCodebase(ctx context.Context, id string) error

	// Projects
	CreateProject(ctx context.Context, p *domain.Project) error
	GetProject(ctx context.Context, id string) (*domain.Project, error)
	// GetProjectByName resolves the human-meaningful name within a
	// workspace (UNIQUE(workspace_id, name) since migration 0012). Returns
	// ErrNotFound if no project in the workspace has that name.
	GetProjectByName(ctx context.Context, workspaceID, name string) (*domain.Project, error)
	ListProjects(ctx context.Context) ([]*domain.Project, error)
	UpdateProject(ctx context.Context, p *domain.Project) error
	DeleteProject(ctx context.Context, id string) error
	// GlobalStats aggregates cross-project usage counts (UC-64 Stats modal).
	GlobalStats(ctx context.Context) (*domain.GlobalStats, error)

	// Scratchpads
	CreateScratchpad(ctx context.Context, s *domain.Scratchpad) error
	GetScratchpad(ctx context.Context, id string) (*domain.Scratchpad, error)
	// GetScratchpadByName resolves the human-meaningful name within a
	// project (UNIQUE(project_id, name) since migration 0012). Returns
	// ErrNotFound if no scratchpad in the project has that name.
	GetScratchpadByName(ctx context.Context, projectID, name string) (*domain.Scratchpad, error)
	ListScratchpads(ctx context.Context, projectID string) ([]*domain.Scratchpad, error)
	UpdateScratchpad(ctx context.Context, s *domain.Scratchpad) error
	DeleteScratchpad(ctx context.Context, id string) error

	// ScratchpadItems
	CreateScratchpadItem(ctx context.Context, i *domain.ScratchpadItem) error
	GetScratchpadItem(ctx context.Context, id string) (*domain.ScratchpadItem, error)
	ListScratchpadItems(ctx context.Context, scratchpadID string) ([]*domain.ScratchpadItem, error)
	// ListScratchpadItemIDsByState returns IDs of items in the given
	// classification_states (oldest first) — the reconciliation sweep's input.
	ListScratchpadItemIDsByState(ctx context.Context, states ...string) ([]string, error)
	UpdateScratchpadItem(ctx context.Context, i *domain.ScratchpadItem) error
	// UpdateClassification writes ONLY the classification columns from i
	// (classification_state, skipped_reason, classification_override,
	// proposed_category, confidence, reasoning, proposed role/want/why,
	// derived_item_id, updated_at), leaving
	// content, OG metadata, grid, blobs, etc. untouched. The classifier loads
	// an item at the start of a multi-second LLM call, so a full-row write would
	// clobber the og_* fields the OG worker set concurrently (lost update).
	// Mirrors UpdateEmbedding's targeted-column approach.
	UpdateClassification(ctx context.Context, i *domain.ScratchpadItem) error
	// UpdateOGMetadata writes ONLY the OpenGraph columns from i (og_title,
	// og_description, og_image_sha, og_fetched_at, updated_at), so the OG
	// worker — which runs concurrently with the classifier — can't clobber
	// classification fields. The two column sets are disjoint.
	UpdateOGMetadata(ctx context.Context, i *domain.ScratchpadItem) error
	// SetScratchpadItemArchived sets/clears archived_at (backlog item #19).
	SetScratchpadItemArchived(ctx context.Context, id string, at *time.Time) error
	// MoveScratchpadItem changes an item's scratchpad_id without touching
	// any other field. Returns ErrNotFound for missing item ids.
	MoveScratchpadItem(ctx context.Context, itemID, dstScratchpadID string) error
	// MoveItemToProject atomically moves a scratchpad item to another
	// scratchpad AND re-homes its derived work item (todo|bug|kb|use_case)
	// to dstProjectID with a fresh per-project number, in one transaction.
	// Used by cross-project item moves so a number collision can never
	// leave a split-brain. derivedID/category may be empty (unclassified
	// item) — then only the item moves.
	MoveItemToProject(ctx context.Context, itemID, dstScratchpadID, category, derivedID, dstProjectID string) error
	DeleteScratchpadItem(ctx context.Context, id string) error
	// NextAvailableGridRow returns max(grid_row + grid_h) across all items in
	// the scratchpad (or 0 if empty). Used by the API to place new items at
	// the bottom of the existing layout.
	NextAvailableGridRow(ctx context.Context, scratchpadID string) (int, error)
	// RestackScratchpadItems rewrites grid_col + grid_row for every item
	// in the given list in a single transaction. grid_w / grid_h are
	// untouched. Used by the canvas Restack action.
	RestackScratchpadItems(ctx context.Context, positions []ItemGridPosition) error

	// TodoItems
	CreateTodoItem(ctx context.Context, t *domain.TodoItem) error
	GetTodoItem(ctx context.Context, id string) (*domain.TodoItem, error)
	ListTodoItems(ctx context.Context, projectID string) ([]*domain.TodoItem, error)
	// ListTodoItemsByScratchpad filters to todos whose source item lives in the
	// given scratchpad. Manually-created and orphaned (source-deleted) todos are
	// excluded — they have no scratchpad lineage.
	ListTodoItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.TodoItem, error)
	UpdateTodoItem(ctx context.Context, t *domain.TodoItem) error
	DeleteTodoItem(ctx context.Context, id string) error
	// ClaimTodoItem flips status to in_progress and records the claimer atomically.
	// Returns ErrConflict when already claimed by a different actor; ErrNotFound for missing ids.
	ClaimTodoItem(ctx context.Context, id, claimedBy string) error

	// BugItems
	CreateBugItem(ctx context.Context, b *domain.BugItem) error
	GetBugItem(ctx context.Context, id string) (*domain.BugItem, error)
	ListBugItems(ctx context.Context, projectID string) ([]*domain.BugItem, error)
	ListBugItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.BugItem, error)
	UpdateBugItem(ctx context.Context, b *domain.BugItem) error
	DeleteBugItem(ctx context.Context, id string) error
	// ClaimBugItem — see ClaimTodoItem; bug uses the legacy 'in-progress' (hyphen) status string.
	ClaimBugItem(ctx context.Context, id, claimedBy string) error

	// KnowledgeEntries
	CreateKnowledgeEntry(ctx context.Context, k *domain.KnowledgeEntry) error
	GetKnowledgeEntry(ctx context.Context, id string) (*domain.KnowledgeEntry, error)
	ListKnowledgeEntries(ctx context.Context, projectID string) ([]*domain.KnowledgeEntry, error)
	ListKnowledgeEntriesByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.KnowledgeEntry, error)
	UpdateKnowledgeEntry(ctx context.Context, k *domain.KnowledgeEntry) error
	DeleteKnowledgeEntry(ctx context.Context, id string) error

	// UseCaseItems — persistent capabilities the product enables (the fourth
	// classifier output, in contrast to TODO/BUG/KB).
	CreateUseCaseItem(ctx context.Context, u *domain.UseCaseItem) error
	GetUseCaseItem(ctx context.Context, id string) (*domain.UseCaseItem, error)
	ListUseCaseItems(ctx context.Context, projectID string) ([]*domain.UseCaseItem, error)
	ListUseCaseItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.UseCaseItem, error)
	UpdateUseCaseItem(ctx context.Context, u *domain.UseCaseItem) error
	DeleteUseCaseItem(ctx context.Context, id string) error
	// ClaimUseCaseItem — see ClaimTodoItem.
	ClaimUseCaseItem(ctx context.Context, id, claimedBy string) error

	// ItemEvents — append-only lineage log (UC-5). RecordItemEvent appends;
	// ListItemEvents returns one owner's events oldest-first. The API merges
	// a source item's events with its derived item's for the full timeline.
	RecordItemEvent(ctx context.Context, e *domain.ItemEvent) error
	ListItemEvents(ctx context.Context, ownerType, ownerID string) ([]*domain.ItemEvent, error)
	// ReparentItemEvents moves one owner's events to a new owner. Used when a
	// reclassification re-derives the work item (slice 4), so its activity log
	// (notes + history) carries over to the new type record instead of being
	// orphaned on the retired one.
	ReparentItemEvents(ctx context.Context, oldOwnerType, oldOwnerID, newOwnerType, newOwnerID string) error
	// LatestNotes returns the most recent note-kind event per owner (the canvas
	// "pulse"). Only owner_type/owner_id/summary/created_at are populated.
	LatestNotes(ctx context.Context) ([]*domain.ItemEvent, error)
	// GetGroupNote returns a group frame's note ("" when none is stored).
	GetGroupNote(ctx context.Context, groupID string) (string, error)
	// SetGroupNote upserts a group frame's note into its dedicated store
	// (canvas rework slice C2 — moved off scratchpad_items.annotations).
	SetGroupNote(ctx context.Context, groupID, note string, updatedAt time.Time) error
	// GroupNotesByScratchpad returns group_id -> note for every group in a
	// scratchpad (used to populate group items once annotations is dropped).
	GroupNotesByScratchpad(ctx context.Context, scratchpadID string) (map[string]string, error)

	// API tokens (multi-user auth) — per-user programmatic tokens. An agent
	// presenting one acts as the owning user. Stored as the token's SHA-256.
	CreateAPIToken(ctx context.Context, t *domain.APIToken) error
	GetAPITokenUser(ctx context.Context, tokenHash string) (string, *time.Time, error)
	ListAPITokens(ctx context.Context, userID string) ([]*domain.APIToken, error)
	DeleteAPIToken(ctx context.Context, id, userID string) error
	TouchAPIToken(ctx context.Context, tokenHash string, now time.Time) error

	// Sessions (multi-user auth) — server-side login sessions keyed by the
	// SHA-256 of the cookie token. GetValid filters out expired sessions;
	// TouchSession slides an active session's expiry; DeleteExpired prunes.
	CreateSession(ctx context.Context, sess *domain.Session) error
	GetValidSession(ctx context.Context, id string, now time.Time) (*domain.Session, error)
	TouchSession(ctx context.Context, id string, now, expiresAt time.Time) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteUserSessions(ctx context.Context, userID string) error

	// Code Analysis (UC-14) — static-analyzer findings + scan runs.
	// Upsert refreshes a finding in place by (project_id, fingerprint),
	// preserving status (dismissals/pushes survive re-scans); ResolveStale
	// stamps resolved_at on open findings gone from a fresh full scan.
	UpsertCodeFinding(ctx context.Context, f *domain.CodeFinding) (bool, error)
	ResolveStaleFindings(ctx context.Context, projectID, source string, seen map[string]bool, now time.Time) (int, error)
	ListCodeFindings(ctx context.Context, projectID string) ([]*domain.CodeFinding, error)
	GetCodeFinding(ctx context.Context, id string) (*domain.CodeFinding, error)
	// HasOpenSecurityFinding backs the governance security gate: any live
	// (unresolved, not dismissed) finding with security_severity >= minScore.
	HasOpenSecurityFinding(ctx context.Context, projectID string, minScore float64) (bool, error)
	UpdateCodeFindingStatus(ctx context.Context, id, status, pushedItemID string, now time.Time) error
	CreateAnalysisScan(ctx context.Context, sc *domain.AnalysisScan) error
	LatestAnalysisScan(ctx context.Context, projectID string) (*domain.AnalysisScan, error)

	// CodeAnchors — structured pointers from any owner type to code.
	// ownerType is one of: "scratchpad_item", "todo_item", "bug_item", "knowledge_entry", "use_case_item".
	// Cascade on owner delete is handled by the Delete* methods above, not by FK.
	CreateCodeAnchor(ctx context.Context, a *domain.CodeAnchor) error
	GetCodeAnchor(ctx context.Context, id string) (*domain.CodeAnchor, error)
	ListCodeAnchors(ctx context.Context, ownerType, ownerID string) ([]*domain.CodeAnchor, error)
	// ListCodeAnchorsByPath returns every kind=file anchor in the given
	// project that points at `path`, regardless of owner type. Each result
	// carries its owner's identity + display title (and scratchpad_id for
	// scratchpad_item owners) so the code canvas can render tooltips and
	// route clicks. Used by the gutter renderer; refetch on every poll.
	ListCodeAnchorsByPath(ctx context.Context, projectID, path string) ([]*domain.CodeAnchorWithOwner, error)

	// Embeddings — written by the agent during classification and by the
	// embed-backfill CLI subcommand for pre-existing rows. Stored as a
	// raw float32 BLOB; cosine similarity happens in Go at search time.
	// See migration 0014 + internal/embed for the encoding helpers.
	UpdateEmbedding(ctx context.Context, table EmbeddableTable, id string, vector []byte, at time.Time) error
	// ListUnembedded returns up to limit rows whose embedded_at is null
	// or older than updated_at (i.e. the embedding is missing or stale
	// after a content edit). Each row's Text field is the canonical
	// embeddable text composition for that table — see EmbeddingTarget.
	ListUnembedded(ctx context.Context, table EmbeddableTable, limit int) ([]EmbeddingTarget, error)
	// GetEmbeddableText returns the canonical embeddable text for a
	// single row, using the same composition as ListUnembedded. Used by
	// the agent right after creating a derived item so the just-created
	// row gets embedded without waiting for a backfill pass.
	GetEmbeddableText(ctx context.Context, table EmbeddableTable, id string) (string, error)
	// ListSearchCandidates returns every row in the project that has an
	// embedding written, across all five item tables. Caller does the
	// cosine ranking in Go. Includes display fields so a single round-
	// trip per search query is enough to render results.
	ListSearchCandidates(ctx context.Context, projectID string) ([]SearchCandidate, error)
	// SetItemSimilarity stores a candidate-match pointer + cosine score
	// on the source item — the agent calls this after embedding when it
	// finds a likely duplicate. The SPA renders the banner from these
	// two fields.
	SetItemSimilarity(ctx context.Context, id, similarToID string, score float64) error
	// ClearItemSimilarity drops the candidate-match fields on the item.
	// Backs the "not a duplicate" dismiss action.
	ClearItemSimilarity(ctx context.Context, id string) error

	// Code chunks (migration 0016) — sliding-window slices of project
	// source files with vector embeddings. Powers the smarter auto-
	// anchor pre-filter and future code-aware Q&A. Created and refreshed
	// by the indexer (internal/codeindex).
	UpsertCodeChunk(ctx context.Context, c *domain.CodeChunk) error
	// ListCodeChunksForFile returns existing chunks for one file (used by
	// the indexer to compare against the freshly-chunked set and decide
	// what to insert / update / delete).
	ListCodeChunksForFile(ctx context.Context, projectID, filePath string) ([]*domain.CodeChunk, error)
	// DeleteCodeChunksByID is a bulk delete used after re-chunking when
	// some old (line_start, line_end) regions don't appear anymore.
	DeleteCodeChunksByID(ctx context.Context, ids []string) error
	// DeleteCodeChunksForMissingFiles drops every chunk whose file_path
	// is not in the provided keep-set. Run by the indexer after a full
	// pass to garbage-collect chunks for files removed from the repo.
	DeleteCodeChunksForMissingFiles(ctx context.Context, projectID string, keep []string) (int, error)
	// ListUnembeddedCodeChunks returns rows whose embedding is NULL.
	// Caller (indexer) batches embed calls and writes via
	// UpdateCodeChunkEmbedding.
	ListUnembeddedCodeChunks(ctx context.Context, projectID string, limit int) ([]*domain.CodeChunk, error)
	// UpdateCodeChunkEmbedding writes only the embedding + embedded_at
	// columns; mirrors UpdateEmbedding for items.
	UpdateCodeChunkEmbedding(ctx context.Context, id string, vector []byte, at time.Time) error
	// MarkCodeChunkEmbedFailed stamps a chunk so the indexer stops
	// retrying it after the embedder has permanently given up (e.g. SVG
	// or other content the tokenizer can't make sense of even after
	// halve-and-retry). Cleared by SQL when retrying.
	MarkCodeChunkEmbedFailed(ctx context.Context, id, errMsg string, at time.Time) error
	// SearchCodeChunks ranks every embedded chunk in the project against
	// the caller-supplied query vector by cosine similarity. Returns the
	// top-N hits sorted descending. The cosine math happens here so the
	// caller doesn't have to fetch every blob over the storage interface.
	SearchCodeChunks(ctx context.Context, projectID string, query []float32, minScore float32, limit int) ([]domain.CodeChunkSearchHit, error)
	// CountCodeChunks returns how many chunks exist for the project.
	// Used by the serve-loop watcher to decide when to log "running
	// initial index" vs. an incremental rerun.
	CountCodeChunks(ctx context.Context, projectID string) (int, error)
	// ListCodeAnchorsForScratchpad returns every anchor whose owner is a
	// scratchpad_item in the given scratchpad. Lets the SPA badge each
	// card with an anchor indicator without an N+1 fetch.
	ListCodeAnchorsForScratchpad(ctx context.Context, scratchpadID string) ([]*domain.CodeAnchor, error)
	UpdateCodeAnchor(ctx context.Context, a *domain.CodeAnchor) error
	DeleteCodeAnchor(ctx context.Context, id string) error
	DeleteCodeAnchorsForOwner(ctx context.Context, ownerType, ownerID string) error
	// CopyCodeAnchors copies every anchor from (srcOwnerType, srcOwnerID) to
	// (dstOwnerType, dstOwnerID) with fresh IDs and timestamps. Used by the
	// agent to propagate anchors from a Scratchpad_Item to its derived item.
	CopyCodeAnchors(ctx context.Context, srcOwnerType, srcOwnerID, dstOwnerType, dstOwnerID string, newID func() string, now time.Time) error

	// Acceptance criteria (migration 0035, glass-box Phase 2). An item's
	// "definition of done" as discrete, individually-checkable assertions,
	// addressed by the same (owner_type, owner_id) scheme as code anchors.
	//
	// ListAcceptanceCriteria returns the owner's criteria ordered by position
	// then created_at. Never nil.
	ListAcceptanceCriteria(ctx context.Context, ownerType, ownerID string) ([]*domain.AcceptanceCriterion, error)
	GetAcceptanceCriterion(ctx context.Context, id string) (*domain.AcceptanceCriterion, error)
	CreateAcceptanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion) error
	// UpdateAcceptanceCriterion writes the mutable fields (text, state,
	// verification_kind, position, satisfied_by, test_command) + updated_at.
	UpdateAcceptanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion) error
	DeleteAcceptanceCriterion(ctx context.Context, id string) error
	// NextAcceptanceCriterionPosition returns max(position)+1 for the owner so
	// a freshly appended criterion sorts last. Returns 0 for an empty list.
	NextAcceptanceCriterionPosition(ctx context.Context, ownerType, ownerID string) (int, error)

	// Verification results (migration 0036, glass-box Phase 3). Each row is one
	// check the verification portfolio ran against an item — the trust signal
	// the trust dial later consumes. Addressed by the same (owner_type,
	// owner_id) scheme as code anchors and acceptance criteria.
	//
	// ListVerificationResults returns the owner's results newest-first. Never nil.
	ListVerificationResults(ctx context.Context, ownerType, ownerID string) ([]*domain.VerificationResult, error)
	GetVerificationResult(ctx context.Context, id string) (*domain.VerificationResult, error)
	CreateVerificationResult(ctx context.Context, v *domain.VerificationResult) error
	// UpdateVerificationResult writes the mutable fields (verdict, commit_sha,
	// exit_code, summary, output, duration_ms) + updated_at so an async run
	// created as "running" can be resolved in place to its terminal verdict.
	UpdateVerificationResult(ctx context.Context, v *domain.VerificationResult) error

	// Review decisions (migration 0038, glass-box Phase 4). The human floor's
	// approve/reject on an implemented change — the closing-loop evidence the
	// trust dial weighs. Addressed by the same (owner_type, owner_id) scheme.
	CreateReviewDecision(ctx context.Context, d *domain.ReviewDecision) error
	// LatestReviewDecision returns the owner's most recent decision, or
	// ErrNotFound when none exists.
	LatestReviewDecision(ctx context.Context, ownerType, ownerID string) (*domain.ReviewDecision, error)

	// Review flags (migration 0059) — persistent "needs another look" markers
	// that route an item into the review queue (attention only, never trust
	// evidence). ActiveReviewFlags returns the uncleared flags for an item;
	// ClearReviewFlags resolves all of an item's active flags as of `at`.
	CreateReviewFlag(ctx context.Context, f *domain.ReviewFlag) error
	ActiveReviewFlags(ctx context.Context, ownerType, ownerID string) ([]*domain.ReviewFlag, error)
	ClearReviewFlags(ctx context.Context, ownerType, ownerID string, at time.Time) error

	// Architecture diagram (migration 0040, glass-box Phase 5). Nodes + edges of
	// the LLM-drafted, human-ratified conceptual structure.
	ListArchitectureNodes(ctx context.Context, projectID string) ([]*domain.ArchitectureNode, error)
	GetArchitectureNode(ctx context.Context, id string) (*domain.ArchitectureNode, error)
	CreateArchitectureNode(ctx context.Context, n *domain.ArchitectureNode) error
	UpdateArchitectureNode(ctx context.Context, n *domain.ArchitectureNode) error
	DeleteArchitectureNode(ctx context.Context, id string) error
	// SetArchitectureNodeStatus caches a node's as-built status; ArchitectureHasMissing
	// asks whether any ratified node is 'missing' (Phase 6 architecture governance).
	SetArchitectureNodeStatus(ctx context.Context, id, status string) error
	ArchitectureHasMissing(ctx context.Context, projectID string) (bool, error)
	ListArchitectureEdges(ctx context.Context, projectID string) ([]*domain.ArchitectureEdge, error)
	CreateArchitectureEdge(ctx context.Context, e *domain.ArchitectureEdge) error
	DeleteArchitectureEdge(ctx context.Context, id string) error
	// DeleteProposedArchitecture clears un-ratified nodes + edges so a re-draft
	// can replace the proposal while ratified structure survives.
	DeleteProposedArchitecture(ctx context.Context, projectID string) error
	// RatifyProposedArchitecture promotes every proposed node + edge to ratified
	// in one transaction (atomic; edges are a plain provenance UPDATE).
	RatifyProposedArchitecture(ctx context.Context, projectID string, now time.Time) error

	// Custom fields (backlog item #1): project-scoped typed field definitions +
	// polymorphic per-item values.
	ListCustomFieldDefs(ctx context.Context, projectID string) ([]*domain.CustomFieldDef, error)
	GetCustomFieldDef(ctx context.Context, id string) (*domain.CustomFieldDef, error)
	CreateCustomFieldDef(ctx context.Context, d *domain.CustomFieldDef) error
	UpdateCustomFieldDef(ctx context.Context, d *domain.CustomFieldDef) error
	DeleteCustomFieldDef(ctx context.Context, id string) error
	ListCustomFieldValues(ctx context.Context, ownerType, ownerID string) ([]*domain.CustomFieldValue, error)
	SetCustomFieldValue(ctx context.Context, v *domain.CustomFieldValue) error

	// Skills (backlog item #16): project-scoped instruction sets served over MCP.
	// Create/Update also maintain the immutable version history (a new version is
	// cut only when name/content changes).
	ListSkills(ctx context.Context, projectID string, enabledOnly bool) ([]*domain.Skill, error)
	GetSkill(ctx context.Context, id string) (*domain.Skill, error)
	CreateSkill(ctx context.Context, sk *domain.Skill) error
	UpdateSkill(ctx context.Context, sk *domain.Skill) error
	DeleteSkill(ctx context.Context, id string) error

	// Skill versioning + evidence-gated throughline tie (Pro). See
	// docs/design/skill-provenance-standard.md. In the reverse-lookup queries a
	// version of 0 means "any version" and an evidence of "" means "any evidence".
	ListSkillVersions(ctx context.Context, skillID string) ([]*domain.SkillVersion, error)
	GetSkillVersion(ctx context.Context, skillID string, version int) (*domain.SkillVersion, error)
	PurgeSkillVersion(ctx context.Context, skillID string, version int) error
	RecordSkillRetrieval(ctx context.Context, r *domain.SkillRetrieval) error
	RecordSkillApplication(ctx context.Context, a *domain.SkillApplication) error
	ListSkillApplications(ctx context.Context, skillID string, version int, evidence string) ([]*domain.SkillApplication, error)
	ListSkillRetrievals(ctx context.Context, skillID string, version int) ([]*domain.SkillRetrieval, error)

	// User-scoped settings (drawer state, theme, model selection, etc.).
	// Value is opaque JSON. Set is upsert. Get returns ErrNotFound when the
	// key has never been set for this user (caller decides on default).
	GetUserSetting(ctx context.Context, userID, key string) (*domain.UserSetting, error)
	SetUserSetting(ctx context.Context, s *domain.UserSetting) error
	DeleteUserSetting(ctx context.Context, userID, key string) error
	ListUserSettings(ctx context.Context, userID string) ([]*domain.UserSetting, error)

	// Project-scoped settings (default classification mode for new scratchpads, etc.).
	GetProjectSetting(ctx context.Context, projectID, key string) (*domain.ProjectSetting, error)
	SetProjectSetting(ctx context.Context, s *domain.ProjectSetting) error
	DeleteProjectSetting(ctx context.Context, projectID, key string) error
	ListProjectSettings(ctx context.Context, projectID string) ([]*domain.ProjectSetting, error)

	// MCP export queue (v0.8.2 outbound MCP client). Drained by the
	// mcpworker background loop. EnqueueExportItem assigns ID and
	// timestamps if zero.
	EnqueueExportItem(ctx context.Context, item *domain.ExportQueueItem) error
	// ListDueExportItems returns items whose next_attempt_at is <= before,
	// ordered by next_attempt_at ASC, capped at limit.
	ListDueExportItems(ctx context.Context, before time.Time, limit int) ([]*domain.ExportQueueItem, error)
	DeleteExportQueueItem(ctx context.Context, id string) error
	// RetryExportQueueItem updates retry state on a row that's already
	// in the queue (incrementing attempts, scheduling the next attempt,
	// recording the most recent error).
	RetryExportQueueItem(ctx context.Context, id string, attempts int, nextAt time.Time, lastError string) error

	// MarkItemSynced flips the per-item sync columns to indicate the
	// remote write succeeded. ownerType is the polymorphic discriminator
	// ("todo_item" / "bug_item" / "knowledge_entry" / "use_case_item"). If
	// remoteID is non-empty it overwrites; if empty the column is left
	// alone (so 'update' operations don't clobber the id set by 'create').
	MarkItemSynced(ctx context.Context, ownerType, ownerID, remoteID string, at time.Time) error
	// MarkItemSyncFailed records a failure (transport or server-side
	// rejection) on the per-item sync columns. The item remains visible
	// in the cache; the indicator UI surfaces the failure.
	MarkItemSyncFailed(ctx context.Context, ownerType, ownerID, errMsg string, at time.Time) error
	// MarkItemSyncPending sets sync_status='pending' and clears
	// last_sync_error. Called at enqueue time so the indicator reflects
	// "in flight" before the worker runs.
	MarkItemSyncPending(ctx context.Context, ownerType, ownerID string) error
	// GetItemSyncState reads the four sync columns for one item.
	// remoteID may be empty (never synced); status is one of
	// "local-only" | "pending" | "synced" | "failed". Returns
	// ErrNotFound when the item does not exist.
	GetItemSyncState(ctx context.Context, ownerType, ownerID string) (remoteID, status, lastError string, err error)

	// DashboardThroughput backs the project metrics dashboard Phase A
	// panel #1. Returns per-day created/completed counts for todos,
	// bugs, and use_cases over [since, now], plus the current open
	// count per type. Day buckets use SQLite's date(ts,'localtime') so
	// the calendar matches the user's wall clock; only non-zero days
	// appear in the series.
	DashboardThroughput(ctx context.Context, projectID string, since time.Time) (*domain.DashboardThroughput, error)

	// DashboardIndexHealth backs the dashboard's indexing-health panel:
	// per-table embedding coverage, code-chunk totals, anchor counts by
	// provenance, and the classify-time dedup count, all scoped to one
	// project.
	DashboardIndexHealth(ctx context.Context, projectID string) (*domain.DashboardIndexHealth, error)

	// DashboardClosedItems returns every closed/implemented todo, bug,
	// and use_case for the project with its created/closed timestamps
	// and commit SHA (empty when none was recorded). Feeds the
	// time-to-close and per-branch dashboard panels; ordering is
	// unspecified.
	DashboardClosedItems(ctx context.Context, projectID string) ([]domain.DashboardClosedItem, error)

	// ListLiveBlobShas returns the distinct set of blob_sha values
	// currently referenced by any scratchpad_item. Backs the
	// background blobstore GC sweeper (internal/blobstore) — anything
	// in the store NOT in this set after a grace TTL is eligible for
	// deletion. Returns an empty slice (never nil) when no items
	// reference blobs. Order is unspecified.
	ListLiveBlobShas(ctx context.Context) ([]string, error)

	// ListCompositeImageShas returns blob SHAs referenced inline
	// within composite content (markdown `![](/api/v1/blobs/<sha>)`).
	// Walks composite items and regex-extracts the references in Go
	// (SQLite REGEXP isn't reliably available across drivers). The
	// GC sweeper unions this set with ListLiveBlobShas so inline
	// images aren't swept as orphans. Returns an empty slice (never
	// nil) when there are no composite items.
	ListCompositeImageShas(ctx context.Context) ([]string, error)

	// CreditsSummary returns workspace-wide vanity totals for the hidden
	// "credits" modal (triple-click the version chip). All COUNT-only,
	// no joins worth caching. ItemsCreated counts scratchpad_items;
	// TodosCompleted counts todo_items in any terminal status; BugsFiled
	// counts every bug_item ever created.
	CreditsSummary(ctx context.Context) (*domain.CreditsSummary, error)
}
