/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

// Mirrors the Go domain structs in internal/domain/domain.go.
// Keep field names in sync with the JSON tags there.

// Phase 2 additions (multi-user data model). Workspaces, Users, Codebases,
// Members. The current UI does not surface them yet; types exist so the
// augmented JSON responses round-trip cleanly.
export type Plan = 'free' | 'team' | 'enterprise';
export type Role = 'owner' | 'admin' | 'member';
export type Visibility = 'private' | 'project';

export interface Workspace {
  id: string;
  name: string;
  plan: Plan;
  seat_limit?: number;
  created_at: string;
}

export interface User {
  id: string;
  email?: string;
  display_name: string;
  avatar?: string;
  external_id?: string;
  provider?: 'local' | 'google' | 'github' | 'oidc';
  created_at: string;
}

export interface WorkspaceMember {
  workspace_id: string;
  user_id: string;
  role: Role;
  created_at: string;
}

export interface ProjectMember {
  project_id: string;
  user_id: string;
  role: Role;
  created_at: string;
}

export interface Codebase {
  id: string;
  project_id: string;
  name: string;
  repo_root: string;
  created_at: string;
}

export interface Project {
  id: string;
  workspace_id: string;
  name: string;
  // Legacy single-repo path. Phase 2 introduces Codebase as the source of
  // truth; repo_root remains populated for the single-codebase case so the
  // existing Files panel keeps working until it's updated.
  repo_root?: string;
  created_at: string;
}

// Owner type for code_anchors rows. Matches the Go constants in
// internal/api/code_anchors.go.
export type CodeAnchorOwnerType =
  | 'scratchpad_item'
  | 'todo_item'
  | 'bug_item'
  | 'knowledge_entry'
  | 'use_case_item';

export type CodeAnchorKind = 'file' | 'commit' | 'pr';

export type CodeAnchorProvenance =
  | 'user-set'
  | 'url-detected'
  | 'file-dropped'
  | 'agent-suggested';

export interface CodeAnchor {
  id: string;
  owner_type: CodeAnchorOwnerType;
  owner_id: string;
  kind: CodeAnchorKind;
  // Optional: codebase the anchor resolves against. Empty = the project's
  // single/primary codebase (the common single-user case).
  codebase_id?: string;
  path?: string;
  line_start?: number;
  line_end?: number;
  revision?: string;
  url?: string;
  label?: string;
  provenance: CodeAnchorProvenance;
  created_at: string;
  updated_at: string;
}

// Pairs a CodeAnchor with enough owner info for the code canvas to render
// tooltips and route clicks. Returned by GET /api/v1/projects/{pid}/code-anchors.
// owner_scratchpad_id is set only for owner_type === 'scratchpad_item'.
export interface CodeAnchorWithOwner extends CodeAnchor {
  owner_title: string;
  owner_scratchpad_id?: string;
}

// Code metrics (UC-100): per-item churn + change-complexity derived from
// the item's commit anchors. complexity is a transparent heuristic band
// (Low | Moderate | High | Very High); the raw signals are shown alongside.
export type ComplexityBand = 'Low' | 'Moderate' | 'High' | 'Very High';

export interface CommitMetrics {
  sha: string;
  short_sha: string;
  files_changed: number;
  added: number;
  deleted: number;
  net: number;
  hunks: number;
  excluded_files: number;
  complexity: ComplexityBand;
  complexity_score: number;
  found: boolean;
  label?: string;
}

export interface CodeMetricsTotals {
  commits: number;
  files_changed: number;
  added: number;
  deleted: number;
  net: number;
  hunks: number;
  excluded_files: number;
  complexity: ComplexityBand;
  complexity_score: number;
}

export interface CodeMetrics {
  repo_configured: boolean;
  commits: CommitMetrics[];
  totals: CodeMetricsTotals;
}

// Acceptance criteria (glass-box Phase 2): an item's measurable definition of
// done. state lifecycle: proposed → accepted → satisfied | failed, with
// rejected = dismissed. verification_kind reserves "how is this checked" for
// the verification phase.
export type CriterionState = 'proposed' | 'accepted' | 'satisfied' | 'failed' | 'rejected';
export type VerificationKind = 'unspecified' | 'test' | 'check' | 'human';

export interface AcceptanceCriterion {
  id: string;
  owner_type: CodeAnchorOwnerType;
  owner_id: string;
  position: number;
  text: string;
  verification_kind: VerificationKind;
  state: CriterionState;
  provenance: 'ai-proposed' | 'user-authored';
  satisfied_by?: string;
  created_at: string;
  updated_at: string;
}

// Verification (glass-box Phase 3): the deterministic layer's result — one
// check the box ran against an item. verdict: running (async in flight) | pass
// (exit 0) | fail (ran, reported failure) | error (harness couldn't get a clean
// verdict). The trust signal the trust dial will later consume.
export type VerificationVerdict = 'running' | 'pass' | 'fail' | 'error' | 'skipped';
export type VerificationLayer = 'deterministic' | 'ai-review' | 'human';

