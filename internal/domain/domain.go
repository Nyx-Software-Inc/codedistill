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

// Package domain defines the core entity types for DevTrack.
// Phase 1 scope: Project, Scratchpad, ScratchpadItem, TodoItem, BugItem, KnowledgeEntry.
// Phase 2 additions (multi-user data model): Workspace, User, WorkspaceMember,
// ProjectMember, Codebase. Visibility fields on Scratchpad + derived items.
// Attributes deferred to later phases: Cross-References, Attachments, Extraction.
package domain

import (
	"encoding/json"
	"time"
)

// UserSetting is a per-user key/value preference (drawer state, theme,
// default priority, model selection, etc.). Value is opaque JSON; callers
// serialize/deserialize. Keys are free-form but should be namespaced
// (e.g. "ui.drawer.open", "agent.model") to avoid collisions.
type UserSetting struct {
	UserID    string          `json:"user_id"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ProjectSetting mirrors UserSetting but scoped to a Project. Used for
// settings that are tied to the body of work rather than the human
// (e.g. default classification mode for new scratchpads in this project).
type ProjectSetting struct {
	ProjectID string          `json:"project_id"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ExportQueueItem is one pending push from CodeDistill to a destination
// MCP server (Linear, Jira, …). Created by API hooks when an item of a
// type with a configured destination is created/updated/deleted; drained
// by the mcpworker background loop.
//
// Owner{Type,ID} is polymorphic — same pattern as CodeAnchor — and refers
// to the local item being mirrored. OwnerType uses the table-style names
// "todo_item" / "bug_item" / "knowledge_entry" / "use_case_item" so it
// matches the existing CodeAnchor convention.
//
// Op is one of "create" | "update" | "status_change" | "delete" — the
// CodeDistill operation that needs to be reflected on the destination.
// Payload is the full args object for the destination tool, in JSON
// (built at enqueue time so the worker doesn't need to re-derive it
// from the local item — the local item may have changed by the time
// the worker runs).
//
// Attempts/NextAttemptAt/LastError carry retry state; the worker uses
// exponential backoff and gives up after a configurable maximum.
type ExportQueueItem struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	OwnerType     string          `json:"owner_type"` // "todo_item" | "bug_item" | "knowledge_entry" | "use_case_item"
	OwnerID       string          `json:"owner_id"`
	Op            string          `json:"op"` // "create" | "update" | "status_change" | "delete"
	Payload       json.RawMessage `json:"payload"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	LastError     string          `json:"last_error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Workspace is the multi-tenant root and the billing/licensing boundary.
// Single-user installs use one implicit workspace with id="local"; multi-user
// installs may have many. A User belongs to one or more Workspaces via
// WorkspaceMember. A Project belongs to exactly one Workspace.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan string `json:"plan"` // "free" | "team" | "enterprise"
	// SeatLimit zero means unlimited / not enforced (free tier).
	SeatLimit int       `json:"seat_limit,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// User is an authenticated principal. In single-user mode, exactly one row
// exists with id="local" and provider="local". SSO populates ExternalID +
// Provider.
type User struct {
	ID          string `json:"id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name"`
	// CanonicalName is the IdP-provided real name, immutable by the user — the
	// disambiguator (with Email) behind an editable DisplayName.
	CanonicalName string    `json:"canonical_name,omitempty"`
	Avatar        string    `json:"avatar,omitempty"`
	ExternalID    string    `json:"external_id,omitempty"`
	Provider      string    `json:"provider,omitempty"` // "local" | "google" | "github" | "oidc"
	CreatedAt     time.Time `json:"created_at"`
}

// WorkspaceMember ties a User to a Workspace with a role.
// Member lifecycle statuses. Only Active members consume a license seat.
const (
	MemberInvited     = "invited"
	MemberActive      = "active"
	MemberDeactivated = "deactivated"
)

type WorkspaceMember struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	Role        string    `json:"role"`   // "owner" | "admin" | "member"
	Status      string    `json:"status"` // "invited" | "active" | "deactivated"
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectMember ties a User to a Project. A user can be a workspace member
// without being on every project; project_members controls per-project access.
type ProjectMember struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"` // "owner" | "admin" | "member"
	CreatedAt time.Time `json:"created_at"`
}

// Codebase is a git worktree owned by a Project. Replaces the previous
// single-repo Project.RepoRoot model — a Project may have N codebases (one
// per repo making up the product). For backwards compatibility, Project.RepoRoot
// is still populated and read for single-codebase projects; it will be dropped
// in a future migration.
type Codebase struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	RepoRoot  string    `json:"repo_root"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	// RepoRoot is the legacy single-repo path. Phase 2 introduces the Codebase
	// table as the source of truth for repo locations; RepoRoot is preserved
	// for the single-codebase case so existing API consumers keep working
	// until the frontend is updated. Will be dropped in a future migration.
	RepoRoot  string    `json:"repo_root,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Scratchpad holds free-form items. ClassificationMode controls agent behavior:
//   - "off":    agent ignores items in this scratchpad
//   - "strict": high-confidence auto-commits; low-confidence silently skipped
//   - "full":   high-confidence auto-commits; low-confidence goes to Inbox
//
// Visibility ("private" | "project") gates who in the project can see the
// scratchpad and items derived from it. Defaults to "project" so single-user
// behavior is unchanged.
type Scratchpad struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	OwnerID            string    `json:"owner_id"`
	Name               string    `json:"name"`
	ClassificationMode string    `json:"classification_mode"`
	Visibility         string    `json:"visibility"` // "private" | "project"
	CreatedAt          time.Time `json:"created_at"`
}

// ScratchpadItem is one content object within a Scratchpad.
// Classification_State values: unprocessed, processing, classified, pending-review, skipped, failed.
// Classification_Override values: "", "todo", "bug", "kb", "skip".
// Skipped_Reason values (only meaningful when state=skipped): strict_mode_low_confidence,
//
//	user_rejected_from_inbox, override_skip, derived_item_deleted.
type ScratchpadItem struct {
	ID           string `json:"id"`
	ScratchpadID string `json:"scratchpad_id"`
	// Name is the user-set label for this item. Empty by default; UI falls
	// back to the first non-blank line of Content when empty so existing
	// items keep their familiar appearance. Setting Name does NOT propagate
	// to a derived todo/bug/KB's subject (Option A — items diverge after
	// creation).
	Name                     string  `json:"name"`
	ContentType              string  `json:"content_type"` // "text", "code_snippet", "link"
	Content                  string  `json:"content"`
	ClassificationState      string  `json:"classification_state"`
	SkippedReason            string  `json:"skipped_reason,omitempty"`
	ClassificationOverride   string  `json:"classification_override,omitempty"`
	ProposedCategory         string  `json:"proposed_category,omitempty"`
	ClassificationConfidence float64 `json:"classification_confidence,omitempty"`
	ClassificationReasoning  string  `json:"classification_reasoning,omitempty"`
	// ProposedRole/Want/Why are agent-extracted shadow fields. Populated when
	// proposed_category=use_case and the source conveys a user-story shape;
	// consumed by the derive pipeline when the item is committed. Empty
	// otherwise.
	ProposedRole  string `json:"proposed_role,omitempty"`
	ProposedWant  string `json:"proposed_want,omitempty"`
	ProposedWhy   string `json:"proposed_why,omitempty"`
	DerivedItemID string `json:"derived_item_id,omitempty"`
	// Hidden removes the item from the active canvas while preserving its
	// grid geometry so unhide restores it in place.
	Hidden bool `json:"hidden"`
	// ArchivedAt marks a deliberately archived item (backlog item #19): kept in
	// full + searchable, but off the canvas. nil = live. Distinct from Hidden.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	// Annotations is a read-model field, not a stored column (0060 dropped the
	// blob): for a group frame the store populates it from group_notes; for
	// everything else notes live on the activity log (item_events).
	// Tags are normalized (lowercased, trimmed, unique) labels.
	Annotations string   `json:"annotations"`
	Tags        []string `json:"tags"`
	// Grid layout on the Scratchpad canvas (24-col snap grid, row-unit client-side).
	GridCol int `json:"grid_col"`
	GridRow int `json:"grid_row"`
	GridW   int `json:"grid_w"`
	GridH   int `json:"grid_h"`
	// Dedup-at-classify candidate match. Populated by the agent right
	// after embedding when the new item's vector cosine-matches an
	// existing scratchpad_item in the same project above the dedup
	// threshold. SimilarToID is the candidate's id; SimilarityScore is
	// the cosine value (0..1). Both empty/zero when no candidate. The
	// SPA shows a "possible duplicate" banner; user can dismiss
	// (clears both fields) or open the candidate to compare.
	SimilarToID     string  `json:"similar_to_id,omitempty"`
	SimilarityScore float64 `json:"similarity_score,omitempty"`
	// Rich-canvas binary metadata (migration 0022). All zero/empty
	// for text/code_snippet/link items; populated for image/file
	// content types. BlobSHA is the cross-reference into the
	// BlobStore (internal/blobstore). MimeType lets the HTTP layer
	// set Content-Type without sniffing. Width/Height are intrinsic
	// pixel dimensions (images only). FileName preserves the
	// original upload name for download Content-Disposition.
	// ByteSize is the blob's size in bytes, denormalized so list
	// views don't have to round-trip to the store.
	BlobSHA  string `json:"blob_sha,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileName string `json:"file_name,omitempty"`
	ByteSize int64  `json:"byte_size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	// OpenGraph metadata for content_type='link' items (Slice 3).
	// Populated asynchronously after item create by the OG fetcher
	// (internal/api/og.go). OGFetchedAt is set on BOTH success and
	// failure — its presence means "we tried"; empty title/description/
	// image_sha after a non-nil fetched_at means "the site had no OG
	// metadata we could parse." OGImageSHA references a blob stored
	// via the same BlobStore as binary items; the GC sweeper's
	// live-set query includes og_image_sha so OG thumbnails aren't
	// swept.
	OGTitle       string     `json:"og_title,omitempty"`
	OGDescription string     `json:"og_description,omitempty"`
	OGImageSHA    string     `json:"og_image_sha,omitempty"`
	OGFetchedAt   *time.Time `json:"og_fetched_at,omitempty"`
	// Slice 6 — group membership. GroupID points at the parent
	// group's id (a scratchpad_item with content_type='group');
	// empty for ungrouped items and for the groups themselves.
	// Collapsed is only meaningful when content_type='group' —
	// the UI hides the group's children when set.
	GroupID   string    `json:"group_id,omitempty"`
	Collapsed bool      `json:"collapsed,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TodoItem struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	CreatorID    string `json:"creator_id"`
	SourceItemID string `json:"source_item_id,omitempty"` // scratchpad item id, nullable when source deleted
	// SourceName mirrors the source scratchpad item's name when set, so
	// list views can prefer the user-given label over the auto-derived
	// Subject. Populated by storage queries via LEFT JOIN; empty when
	// the source item has no name or has been deleted.
	SourceName string `json:"source_name,omitempty"`
	// Number is a 1-based per-project sequence, assigned at creation. Stored
	// as INTEGER; the UI renders T-{n} when surfaced (today: not exposed —
	// reserved for external-ID sync and cross-references).
	Number   int    `json:"number"`
	Subject  string `json:"subject"`
	Priority string `json:"priority"` // high, medium, low, none
	// Tags live on the work record (canvas rework C3); seeded from the source's
	// #hashtags at derivation, editable here.
	Tags        []string   `json:"tags"`
	Status      string     `json:"status"`     // incomplete, in_progress, complete, abandoned
	Origin      string     `json:"origin"`     // manual, agent-derived
	Visibility  string     `json:"visibility"` // "private" | "project"
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// DueDate is an optional user-set deadline (CodeDestill_imports todo #6).
	DueDate *time.Time `json:"due_date,omitempty"`
	// CommitSHA / CommitTag: filled when status flips to complete —
	// CommitSHA auto-populated from project HEAD if blank, CommitTag is
	// user-supplied. Cleared on flip back to incomplete. See migration
	// 0017 for the full transition rules.
	CommitSHA string `json:"commit_sha,omitempty"`
	CommitTag string `json:"commit_tag,omitempty"`
	// ClaimedBy / ClaimedAt: who/what is currently working on this todo.
	// Set by POST /api/v1/todos/{id}/claim (or the equivalent MCP tool).
	// Survives the move to a terminal status as a historical record so
	// the UI can show "completed by Claude on …".
	ClaimedBy string     `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	// MCP-export sync state (v0.8.2 #9 client). Populated by the
	// outbound worker when the user has a destination configured for
	// todos. SyncStatus is one of "local-only" | "pending" | "synced"
	// | "failed"; when no destination is configured, every item is
	// "local-only" and the other fields are empty.
	RemoteID      string     `json:"remote_id,omitempty"`
	SyncStatus    string     `json:"sync_status,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
}

type BugItem struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	CreatorID    string `json:"creator_id"`
	SourceItemID string `json:"source_item_id,omitempty"`
	// See TodoItem.SourceName.
	SourceName string `json:"source_name,omitempty"`
	Number     int    `json:"number"`
	Subject    string `json:"subject"`
	Severity   string `json:"severity"` // critical, major, minor, trivial
	// Tags live on the work record (canvas rework C3).
	Tags              []string  `json:"tags"`
	Status            string    `json:"status"` // open, investigating, in-progress, fixed, verified, closed
	StepsToReproduce  string    `json:"steps_to_reproduce,omitempty"`
	ExpectedBehavior  string    `json:"expected_behavior,omitempty"`
	ActualBehavior    string    `json:"actual_behavior,omitempty"`
	Environment       string    `json:"environment,omitempty"`
	AffectedComponent string    `json:"affected_component,omitempty"`
	Origin            string    `json:"origin"`
	Visibility        string    `json:"visibility"` // "private" | "project"
	CreatedAt         time.Time `json:"created_at"`
	// CompletedAt / CommitSHA / CommitTag: set when status flips to a
	// terminal value (fixed, verified, closed) — CompletedAt auto-stamped,
	// CommitSHA auto-populated from project HEAD if blank, CommitTag is
	// user-supplied. See migration 0017 for the full transition rules.
	// Note: validBugTransition currently forbids backward transitions, so
	// the "clear on un-flip" branch isn't reachable via the HTTP API today.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	CommitTag   string     `json:"commit_tag,omitempty"`
	// DueDate is an optional user-set deadline (UC-46; mirrors TodoItem).
	DueDate *time.Time `json:"due_date,omitempty"`
	// See TodoItem.ClaimedBy.
	ClaimedBy string     `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	// See TodoItem for sync-field semantics.
	RemoteID      string     `json:"remote_id,omitempty"`
	SyncStatus    string     `json:"sync_status,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
}

type KnowledgeEntry struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	CreatorID    string `json:"creator_id"`
	SourceItemID string `json:"source_item_id,omitempty"`
	// See TodoItem.SourceName.
	SourceName string `json:"source_name,omitempty"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	// Tags live on the work record (canvas rework C3).
	Tags       []string `json:"tags"`
	Visibility string   `json:"visibility"` // "private" | "project"
	// Status: "active" | "deprecated". Deprecated KB stays searchable
	// (with include_done=true) but is hidden from default list views
	// and MCP reads — for "this knowledge is stale, kept for history."
	Status string `json:"status"`
	// Kind structures the project brain (glass-box Phase 5): "architecture" |
	// "convention" | "decision" | "reference". reference is the default (plain
	// KB); the first three are the agent-facing context substrate.
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	// See TodoItem.ClaimedBy. KB rarely needs claim semantics in
	// practice but the columns exist for symmetry across types.
	ClaimedBy string     `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	// See TodoItem for sync-field semantics.
	RemoteID      string     `json:"remote_id,omitempty"`
	SyncStatus    string     `json:"sync_status,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
}