export interface VerificationResult {
  id: string;
  owner_type: CodeAnchorOwnerType;
  owner_id: string;
  layer: VerificationLayer;
  kind: string;
  check_name: string;
  verdict: VerificationVerdict;
  commit_sha?: string;
  exit_code?: number;
  summary: string;
  output: string;
  duration_ms: number;
  produced_by: string;
  created_at: string;
  updated_at: string;
}

// One mapped acceptance criterion + its latest per-criterion verdict (slice 2).
export interface CriterionVerification {
  criterion_id: string;
  text: string;
  command: string;
  state: CriterionState;
  result: VerificationResult | null; // latest deterministic (test) verdict
  review: VerificationResult | null; // latest adversarial AI-review verdict
}

// A configured item-level check (test or a scanner) — kind + command.
export interface ConfiguredCheck {
  kind: string;
  command: string;
}

export interface VerificationResponse {
  results: VerificationResult[];
  checks: ConfiguredCheck[];
  repo_configured: boolean;
  available: boolean;
  criteria: CriterionVerification[];
}

export type ClassificationMode = 'off' | 'strict' | 'full';

export interface Scratchpad {
  id: string;
  project_id: string;
  owner_id: string;
  name: string;
  classification_mode: ClassificationMode;
  visibility: Visibility;
  created_at: string;
}

export type ClassificationState =
  | 'unprocessed'
  | 'processing'
  | 'classified'
  | 'pending-review'
  | 'skipped'
  | 'failed';

export type ClassificationOverride = '' | 'todo' | 'bug' | 'kb' | 'use_case' | 'skip';

export interface ScratchpadItem {
  id: string;
  scratchpad_id: string;
  // Optional user-set label. UI falls back to the first non-blank line of
  // `content` when this is empty so existing items keep their appearance.
  name: string;
  content_type: 'text' | 'code_snippet' | 'link' | 'image' | 'file' | 'sketch' | 'composite' | 'group';
  content: string;
  // Rich-canvas binary metadata. All null/empty for text/code_snippet/link.
  // For image items, blob_sha is the content-addressed SHA-256 the
  // backend hands out at upload; fetch the bytes from /api/v1/blobs/<sha>.
  // width/height are intrinsic pixel dimensions; byte_size + file_name
  // are denormalized for list views.
  blob_sha?: string;
  mime_type?: string;
  file_name?: string;
  byte_size?: number;
  width?: number;
  height?: number;
  // OpenGraph metadata for content_type='link' items (Slice 3).
  // Populated asynchronously after item create; the SPA's POLL_MS
  // refresh will reveal them. og_fetched_at being present + the
  // other fields empty means "we tried, no OG data."
  og_title?: string;
  og_description?: string;
  og_image_sha?: string;
  og_fetched_at?: string;
  // Slice 6 — group membership. group_id points at the parent
  // group's id (a scratchpad_item with content_type='group').
  // collapsed is only meaningful when this item IS a group.
  group_id?: string;
  collapsed?: boolean;
  classification_state: ClassificationState;
  skipped_reason?: string;
  classification_override?: ClassificationOverride;
  proposed_category?: string;
  classification_confidence?: number;
  classification_reasoning?: string;
  derived_item_id?: string;
  hidden: boolean;
  archived_at?: string | null;
  annotations: string;
  tags: string[];
  grid_col: number;
  grid_row: number;
  grid_w: number;
  grid_h: number;
  // Dedup-at-classify candidate match. Set by the agent right after
  // embedding when an existing scratchpad_item in the same project
  // crosses the similarity threshold. SPA renders it in the
  // pending-review banner as "possible duplicate of …".
  similar_to_id?: string;
  similarity_score?: number;
  created_at: string;
  updated_at: string;
}

export type Priority = 'high' | 'medium' | 'low' | 'none';
export type TodoStatus = 'incomplete' | 'in_progress' | 'complete' | 'abandoned';

/** MCP-export sync cache columns (v0.8.2 outbound export). Present on
 *  every exportable item type. Empty / 'local-only' means the item has
 *  never been routed to a destination (no config for the type, or the
 *  item predates it). */
export interface SyncFields {
  /** Destination's id for the item, captured from the remote create. */
  remote_id?: string;
  /** 'local-only' | 'pending' | 'synced' | 'failed' */
  sync_status?: string;
  last_sync_at?: string;
  /** Most recent failure detail; shown in the SyncIndicator tooltip. */
  last_sync_error?: string;
}