// UseCaseItem captures a persistent capability the product enables — what the
// system *does*, in contrast to a TodoItem which is a unit of work to be done
// (e.g. "users can export a project as a zip" vs. "add a -export CLI flag").
// Status tracks lifecycle from proposed through implemented (or abandoned);
// the commit attribution triple (CommitSHA, CommitTag, ImplementationDate) is
// set when status flips to "completed", recording when and where the
// capability shipped. Implementation locations themselves live as CodeAnchor
// rows with OwnerType="use_case_item".
type UseCaseItem struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	CreatorID    string `json:"creator_id"`
	SourceItemID string `json:"source_item_id,omitempty"`
	// See TodoItem.SourceName.
	SourceName string `json:"source_name,omitempty"`
	// Number is a 1-based per-project sequence, surfaced in the UI as UC-{n}.
	Number      int    `json:"number"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	// Role / Want / Why are the structured pieces of an "as a [role] I want X
	// so that Y" user story, populated by the agent's classify pass when the
	// LLM detects that shape. Empty when the source is a casual note; the
	// original paste lives in Description either way.
	Role string `json:"role,omitempty"`
	Want string `json:"want,omitempty"`
	Why  string `json:"why,omitempty"`
	// Tags live on the work record (canvas rework C3).
	Tags   []string `json:"tags"`
	Status string   `json:"status"` // "open" | "approved" | "in_progress" | "completed" | "rejected"
	// Priority uses the same vocabulary as TodoItem. Bugs deliberately do not
	// have one — they rank by Severity, which PriorityRank maps onto this order
	// rather than giving bugs two competing axes (migration 0062).
	Priority           string     `json:"priority"` // high, medium, low, none
	TargetRelease      string     `json:"target_release,omitempty"`
	ImplementationDate *time.Time `json:"implementation_date,omitempty"`
	CommitSHA          string     `json:"commit_sha,omitempty"`
	CommitTag          string     `json:"commit_tag,omitempty"`
	// DueDate is an optional user-set deadline (UC-46; mirrors TodoItem).
	DueDate    *time.Time `json:"due_date,omitempty"`
	Origin     string     `json:"origin"`     // "manual" | "agent-derived"
	Visibility string     `json:"visibility"` // "private" | "project"
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// See TodoItem.ClaimedBy.
	ClaimedBy string     `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	// See TodoItem for sync-field semantics.
	RemoteID      string     `json:"remote_id,omitempty"`
	SyncStatus    string     `json:"sync_status,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
}

// CodeAnchor is a structured pointer from a domain item to code — either a
// file range (path + optional line range, optionally pinned to a git revision),
// a commit SHA, or a pull-request URL. A single polymorphic table backs all
// four owner kinds; OwnerType discriminates.
//
// Kind semantics:
//   - "file":   Path required; LineStart/LineEnd optional (0 = unset);
//     Revision optional (empty = working copy / latest).
//   - "commit": Revision required (7–40 hex chars); URL optional; Path unused.
//   - "pr":     URL required; other fields advisory.
//
// Provenance records how the anchor was created and feeds future re-extraction
// rules (Req 23-style) without a schema change.
// ItemEvent is one entry in an item's lineage log (UC-5). owner_type/
// owner_id point at the affected row; the lineage view merges a source
// scratchpad_item's events with those of its derived work item. Kind is a
// short machine tag (created, classified, reclassified, tags-changed,
// annotated, moved, status-changed, implemented); Summary is the
// human-readable line; Source is the actor surface ('ui'|'mcp'|'agent').
// APIToken is a per-user programmatic token (multi-user auth). An agent
// presenting it acts as UserID. TokenHash is the SHA-256 of the raw token; ID is
// a non-secret handle for listing/revoking.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TokenHash  string     `json:"-"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Session is a server-side login session (multi-user auth). ID is the SHA-256 of
// the random cookie token — the raw secret never hits the DB. Resolved per
// request to set the acting user; revocable.
type Session struct {
	ID           string    `json:"-"`
	UserID       string    `json:"user_id"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserAgent    string    `json:"user_agent,omitempty"`
}

type ItemEvent struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	// Body is the full markdown content for a note-kind entry (UC-51 activity
	// log). One-line system events leave this empty and use Summary.
	Body string `json:"body,omitempty"`
	// Source is the MECHANISM/surface the action came through: "ui" | "agent".
	Source string `json:"source"`
	// ActorUserID is WHO performed it (a user id) — orthogonal to Source. In
	// single-user mode this is always the local user; it varies only once
	// per-user auth lands. Empty for pre-attribution history.
	ActorUserID string    `json:"actor_user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type CodeAnchor struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"` // "scratchpad_item" | "todo_item" | "bug_item" | "knowledge_entry" | "use_case_item"
	OwnerID   string `json:"owner_id"`
	Kind      string `json:"kind"` // "file" | "commit" | "pr"
	// CodebaseID is optional. NULL/empty means anchored against the project's
	// single/primary codebase — the common single-user case. Multi-codebase
	// projects must set it explicitly so the Files panel can resolve paths
	// against the right repo.
	CodebaseID string    `json:"codebase_id,omitempty"`
	Path       string    `json:"path,omitempty"`
	LineStart  int       `json:"line_start,omitempty"`
	LineEnd    int       `json:"line_end,omitempty"`
	Revision   string    `json:"revision,omitempty"`
	URL        string    `json:"url,omitempty"`
	Label      string    `json:"label,omitempty"`
	Provenance string    `json:"provenance"` // "user-set" | "url-detected" | "file-dropped" | "agent-suggested"
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CodeChunk is a sliding-window slice of a project's source file with an
// embedding vector. Stored denormalized (content kept alongside file_path
// + line range) so search results can render snippets without re-reading
// files. Indexer upserts on (project_id, file_path, line_start, line_end);
// content_hash gates re-embedding so unchanged chunks skip the LLM call.
type CodeChunk struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	FilePath     string    `json:"file_path"`
	LineStart    int       `json:"line_start"`
	LineEnd      int       `json:"line_end"`
	ContentHash  string    `json:"content_hash"`
	Content      string    `json:"content"`
	HasEmbedding bool      `json:"has_embedding"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CodeChunkSearchHit is the search-side result for a chunk: id + range
// + cosine + a short content excerpt for display. Used by auto-anchor
// + future Q&A. Embedding stays inside storage; rank consumer doesn't
// need the raw vector.
type CodeChunkSearchHit struct {
	ID        string  `json:"id"`
	FilePath  string  `json:"file_path"`
	LineStart int     `json:"line_start"`
	LineEnd   int     `json:"line_end"`
	Snippet   string  `json:"snippet"`
	Score     float32 `json:"score"`
}

// CodeAnchorWithOwner pairs a CodeAnchor with enough information about its
// owner for the code canvas to render a tooltip and route a click. Returned
// from project-scoped lookups that need cross-owner-type results.
//
// OwnerScratchpadID is set only for owners of type "scratchpad_item" — needed
// so the SPA can switch scratchpads before highlighting the card.
type CodeAnchorWithOwner struct {
	CodeAnchor
	OwnerTitle        string `json:"owner_title"`
	OwnerScratchpadID string `json:"owner_scratchpad_id,omitempty"`
}

// AcceptanceCriterion is one discrete, individually-checkable assertion in an
// item's "definition of done" (glass-box Phase 2). owner_type/owner_id use the
// same addressing as CodeAnchor. State is the lifecycle: proposed → accepted →
// satisfied | failed, with rejected as a dismissal. SatisfiedBy is the commit
// that satisfied it (filled by the verification phase; empty until then).
type AcceptanceCriterion struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"` // "todo_item" | "bug_item" | "use_case_item" | …
	OwnerID   string `json:"owner_id"`
	Position  int    `json:"position"`
	Text      string `json:"text"`
	// VerificationKind reserves how the criterion gets checked once Phase 3
	// lands: "test" | "check" | "human" | "unspecified".
	VerificationKind string `json:"verification_kind"`
	// State: "proposed" | "accepted" | "satisfied" | "failed" | "rejected".
	State string `json:"state"`
	// Provenance: "ai-proposed" | "user-authored".
	Provenance  string `json:"provenance"`
	SatisfiedBy string `json:"satisfied_by,omitempty"` // commit SHA, Phase 3
	// TestCommand is the shell command that proves this criterion (glass-box
	// Phase 3, slice 2). Empty = no per-criterion check. When set, the
	// verification service runs it at the recorded commit and advances State.
	TestCommand string    `json:"test_command,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// VerificationResult is one check the verification portfolio ran against an
// item, and its verdict (glass-box Phase 3). owner_type/owner_id use the same
// addressing as CodeAnchor and AcceptanceCriterion. Slice 1 produces only the
// deterministic layer — the box runs the project's configured test command in
// an isolated worktree at CommitSHA — but Layer/Kind/ProducedBy reserve the
// AI-review and human tiers (plan principle #6). It is the trust signal the
// Phase-4 trust dial later consumes.
type VerificationResult struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"` // "todo_item" | "bug_item" | "use_case_item" | …
	OwnerID   string `json:"owner_id"`
	// Layer is the reliability tier: "deterministic" | "ai-review" | "human".
	Layer string `json:"layer"`
	// Kind is what the check is: "test" | "lint" | "build" | "check".
	Kind string `json:"kind"`
	// CheckName labels the check (e.g. the configured command or a short tag).
	CheckName string `json:"check_name"`
	// Verdict: "running" (async run in flight) | "pass" (exit 0) | "fail"
	// (command ran, reported failure) | "error" (harness couldn't get a clean
	// verdict — checkout failed, timeout, command not found).
	Verdict string `json:"verdict"`
	// CommitSHA is the commit the check ran against; empty until known.
	CommitSHA string `json:"commit_sha,omitempty"`
	// ExitCode is the command's exit status; nil while running or on a harness
	// error that never reached the command.
	ExitCode *int `json:"exit_code,omitempty"`
	// Summary is a one-line human description ("12 passed, 0 failed" or the
	// harness error). Output is the captured, size-capped command output.
	Summary    string `json:"summary"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
	// ProducedBy: "box" | "agent" | "ai" | "human".
	ProducedBy string    `json:"produced_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Verification layer, kind, verdict, and producer constants — the small fixed
// vocabularies the verification portfolio writes and the UI switches on.
const (
	VerifyLayerDeterministic = "deterministic"
	VerifyLayerAIReview      = "ai-review"
	VerifyLayerHuman         = "human"

	VerifyKindTest  = "test"
	VerifyKindLint  = "lint"
	VerifyKindTypes = "types"
	VerifyKindSAST  = "sast"
	VerifyKindVuln  = "vuln"
	VerifyKindBuild = "build"
	VerifyKindCheck = "check"

	VerifyVerdictRunning = "running"
	VerifyVerdictPass    = "pass"
	VerifyVerdictFail    = "fail"
	VerifyVerdictError   = "error"
	// Skipped: the check could not RUN (the tool isn't installed — exit
	// 127/126) as opposed to running and failing. Neutral: never a pass,
	// never counted as a failure that gates completion or dents trust.
	// Reporting a missing tool as "fail" is a false red that undermines the
	// "green is earned" signal.
	VerifyVerdictSkipped = "skipped"

	VerifyProducedByBox   = "box"
	VerifyProducedByAgent = "agent"
	VerifyProducedByAI    = "ai"
	VerifyProducedByHuman = "human"
)

// ReviewDecision is the human floor's call on an implemented change (glass-box
// Phase 4, slice 4.3): approved or rejected, with an optional note and the
// commit reviewed. owner_type/owner_id address the work item like CodeAnchor.
// The latest decision per item is current; an approval is positive trust
// evidence, a rejection negative — closing the oversight loop.
type ReviewDecision struct {
	ID        string    `json:"id"`
	OwnerType string    `json:"owner_type"`
	OwnerID   string    `json:"owner_id"`
	Decision  string    `json:"decision"` // "approved" | "rejected"
	CommitSHA string    `json:"commit_sha,omitempty"`
	Reviewer  string    `json:"reviewer,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReviewFlag is a persistent "this change needs another look" marker, distinct
// from a ReviewDecision (a human verdict that feeds the trust dial) and from the
// computed risk score. While active (ClearedAt == nil) it forces the item into
// the review queue with its Reason, but it is NEVER counted as trust evidence.
// Source labels what raised it (e.g. "skill_remediation").
type ReviewFlag struct {
	ID        string     `json:"id"`
	OwnerType string     `json:"owner_type"`
	OwnerID   string     `json:"owner_id"`
	Reason    string     `json:"reason"`
	Source    string     `json:"source"`
	CreatedAt time.Time  `json:"created_at"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
}

// ArchitectureNode is one component in the self-maintaining architecture diagram
// (glass-box Phase 5). The LLM drafts nodes as proposed; the human ratifies.
// Kind: "ui" | "service" | "store" | "external" | "component". Area is an
// optional path hint wiring the node to code. PosX/PosY persist the layout.
type ArchitectureNode struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
	Area        string    `json:"area,omitempty"`
	PosX        float64   `json:"pos_x"`
	PosY        float64   `json:"pos_y"`
	Provenance  string    `json:"provenance"` // "proposed" | "ratified"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ArchitectureEdge is a relationship between two architecture nodes.
type ArchitectureEdge struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	FromNode   string    `json:"from_node"`
	ToNode     string    `json:"to_node"`
	Label      string    `json:"label,omitempty"`
	Provenance string    `json:"provenance"`
	CreatedAt  time.Time `json:"created_at"`
}

// Custom field types.
const (
	CustomFieldText   = "text"
	CustomFieldNumber = "number"
	CustomFieldSelect = "select"
	CustomFieldDate   = "date"
)

// CustomFieldDef is a user-defined, project-scoped typed field (backlog item #1).
// AppliesTo lists the owner types (todo_item/bug_item/use_case_item/
// knowledge_entry) it shows on. Options is used only by the "select" type.
type CustomFieldDef struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	FieldType string    `json:"field_type"`
	Options   []string  `json:"options"`
	AppliesTo []string  `json:"applies_to"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CustomFieldValue is one item's value for a custom field (polymorphic owner,
// same pattern as code anchors / verification results). Value is stored as text
// regardless of type; the type drives the editor widget + validation.
type CustomFieldValue struct {
	FieldID   string    `json:"field_id"`
	OwnerType string    `json:"owner_type"`
	OwnerID   string    `json:"owner_id"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Skill is a reusable plain-language instruction set the agent retrieves over
// MCP and follows (backlog item #16). Project-scoped; a paid feature.
type Skill struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Enabled   bool      `json:"enabled"`
	Position  int       `json:"position"`
	Version   int       `json:"version"` // current head version (see SkillVersion history)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SkillVersion is one immutable snapshot of a Skill's name+content. A new