export interface TodoItem extends SyncFields {
  id: string;
  project_id: string;
  creator_id: string;
  source_item_id?: string;
  /** Name from the originating scratchpad item, when the user set one.
   *  List views prefer this over `subject` for the visible label. */
  source_name?: string;
  /** Per-project sequence (T-{n}). Stored everywhere; surfaced only on use cases for now. */
  number: number;
  subject: string;
  priority: Priority;
  status: TodoStatus;
  origin: 'manual' | 'agent-derived';
  visibility: Visibility;
  created_at: string;
  completed_at?: string;
  /** Set when status flips to complete (auto-filled from project HEAD if blank). Cleared on reopen. */
  commit_sha?: string;
  commit_tag?: string;
  /** Optional user-set deadline (ISO). */
  due_date?: string;
  /** Free-form tags on the work record (canvas rework C3). Editable here; the source item's own tags stay frozen as provenance. */
  tags: string[];
  /** Who/what is currently working on this todo. Set by POST /todos/{id}/claim or the equivalent MCP tool. Survives the move to a terminal status as a historical record. */
  claimed_by?: string;
  claimed_at?: string;
}

export type Severity = 'critical' | 'major' | 'minor' | 'trivial';
export type BugStatus =
  // Active states.
  | 'open' | 'investigating' | 'in-progress'
  // Fix-type terminals — these auto-fill commit_sha from project HEAD
  // when transitioning into them (priority #2 plumbing).
  | 'fixed' | 'verified' | 'closed'
  // Non-fix terminals — bug leaves the active queue but no commit
  // attribution. completed_at still stamped.
  | 'not_a_bug' | 'wont_fix' | 'duplicate';

export interface BugItem extends SyncFields {
  id: string;
  project_id: string;
  creator_id: string;
  source_item_id?: string;
  /** See TodoItem.source_name. */
  source_name?: string;
  number: number;
  subject: string;
  severity: Severity;
  status: BugStatus;
  due_date?: string;
  /** Work-record tags (canvas rework C3). See TodoItem.tags. */
  tags: string[];
  steps_to_reproduce?: string;
  expected_behavior?: string;
  actual_behavior?: string;
  environment?: string;
  affected_component?: string;
  origin: 'manual' | 'agent-derived';
  visibility: Visibility;
  created_at: string;
  /** Set when status flips into a terminal state (fixed/verified/closed). Pinned to first crossing. */
  completed_at?: string;
  commit_sha?: string;
  commit_tag?: string;
  /** See TodoItem.claimed_by. */
  claimed_by?: string;
  claimed_at?: string;
}

export type KnowledgeStatus = 'active' | 'deprecated';

export type KbKind = 'architecture' | 'convention' | 'decision' | 'reference';

export interface KnowledgeEntry extends SyncFields {
  id: string;
  project_id: string;
  creator_id: string;
  source_item_id?: string;
  /** See TodoItem.source_name. */
  source_name?: string;
  number: number;
  title: string;
  content: string;
  visibility: Visibility;
  /** active = visible by default; deprecated = hidden in lists, kept for history. */
  status: KnowledgeStatus;
  /** Project-brain kind (glass-box Phase 5). reference = plain KB. */
  kind: KbKind;
  /** Work-record tags (canvas rework C3). See TodoItem.tags. */
  tags: string[];
  created_at: string;
  /** See TodoItem.claimed_by. KB rarely needs claim semantics in practice. */
  claimed_by?: string;
  claimed_at?: string;
}

/** Renamed + widened v0.10.14 (migration 0030): approved = selected
 *  for implementation; rejected/completed are the terminal states. */
export type UseCaseStatus = 'open' | 'approved' | 'in_progress' | 'completed' | 'rejected';

export interface UseCaseItem extends SyncFields {
  id: string;
  project_id: string;
  creator_id: string;
  source_item_id?: string;
  /** See TodoItem.source_name. */
  source_name?: string;
  /** Per-project sequence; rendered as `UC-{number}` in the UI. */
  number: number;
  subject: string;
  description?: string;
  /** Agent-extracted "as a [role] I want X so that Y" pieces — empty when the source isn't a user story. */
  role?: string;
  want?: string;
  why?: string;
  status: UseCaseStatus;
  due_date?: string;
  /** Work-record tags (canvas rework C3). See TodoItem.tags. */
  tags: string[];
  target_release?: string;
  implementation_date?: string;
  commit_sha?: string;
  commit_tag?: string;
  origin: 'manual' | 'agent-derived';
  visibility: Visibility;
  created_at: string;
  updated_at: string;
  /** See TodoItem.claimed_by. */
  claimed_by?: string;
  claimed_at?: string;
}

/** Item types the implementation matcher (priority #3) ranks commits against. */
export type MatchableItemType = 'todo_item' | 'bug_item' | 'use_case_item';

/** Canvas card filter (ScratchpadPane + FilterPanel). 'all' = no filter;
 *  category values match effective classification; 'unclassified' = still
 *  funneling; 'pending_review' = agent flagged for confirmation. */
export type CanvasFilter =
  | 'all'
  | 'todo'
  | 'bug'
  | 'kb'
  | 'use_case'
  | 'unclassified'
  | 'pending_review';