// version is cut whenever the name or content changes (never for enabled/
// position). History is append-only; the only removal is a deliberate
// compliance purge.
type SkillVersion struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skill_id"`
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

// SkillRetrieval records the raw fact that get_skill fetched a skill version.
// A fetch is evidence-of-availability, not proof of use — kept separate from
// SkillApplication so "retrieved" is never mistaken for "applied".
type SkillRetrieval struct {
	ID           string    `json:"id"`
	SkillID      string    `json:"skill_id"`
	SkillVersion int       `json:"skill_version"`
	ProjectID    string    `json:"project_id"`
	Actor        string    `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}

// Evidence classes for a SkillApplication — how we know the skill was used.
// This is a spec-shaped, vendor-neutral taxonomy (see
// docs/design/skill-provenance-standard.md).
const (
	SkillEvidenceAttested  = "attested"  // the agent explicitly declared it (skills_used)
	SkillEvidenceRetrieved = "retrieved" // inferred from a fetch during the work
)

// SkillApplication binds a skill version to a specific change (owner + commit)
// — the evidence-gated tie to the throughline. It is written ONLY when there is
// evidence of use; an item with no applications means no skill was used. This
// is CodeDistill's "skill application record" (candidate open standard).
type SkillApplication struct {
	ID           string    `json:"id"`
	SkillID      string    `json:"skill_id"`
	SkillVersion int       `json:"skill_version"`
	OwnerType    string    `json:"owner_type"`
	OwnerID      string    `json:"owner_id"`
	CommitSHA    string    `json:"commit_sha"`
	Evidence     string    `json:"evidence"`
	CreatedAt    time.Time `json:"created_at"`
}

// ValidCustomFieldType reports whether t is a supported field type.
func ValidCustomFieldType(t string) bool {
	switch t {
	case CustomFieldText, CustomFieldNumber, CustomFieldSelect, CustomFieldDate:
		return true
	}
	return false
}

// DayCount is one day bucket in a throughput series. Date is a
// local-time YYYY-MM-DD string (SQLite's date(ts,'localtime')); Count
// is the number of rows that fell into that bucket. Only non-zero
// days are emitted — the frontend lays out a calendar and fills gaps.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// DashboardThroughput backs the project metrics dashboard's Phase A
// throughput panel. Per-day created/completed counts for the three
// item types with a terminal status, plus the current open count per
// type. Maps are keyed by item-type slug: "todos", "bugs",
// "use_cases".
//
// "Completed" uses the per-type completion timestamp:
//   - todos:     completed_at
//   - bugs:      completed_at
//   - use_cases: implementation_date
//
// Since is the lower bound (inclusive) of the window the caller asked
// for, echoed back so the frontend can render the right calendar
// without having to round-trip its own clock.
type DashboardThroughput struct {
	CreatedByDay   map[string][]DayCount `json:"created_by_day"`
	CompletedByDay map[string][]DayCount `json:"completed_by_day"`
	Open           map[string]int        `json:"open"`
	Since          time.Time             `json:"since"`
}

// CoverageCount is one embedding-coverage bucket in the indexing-health
// panel: how many rows of a table have an embedding vs. how many exist.
type CoverageCount struct {
	Embedded int `json:"embedded"`
	Total    int `json:"total"`
}

// DashboardIndexHealth backs the dashboard's indexing-health panel
// (Phase A panel #3). All counts are scoped to one project.
//
// Coverage is keyed by item-type slug: "items" (scratchpad_items),
// "todos", "bugs", "kb", "use_cases". AnchorsByProvenance uses the
// code_anchors.provenance values (user-set, url-detected, file-dropped,
// agent-suggested) — agent-suggested is the auto/reverse-anchor count.
// DedupFlagged counts scratchpad items the classifier marked as a
// near-duplicate (similar_to_id set); rate = flagged / Coverage["items"].Total.
type DashboardIndexHealth struct {
	Coverage            map[string]CoverageCount `json:"coverage"`
	ChunkCount          int                      `json:"chunk_count"`
	ChunkBytes          int64                    `json:"chunk_bytes"`
	ChunkUnembedded     int                      `json:"chunk_unembedded"`
	ChunkFailed         int                      `json:"chunk_failed"`
	AnchorTotal         int                      `json:"anchor_total"`
	AnchorsByProvenance map[string]int           `json:"anchors_by_provenance"`
	DedupFlagged        int                      `json:"dedup_flagged"`
}

// DashboardClosedItem is one closed/implemented item row feeding the
// time-to-close and per-branch dashboard panels. Type is the item-type
// slug ("todos", "bugs", "use_cases"); ClosedAt is the per-type
// completion timestamp (completed_at / implementation_date); CommitSHA
// is empty when the item was closed without a commit reference.
type DashboardClosedItem struct {
	Type      string
	CreatedAt time.Time
	ClosedAt  time.Time
	CommitSHA string
}

// CreditsSummary is the workspace-wide vanity rollup behind the hidden
// "credits" modal (triple-click the version chip). All fields are simple
// totals — no per-project breakdown, no time windowing.
type CreditsSummary struct {
	ItemsCreated   int `json:"items_created"`
	TodosCompleted int `json:"todos_completed"`
	BugsFiled      int `json:"bugs_filed"`
}

// GlobalStats is the cross-project usage summary behind the profile-menu Stats
// modal (UC-64). Item counts are broken down by status (status → count).
type GlobalStats struct {
	Projects    int            `json:"projects"`
	Scratchpads int            `json:"scratchpads"`
	KB          int            `json:"kb"`
	Todos       map[string]int `json:"todos"`
	Bugs        map[string]int `json:"bugs"`
	UseCases    map[string]int `json:"use_cases"`
}
