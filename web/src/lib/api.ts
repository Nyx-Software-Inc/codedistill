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

import type {
  Project,
  Scratchpad,
  ScratchpadItem,
  TodoItem,
  BugItem,
  KnowledgeEntry,
  KnowledgeStatus,
  KbKind,
  UseCaseItem,
  UseCaseStatus,
  ClassificationMode,
  ClassificationOverride,
  Priority,
  TodoStatus,
  Severity,
  BugStatus,
  CodeAnchor,
  CodeMetrics,
  AcceptanceCriterion,
  CriterionState,
  VerificationKind,
  VerificationResult,
  VerificationResponse,
  CodeAnchorWithOwner,
  CodeAnchorOwnerType,
  CodeAnchorKind,
} from './types';
import { activity } from './activity.svelte';

const BASE = '/api/v1';

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  activity.begin();
  try {
    return await reqInner<T>(method, path, body);
  } finally {
    activity.end();
  }
}

async function reqInner<T>(method: string, path: string, body?: unknown): Promise<T> {
  const opts: RequestInit = { method, headers: {} };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
    (opts.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  const res = await fetch(BASE + path, opts);
  if (!res.ok) {
    // Lift the server's structured `{"error": "..."}` payload to the front
    // of the error message so user-visible toasts read "name already in
    // use" instead of the raw "PATCH /api/v1/projects/abc: 409 {...}".
    // Path + status stay in the message for debugging.
    const text = await res.text();
    let msg = text;
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === 'string') msg = parsed.error;
    } catch { /* not JSON — keep raw text */ }
    throw new Error(`${msg} (${method} ${path}: ${res.status})`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// Health
export const health = () => req<{ status: string }>('GET', '/health');

// Version — build identity of the running binary, injected via -ldflags.
export interface VersionInfo {
  version: string;
  git_sha: string;
  build_date: string;
}
export const getVersion = () => req<VersionInfo>('GET', '/version');

// License — commercial state of the running binary. `features` is the
// list of currently-ON paid features ("mcp" | "anchors" | "matcher" |
// "multiuser" | "s3"); the SPA hides gated affordances accordingly.
// state: none|invalid|valid|grace|expired. Grace/expired drive the
// renewal banner in App.svelte.
export interface LicenseInfo {
  state: 'none' | 'invalid' | 'valid' | 'grace' | 'expired';
  edition?: string;
  customer?: string;
  seats?: number;
  features: string[];
  expires_at?: string;
  grace_until?: string;
  reason?: string;
  oss_build: boolean;
}
export const getLicense = () => req<LicenseInfo>('GET', '/license');

// Activation-service slice 2: redeem a purchase claim code / refresh after a
// renewal. Server calls the activation service with this machine's own
// fingerprint and installs the returned node-locked license.
export interface RedeemResult {
  customer: string;
  edition: string;
  expires_at: string;
  restart_required: boolean;
}
export const redeemLicense = (claimCode: string) =>
  req<RedeemResult>('POST', '/license/redeem', { claim_code: claimCode });
export const refreshLicense = () => req<RedeemResult>('POST', '/license/refresh');

// The current user (who the app is acting as). Single-user: the local user,
// display name from the license holder. Multi-user (later): the session user.
export interface Me {
  id: string;
  display_name: string;
  real_name?: string;
  email?: string;
  is_admin: boolean;
  multi_user: boolean;
}
export const getMe = () => req<Me>('GET', '/me');
export const updateMe = (displayName: string) =>
  req<{ display_name: string }>('PATCH', '/me', { display_name: displayName });
export const logout = () => req<void>('POST', '/auth/logout');

// Resolve any user's identity for the hover tooltip behind an attributed name.
export interface UserIdentity {
  id: string;
  display_name: string;
  real_name?: string;
  email?: string;
}

// --- Admin: workspace members (owner/admin only) ---
export interface Member {
  user_id: string;
  display_name: string;
  email?: string;
  role: string;
  status: string;
}
export const listMembers = () => req<Member[]>('GET', '/members');
export const deactivateMember = (userID: string) => req<void>('POST', `/members/${userID}/deactivate`);
export const reactivateMember = (userID: string) => req<void>('POST', `/members/${userID}/reactivate`);
export const setMemberRole = (userID: string, role: string) => req<void>('PUT', `/members/${userID}/role`, { role });

// Server access — the API write-auth token (Settings → Server access).
// getAuthToken requires the caller to already be authenticated (the SPA
// passes its cd_auth cookie); regenerate rotates it and kicks old clients.
export interface AuthTokenInfo {
  enabled: boolean;
  token?: string;
}
export const getAuthToken = () => req<AuthTokenInfo>('GET', '/auth/token');
export const regenerateAuthToken = () =>
  req<{ token: string }>('POST', '/auth/token/regenerate');

// Credits — workspace-wide vanity totals behind the hidden modal
// (triple-click the version chip).
export interface CreditsSummary {
  items_created: number;
  todos_completed: number;
  bugs_filed: number;
}
export const getCredits = () => req<CreditsSummary>('GET', '/credits');

// Power — current AC/Battery/Unknown source from the in-process
// power.Watcher. Polled by the SPA to drive the throttle indicator
// and the Indexing settings panel's live status line.
export type PowerSource = 'ac' | 'battery' | 'unknown';
export const getPowerSource = () =>
  req<{ source: PowerSource }>('GET', '/power');

// Projects
export const listProjects = () => req<Project[]>('GET', '/projects');

// Cross-project usage summary for the profile Stats modal (UC-64). Item counts
// are keyed by status.
export type GlobalStats = {
  projects: number;
  scratchpads: number;
  kb: number;
  todos: Record<string, number>;
  bugs: Record<string, number>;
  use_cases: Record<string, number>;
};
export const getGlobalStats = () => req<GlobalStats>('GET', '/stats');
export const createProject = (body: { name: string; repo_root?: string }) =>
  req<Project>('POST', '/projects', body);
export const getProject = (id: string) => req<Project>('GET', `/projects/${id}`);
export const updateProject = (
  id: string,
  body: { name?: string; repo_root?: string },
) => req<Project>('PATCH', `/projects/${id}`, body);
export const deleteProject = (id: string) =>
  req<void>('DELETE', `/projects/${id}`);

// Scratchpads
export const listScratchpads = (projectId: string) =>
  req<Scratchpad[]>('GET', `/projects/${projectId}/scratchpads`);
export const createScratchpad = (
  projectId: string,
  body: { name: string; classification_mode?: ClassificationMode },
) => req<Scratchpad>('POST', `/projects/${projectId}/scratchpads`, body);
export const updateScratchpad = (
  id: string,
  body: { name?: string; classification_mode?: ClassificationMode },
) => req<Scratchpad>('PATCH', `/scratchpads/${id}`, body);
export const deleteScratchpad = (id: string) =>
  req<void>('DELETE', `/scratchpads/${id}`);

/** Compact the grid. Modes:
 *  - tidy: keep every card's size, slide each into the lowest gap that fits
 *  - fit:  recompute natural sizes for text cards from content, then tidy
 *  - cols2 / cols3: force a uniform 2- or 3-column width, then tidy */
export type RestackMode = 'tidy' | 'fit' | 'cols2' | 'cols3';
export const restackScratchpad = (id: string, mode: RestackMode = 'tidy') =>
  req<{ items_restacked: number }>('POST', `/scratchpads/${id}/restack`, { mode });

// Scratchpad Items
export const listItems = (scratchpadId: string, includeArchived = false) =>
  req<ScratchpadItem[]>('GET', `/scratchpads/${scratchpadId}/items${includeArchived ? '?include_archived=true' : ''}`);

// Archive / un-archive a scratchpad item (backlog item #19).
export const archiveItem = (id: string) =>
  req<void>('POST', `/items/${id}/archive`);
export const unarchiveItem = (id: string) =>
  req<void>('POST', `/items/${id}/unarchive`);
// Re-enqueue a stuck capture (unprocessed / failed) for classification.
export const reprocessItem = (id: string) =>
  req<{ id: string; status: string }>('POST', `/items/${id}/reprocess`);
export const createItem = (
  scratchpadId: string,
  body: {
    name?: string;
    content: string;
    content_type?: 'text' | 'code_snippet' | 'link' | 'image' | 'file' | 'sketch' | 'composite' | 'group';
    classification_override?: ClassificationOverride;
  },
) => req<ScratchpadItem>('POST', `/scratchpads/${scratchpadId}/items`, body);

// Bare blob upload — POSTs a file, returns {sha, size, mime_type,
// width, height}, does NOT create a scratchpad_item. Used by the
// sketch preview-upload flow and the future composite-doc paste
// flow. Optional scratchpadId scopes the BlobConfig cascade for
// per-project setting overrides.
export interface UploadedBlob {
  sha: string;
  size: number;
  mime_type: string;
  file_name?: string;
  width?: number;
  height?: number;
}
export async function uploadBlobOnly(file: File, scratchpadId?: string): Promise<UploadedBlob> {
  const fd = new FormData();
  fd.append('file', file, file.name);
  const qs = scratchpadId ? `?scratchpad_id=${encodeURIComponent(scratchpadId)}` : '';
  activity.begin();
  let res: Response;
  try {
    res = await fetch(`/api/v1/blobs${qs}`, { method: 'POST', body: fd });
  } finally {
    activity.end();
  }
  if (!res.ok) {
    const text = await res.text();
    let msg = text;
    try {
      const parsed = JSON.parse(text) as { error?: string };
      if (parsed.error) msg = parsed.error;
    } catch { /* not JSON */ }
    throw new Error(`POST /blobs: ${res.status} ${msg}`);
  }
  return (await res.json()) as UploadedBlob;
}

// dataURLtoBlob converts a 'data:image/png;base64,...' string into
// a real Blob/File for upload. Used by SketchEditor when persisting
// the Excalidraw preview PNG.
export function dataURLtoFile(dataURL: string, filename: string): File | null {
  const match = /^data:([^;]+);base64,(.*)$/.exec(dataURL);
  if (!match) return null;
  const mime = match[1];
  try {
    const bytes = atob(match[2]);
    const arr = new Uint8Array(bytes.length);
    for (let i = 0; i < bytes.length; i++) arr[i] = bytes.charCodeAt(i);
    return new File([arr], filename, { type: mime });
  } catch {
    return null;
  }
}

// Rich-canvas image upload. Posts a multipart body with a single
// 'file' field. Server-side caps + MIME allowlist live in BlobConfig
// (resolved per-request via the project/user settings cascade). 413
// = oversize; 415 = MIME not allowed; 400 = malformed body.
export async function uploadBlob(scratchpadId: string, file: File): Promise<ScratchpadItem> {
  const fd = new FormData();
  fd.append('file', file, file.name);
  activity.begin();
  let res: Response;
  try {
    res = await fetch(`/api/v1/scratchpads/${scratchpadId}/items/blob`, {
      method: 'POST',
      body: fd,
    });
  } finally {
    activity.end();
  }
  if (!res.ok) {
    const text = await res.text();
    let msg = text;
    try {
      const parsed = JSON.parse(text) as { error?: string };
      if (parsed.error) msg = parsed.error;
    } catch {
      // not JSON — leave msg as the raw text
    }
    throw new Error(`POST /scratchpads/${scratchpadId}/items/blob: ${res.status} ${msg}`);
  }
  return (await res.json()) as ScratchpadItem;
}
export const getItem = (id: string) => req<ScratchpadItem>('GET', `/items/${id}`);
export const updateItem = (
  id: string,
  body: {
    name?: string;
    content?: string;
    // Required (true) when editing a CLASSIFIED item's content — marks the
    // edit as a deliberate correction of the original text (source guard).
    correction?: boolean;
    classification_override?: ClassificationOverride;
    hidden?: boolean;
    annotations?: string;
    tags?: string[];
    grid_col?: number;
    grid_row?: number;
    grid_w?: number;
    grid_h?: number;
    // Blob metadata patches — used by the SketchEditor preview-upload
    // flow and the composite-doc paste flow.
    blob_sha?: string;
    mime_type?: string;
    byte_size?: number;
    width?: number;
    height?: number;
    // Slice 6 — group membership + collapse state. Empty
    // group_id removes the item from its group; the server
    // auto-deletes the previous group if empty.
    group_id?: string;
    collapsed?: boolean;
  },
) => req<ScratchpadItem>('PATCH', `/items/${id}`, body);
export const deleteItem = (id: string) => req<void>('DELETE', `/items/${id}`);

// UC-5 item lineage: the merged event log for a scratchpad item + its
// derived work item, oldest-first.
export interface ItemEvent {
  id: string;
  owner_type: string;
  owner_id: string;
  kind: string;
  summary: string;
  // Full markdown body for a note-kind entry (empty for one-line system events).
  body?: string;
  source: string; // 'ui' | 'mcp' | 'agent'
  actor_user_id?: string;
  created_at: string;
}
export const getItemLineage = (itemId: string) =>
  req<ItemEvent[]>('GET', `/items/${itemId}/lineage`);

// Move an item to a different scratchpad (same project). Backend rejects
// cross-project moves since anchors are repo-scoped.
export const moveItem = (id: string, scratchpadId: string) =>
  req<ScratchpadItem>('POST', `/items/${id}/move`, { scratchpad_id: scratchpadId });

// Dismiss the dedup candidate-match suggestion on an item ("not a
// duplicate" action from the pending-review banner).
export const dismissSimilarity = (id: string) =>
  req<ScratchpadItem>('POST', `/items/${id}/dismiss-similarity`);

// Accept a "possibly related" banner: group the item with its match.
export const groupSimilar = (id: string) =>
  req<void>('POST', `/items/${id}/group-similar`);

// Inbox
export const listInbox = () => req<ScratchpadItem[]>('GET', '/inbox');
export const acceptItem = (id: string) => req<ScratchpadItem>('POST', `/items/${id}/accept`);
export const reclassifyItem = (id: string, category: 'todo' | 'bug' | 'kb' | 'use_case') =>
  req<ScratchpadItem>('POST', `/items/${id}/reclassify`, { category });
export const rejectItem = (id: string) => req<ScratchpadItem>('POST', `/items/${id}/reject`);

// Append `?include_done=true` to a list URL when callers want done
// items in the response. The backend filters by default (the v0.8.3
// lifecycle work hides done items from list views unless asked).
const doneSuffix = (includeDone: boolean) =>
  includeDone ? '?include_done=true' : '';

// Todos
export const listTodos = (projectId: string, includeDone = false) =>
  req<TodoItem[]>('GET', `/projects/${projectId}/todos${doneSuffix(includeDone)}`);
export const listTodosByScratchpad = (scratchpadId: string, includeDone = false) =>
  req<TodoItem[]>('GET', `/scratchpads/${scratchpadId}/todos${doneSuffix(includeDone)}`);
export const getTodo = (id: string) => req<TodoItem>('GET', `/todos/${id}`);
export const updateTodo = (
  id: string,
  body: {
    subject?: string;
    priority?: Priority;
    status?: TodoStatus;
    commit_sha?: string;
    commit_tag?: string;
    due_date?: string; // ISO date ("2026-06-30") or RFC3339; "" clears
    tags?: string[]; // work-record tags (canvas rework C3); replaces the full set
  },
) => req<TodoItem>('PATCH', `/todos/${id}`, body);
export const deleteTodo = (id: string) => req<void>('DELETE', `/todos/${id}`);
export const claimTodo = (id: string, claimedBy: string) =>
  req<TodoItem>('POST', `/todos/${id}/claim`, { claimed_by: claimedBy });
export const reopenTodo = (id: string) =>
  req<TodoItem>('POST', `/todos/${id}/reopen`);

// Bugs
export const listBugs = (projectId: string, includeDone = false) =>
  req<BugItem[]>('GET', `/projects/${projectId}/bugs${doneSuffix(includeDone)}`);
export const listBugsByScratchpad = (scratchpadId: string, includeDone = false) =>
  req<BugItem[]>('GET', `/scratchpads/${scratchpadId}/bugs${doneSuffix(includeDone)}`);
export const getBug = (id: string) => req<BugItem>('GET', `/bugs/${id}`);
export const updateBug = (
  id: string,
  body: {
    subject?: string;
    severity?: Severity;
    status?: BugStatus;
    steps_to_reproduce?: string;
    expected_behavior?: string;
    actual_behavior?: string;
    environment?: string;
    affected_component?: string;
    commit_sha?: string;
    commit_tag?: string;
    due_date?: string; // ISO date ("2026-06-30") or RFC3339; "" clears
    tags?: string[]; // work-record tags (canvas rework C3); replaces the full set
  },
) => req<BugItem>('PATCH', `/bugs/${id}`, body);
export const deleteBug = (id: string) => req<void>('DELETE', `/bugs/${id}`);
export const claimBug = (id: string, claimedBy: string) =>
  req<BugItem>('POST', `/bugs/${id}/claim`, { claimed_by: claimedBy });
export const reopenBug = (id: string) =>
  req<BugItem>('POST', `/bugs/${id}/reopen`);

// KB
export const listKB = (projectId: string, includeDone = false) =>
  req<KnowledgeEntry[]>('GET', `/projects/${projectId}/kb${doneSuffix(includeDone)}`);
export const listKBByScratchpad = (scratchpadId: string, includeDone = false) =>
  req<KnowledgeEntry[]>('GET', `/scratchpads/${scratchpadId}/kb${doneSuffix(includeDone)}`);
export const getKB = (id: string) => req<KnowledgeEntry>('GET', `/kb/${id}`);
export const deleteKB = (id: string) => req<void>('DELETE', `/kb/${id}`);

// Update a KB entry — including its project-brain kind (glass-box Phase 5).
export const updateKB = (
  id: string,
  body: { title?: string; content?: string; status?: KnowledgeStatus; kind?: KbKind; tags?: string[] },
) => req<KnowledgeEntry>('PATCH', `/kb/${id}`, body);
export const reopenKB = (id: string) =>
  req<KnowledgeEntry>('POST', `/kb/${id}/reopen`);
// KB has no claim semantics — entries are reference material.

// Use Cases
export const listUseCases = (projectId: string, includeDone = false) =>
  req<UseCaseItem[]>('GET', `/projects/${projectId}/use-cases${doneSuffix(includeDone)}`);
export const listUseCasesByScratchpad = (scratchpadId: string, includeDone = false) =>
  req<UseCaseItem[]>('GET', `/scratchpads/${scratchpadId}/use-cases${doneSuffix(includeDone)}`);
export const getUseCase = (id: string) =>
  req<UseCaseItem>('GET', `/use-cases/${id}`);
export const updateUseCase = (
  id: string,
  body: {
    subject?: string;
    description?: string;
    role?: string;
    want?: string;
    why?: string;
    status?: UseCaseStatus;
    priority?: Priority;
    target_release?: string;
    commit_sha?: string;
    commit_tag?: string;
    implementation_date?: string;
    due_date?: string; // ISO date ("2026-06-30") or RFC3339; "" clears
    tags?: string[]; // work-record tags (canvas rework C3); replaces the full set
  },
) => req<UseCaseItem>('PATCH', `/use-cases/${id}`, body);
export const deleteUseCase = (id: string) =>
  req<void>('DELETE', `/use-cases/${id}`);
export const claimUseCase = (id: string, claimedBy: string) =>
  req<UseCaseItem>('POST', `/use-cases/${id}/claim`, { claimed_by: claimedBy });
export const reopenUseCase = (id: string) =>
  req<UseCaseItem>('POST', `/use-cases/${id}/reopen`);

// Dashboard (Phase A panel #1). Per-day created/completed counts +
// open totals for todos/bugs/use_cases.
export interface DayCount {
  date: string; // YYYY-MM-DD local
  count: number;
}
export interface DashboardThroughput {
  created_by_day: Record<string, DayCount[]>;
  completed_by_day: Record<string, DayCount[]>;
  open: Record<string, number>;
  since: string;
}
export const getDashboardThroughput = (projectId: string, days = 30) =>
  req<DashboardThroughput>(
    'GET',
    `/projects/${projectId}/dashboard/throughput?days=${days}`,
  );

// Dashboard panel #3 — indexing health. Coverage keys: items, todos,
// bugs, kb, use_cases; provenance keys mirror code_anchors.provenance.
export interface CoverageCount {
  embedded: number;
  total: number;
}
export interface DashboardIndexHealth {
  coverage: Record<string, CoverageCount>;
  chunk_count: number;
  chunk_bytes: number;
  chunk_unembedded: number;
  chunk_failed: number;
  anchor_total: number;
  anchors_by_provenance: Record<string, number>;
  dedup_flagged: number;
}
export const getDashboardIndexHealth = (projectId: string) =>
  req<DashboardIndexHealth>(
    'GET',
    `/projects/${projectId}/dashboard/index-health`,
  );

// Dashboard panels #4 + #5 — time-to-close stats and per-branch
// attribution of closed items. branches is empty (and repo_available
// false) when the project has no usable repo_root.
export interface LifecycleTypeStats {
  count: number;
  median_hours: number;
  p90_hours: number;
}
export interface DashboardBranchRow {
  branch: string;
  counts: Record<string, number>;
  total: number;
}
export interface DashboardLifecycle {
  time_to_close: Record<string, LifecycleTypeStats>;
  branches: DashboardBranchRow[];
  no_commit: Record<string, number>;
  unresolved: Record<string, number>;
  repo_available: boolean;
  default_branch?: string;
}
export const getDashboardLifecycle = (projectId: string) =>
  req<DashboardLifecycle>('GET', `/projects/${projectId}/dashboard/lifecycle`);

// Review queue (glass-box Phase 4 — attention routing): implemented items ranked
// by risk (blast-radius + verification + always-review zones). Advisory.
export interface ReviewQueueEntry {
  owner_type: string;
  id: string;
  number: number;
  subject: string;
  status: string;
  band: 'Low' | 'Medium' | 'High' | 'Critical' | 'Unknown';
  score: number;
  reasons: string[];
  needs_review: boolean;
  // unanchored: implemented but no recorded code location (broken throughline).
  unanchored?: boolean;
  in_zone: boolean;
  complexity_band?: string;
  has_verification: boolean;
  failing: boolean;
  refuted: boolean;
  reverted: boolean;
  domains?: string[];
  decision?: '' | 'approved' | 'rejected';
  note?: string;
}
// Earned trust (slice 4.2): the dial that turns risk into auto-clear vs escalate.
export interface ProjectTrust {
  tier: 'New' | 'Building' | 'Earned';
  clean: number;
  failed: number;
  total: number;
  escalate_at_or_above: string;
  explanation: string;
}
export interface ReviewQueueResponse {
  items: ReviewQueueEntry[];
  repo_configured: boolean;
  trust: ProjectTrust;
  escalate_at_or_above: string;
  // Per-area earned trust (Phase 4 competence map): top-level dir → its trust.
  domain_trust?: Record<string, ProjectTrust>;
}
export const getReviewQueue = (projectId: string) =>
  req<ReviewQueueResponse>('GET', `/projects/${projectId}/review-queue`);

// Human-floor approve/reject on an item's implementation (slice 4.3 — closes the
// loop; the decision feeds the trust dial). ownerType is todo_item|bug_item|use_case_item.
export const recordReview = (
  ownerType: string,
  id: string,
  decision: 'approved' | 'rejected',
  note = '',
) => req<{ decision: string }>('POST', `/${ownerPath[ownerType as CodeAnchorOwnerType]}/${id}/review`, { decision, note });

// The item's latest recorded decision (or null when never reviewed) — lets the
// sign-off bar render a persisted state instead of re-prompting on every mount.
export type ReviewDecision = {
  decision: 'approved' | 'rejected';
  commit_sha?: string;
  note?: string;
  created_at: string;
};
export const getLatestReview = async (
  ownerType: string,
  id: string,
): Promise<ReviewDecision | null> => {
  try {
    return await req<ReviewDecision>('GET', `/${ownerPath[ownerType as CodeAnchorOwnerType]}/${id}/review`);
  } catch (e) {
    if (/ 404\)$/.test(String(e))) return null; // never reviewed
    throw e;
  }
};

// Drift detection (glass-box Phase 5 — lineage health): where intent and code
// have come apart. untraced = commits with no intent; unbuilt = done items with
// no code; diverged = built items whose verification is red.
export interface DriftCommit {
  sha: string;
  short_sha: string;
  subject: string;
  author: string;
  date: string;
}
export interface DriftItem {
  owner_type: string;
  id: string;
  number: number;
  subject: string;
  status: string;
  reason: string;
}
export interface DriftResponse {
  untraced: DriftCommit[];
  unbuilt: DriftItem[];
  diverged: DriftItem[];
  repo_configured: boolean;
  commits_scanned: number;
}
export const getDrift = (projectId: string) =>
  req<DriftResponse>('GET', `/projects/${projectId}/drift`);

// Architecture diagram (glass-box Phase 5): LLM-drafted, human-ratified structure.
export type ArchKind = 'ui' | 'service' | 'store' | 'external' | 'component';
export interface ArchitectureNode {
  id: string;
  project_id: string;
  name: string;
  kind: ArchKind;
  description: string;
  area?: string;
  pos_x: number;
  pos_y: number;
  provenance: 'proposed' | 'ratified';
}
export interface ArchitectureEdge {
  id: string;
  project_id: string;
  from_node: string;
  to_node: string;
  label?: string;
  provenance: 'proposed' | 'ratified';
}
export interface ArchitectureDelta {
  node_status: Record<string, 'matched' | 'missing' | 'unmapped'>;
  uncovered_areas: string[];
}
// The background enrichment run behind an async draft (structure lands
// instantly; names/descriptions stream in one model call at a time).
export interface ArchDraftJob {
  running: boolean;
  total: number;
  done: number;
  scope?: string;
  eta_seconds?: number;
  error?: string;
}
export interface ArchitectureResponse {
  nodes: ArchitectureNode[];
  edges: ArchitectureEdge[];
  delta?: ArchitectureDelta;
  repo_configured?: boolean;
  draft_job?: ArchDraftJob;
  unenriched_nodes?: number;
}
export const getArchitecture = (projectId: string) =>
  req<ArchitectureResponse>('GET', `/projects/${projectId}/architecture`);
export const draftArchitecture = (projectId: string, mode: 'auto' | 'redraft' = 'auto', scope?: string) =>
  req<ArchitectureResponse>('POST', `/projects/${projectId}/architecture/draft`, scope ? { mode, scope } : { mode });
export const cancelArchDraft = (projectId: string) =>
  req<void>('POST', `/projects/${projectId}/architecture/draft/cancel`);
export const ratifyArchitecture = (projectId: string) =>
  req<ArchitectureResponse>('POST', `/projects/${projectId}/architecture/ratify`);
export const updateArchNode = (
  id: string,
  body: Partial<{ name: string; kind: ArchKind; description: string; area: string; pos_x: number; pos_y: number; provenance: 'proposed' | 'ratified' }>,
) => req<ArchitectureNode>('PATCH', `/architecture/nodes/${id}`, body);
export const deleteArchNode = (id: string) => req<void>('DELETE', `/architecture/nodes/${id}`);

// Data-model / ER diagram (UC-45): deterministic, computed on demand from the
// project's SQL schema. Relations are declared FOREIGN KEYs (inferred=false) or
// *_id naming-convention guesses (inferred=true).
export interface DataModelColumn {
  name: string;
  type: string;
  pk: boolean;
  nullable: boolean;
  fk?: { table: string; column: string };
  // Provenance: the source file whose statement introduced this column.
  added_in?: string;
}
export interface DataModelTable {
  name: string;
  columns: DataModelColumn[];
  // Provenance: the file that CREATE'd the table, and later files that altered it.
  defined_in?: string;
  modified_by?: string[];
}
export interface DataModelRelation {
  from_table: string;
  from_col: string;
  to_table: string;
  to_col: string;
  inferred: boolean;
}
export interface DataModelResponse {
  tables: DataModelTable[];
  relations: DataModelRelation[];
  sources_parsed: string[];
  warnings: string[];
  repo_configured: boolean;
}
export const getDataModel = (projectId: string) =>
  req<DataModelResponse>('GET', `/projects/${projectId}/datamodel`);

// Activity log (item-editing redesign): append a `note` (markdown body) to a
// work item's timeline. Reads reuse getItemLineage (merged source + derived).
const logPrefixByOwner: Record<string, string> = {
  todo_item: 'todos',
  bug_item: 'bugs',
  use_case_item: 'use-cases',
  knowledge_entry: 'kb',
};
export const getActivityLog = (ownerType: string, ownerId: string) =>
  req<ItemEvent[]>('GET', `/${logPrefixByOwner[ownerType] ?? ownerType}/${ownerId}/log`);
export const appendNote = (ownerType: string, ownerId: string, text: string) =>
  req<ItemEvent>('POST', `/${logPrefixByOwner[ownerType] ?? ownerType}/${ownerId}/notes`, { text });
// Canvas "pulse": the latest note per owner across the store. The client maps
// owners (source item + its derived item) to each card.
export const getLatestNotes = () => req<{ notes: ItemEvent[] }>('GET', '/latest-notes');

// Governance (glass-box Phase 6): the enforcement-level policy view.
export type EnforcementLevel = 'off' | 'flag' | 'gate' | 'hard_block';
export interface GovernanceResponse {
  licensed: boolean;
  verification: EnforcementLevel;
  review: EnforcementLevel;
  architecture: EnforcementLevel;
  security: EnforcementLevel;
}
export const getGovernance = (projectId: string) =>
  req<GovernanceResponse>('GET', `/projects/${projectId}/governance`);

// Code anchors. ownerType is the backing table for the owner; the URL form
// mirrors the owner-specific nested routes on the server (items/todos/bugs/kb).
const ownerPath: Record<CodeAnchorOwnerType, string> = {
  scratchpad_item: 'items',
  todo_item: 'todos',
  bug_item: 'bugs',
  knowledge_entry: 'kb',
  use_case_item: 'use-cases',
};

export interface CodeAnchorInput {
  kind: CodeAnchorKind;
  path?: string;
  line_start?: number;
  line_end?: number;
  revision?: string;
  url?: string;
  label?: string;
}

export const listCodeAnchors = (ownerType: CodeAnchorOwnerType, ownerId: string) =>
  req<CodeAnchor[]>('GET', `/${ownerPath[ownerType]}/${ownerId}/code-anchors`);

// Code metrics (UC-100): churn + change-complexity for the item's commit
// anchors. Free read; returns repo_configured=false (empty payload) when the
// project has no git repo configured.
export const getCodeMetrics = (ownerType: CodeAnchorOwnerType, ownerId: string) =>
  req<CodeMetrics>('GET', `/${ownerPath[ownerType]}/${ownerId}/code-metrics`);

// Acceptance criteria (glass-box Phase 2). Reads free; writes via write-auth.
export const listAcceptanceCriteria = (ownerType: CodeAnchorOwnerType, ownerId: string) =>
  req<AcceptanceCriterion[]>('GET', `/${ownerPath[ownerType]}/${ownerId}/acceptance-criteria`);

export const createAcceptanceCriterion = (
  ownerType: CodeAnchorOwnerType,
  ownerId: string,
  body: { text: string; verification_kind?: VerificationKind; state?: CriterionState },
) => req<AcceptanceCriterion>('POST', `/${ownerPath[ownerType]}/${ownerId}/acceptance-criteria`, body);

export const updateAcceptanceCriterion = (
  id: string,
  body: { text?: string; state?: CriterionState; verification_kind?: VerificationKind; position?: number },
) => req<AcceptanceCriterion>('PATCH', `/acceptance-criteria/${id}`, body);

export const deleteAcceptanceCriterion = (id: string) =>
  req<void>('DELETE', `/acceptance-criteria/${id}`);

// Draft with AI — starts a BACKGROUND job (202) and returns immediately; the
// draft runs server-side on context.Background so closing the modal / reloading
// / closing the tab can't cancel it. The criteria appear via the normal GET once
// the job finishes (it fires an ItemsChanged SSE). 503 if no model. Poll
// listDraftingCriteria (or watch the SSE refresh) to know when it's done.
export const draftAcceptanceCriteria = (ownerType: CodeAnchorOwnerType, ownerId: string) =>
  req<{ status: string }>('POST', `/${ownerPath[ownerType]}/${ownerId}/acceptance-criteria/draft`);

// The owner ids currently mid-draft, for the "drafting…" indicator on cards + in
// the modal. Returns the raw refs; callers usually build a Set of owner_id.
export const listDraftingCriteria = () =>
  req<{ owner_type: string; owner_id: string }[]>('GET', '/acceptance-criteria/drafting');

// Verification (glass-box Phase 3): the deterministic layer's results + manual
// re-run. List is a free read; trigger re-runs the project's configured check at
// the item's latest recorded commit (write-auth) and returns the running row.
export const getVerification = (ownerType: CodeAnchorOwnerType, ownerId: string) =>
  req<VerificationResponse>('GET', `/${ownerPath[ownerType]}/${ownerId}/verification`);

// useHead: run against the project's current HEAD when the item has no recorded
// implementation commit (explicit, caveated fallback). The result records the
// HEAD sha it tested against; the item's implementation commit is left untouched.
export const triggerVerification = (
  ownerType: CodeAnchorOwnerType,
  ownerId: string,
  useHead = false,
) => req<VerificationResult[]>('POST', `/${ownerPath[ownerType]}/${ownerId}/verify${useHead ? '?at=head' : ''}`);

// Installed local models, for the reviewer-model picker. Returns [] when Ollama
// is unreachable so the UI degrades to a plain text field.
export const listOllamaModels = () =>
  req<{ models: string[] }>('GET', '/ollama/models').then((r) => r.models);

export const createCodeAnchor = (
  ownerType: CodeAnchorOwnerType,
  ownerId: string,
  body: CodeAnchorInput,
) => req<CodeAnchor>('POST', `/${ownerPath[ownerType]}/${ownerId}/code-anchors`, body);

export const updateCodeAnchor = (id: string, body: CodeAnchorInput) =>
  req<CodeAnchor>('PATCH', `/code-anchors/${id}`, body);

export const deleteCodeAnchor = (id: string) =>
  req<void>('DELETE', `/code-anchors/${id}`);

export const listProjectAnchorsByPath = (projectId: string, path: string) => {
  const params = new URLSearchParams({ path });
  return req<CodeAnchorWithOwner[]>('GET', `/projects/${projectId}/code-anchors?${params}`);
};

export const listScratchpadAnchors = (scratchpadId: string) =>
  req<CodeAnchor[]>('GET', `/scratchpads/${scratchpadId}/code-anchors`);

// Semantic search across the project. Embeds q via Ollama (server side),
// scans every embedded item, returns the top hits ranked by cosine
// similarity. Empty array when nothing crosses the relevance threshold.
export type SearchHitKind =
  | 'scratchpad_item'
  | 'todo_item'
  | 'bug_item'
  | 'knowledge_entry'
  | 'use_case_item';

export interface SearchHit {
  kind: SearchHitKind;
  id: string;
  title: string;
  snippet: string;
  /** Originating scratchpad — present for scratchpad items always, and for
   *  derived items (todos/bugs/etc.) when their source lineage is intact. */
  scratchpad_id?: string;
  scratchpad_name?: string;
  score: number;
}

export interface SearchResponse {
  hits: SearchHit[];
  scanned: number;
}

export const search = (projectId: string, q: string, limit = 20) =>
  req<SearchResponse>('POST', '/search', { q, project_id: projectId, limit });

// Q&A — retrieval-augmented answer over items + code chunks.
export type QACitationKind =
  | 'scratchpad_item'
  | 'todo_item'
  | 'bug_item'
  | 'knowledge_entry'
  | 'use_case_item'
  | 'code_chunk';

export interface QACitation {
  marker: string; // "item:abc" / "code:def"
  kind: QACitationKind;
  id: string;
  title: string;
  file_path?: string;
  line_start?: number;
  line_end?: number;
  scratchpad_id?: string;
}

export interface QAResponse {
  answer: string;
  citations: QACitation[];
  used_items: number;
  used_chunks: number;
}

export const qa = (projectId: string, q: string) =>
  req<QAResponse>('POST', '/qa', { q, project_id: projectId });

// Files — the project's configured git worktree surface.
export interface FileTreeResponse {
  paths: string[];
}

export interface FileContentResponse {
  path: string;
  revision: string; // "working" when working-copy
  content: string;
  binary: boolean;
  mtime?: string; // RFC3339Nano; only set for working-copy reads
}

export interface FileMtimeResponse {
  path: string;
  mtime: string;
}

export interface CommitInfo {
  sha: string;
  short_sha: string;
  author: string;
  email: string;
  date: string;
  subject: string;
}

export const listFiles = (projectId: string) =>
  req<FileTreeResponse>('GET', `/projects/${projectId}/files`);

export const fileContent = (projectId: string, path: string, revision?: string) => {
  const params = new URLSearchParams({ path });
  if (revision) params.set('revision', revision);
  return req<FileContentResponse>('GET', `/projects/${projectId}/files/content?${params}`);
};

export const fileCommits = (projectId: string, path: string, limit = 50) => {
  const params = new URLSearchParams({ path, limit: String(limit) });
  return req<CommitInfo[]>('GET', `/projects/${projectId}/files/commits?${params}`);
};

export const fileMtime = (projectId: string, path: string) => {
  const params = new URLSearchParams({ path });
  return req<FileMtimeResponse>('GET', `/projects/${projectId}/files/mtime?${params}`);
};

// Export — zip bundle download. Server sets Content-Disposition with a
// suggested filename; we honor it and trigger a browser download via a
// transient anchor. Throws on non-2xx.

function parseFilename(header: string | null, fallback: string): string {
  if (!header) return fallback;
  // Prefer RFC 5987 filename* (UTF-8) when present; fall back to plain filename.
  const star = /filename\*=UTF-8''([^;]+)/i.exec(header);
  if (star) return decodeURIComponent(star[1].trim());
  const plain = /filename="?([^";]+)"?/i.exec(header);
  return plain ? plain[1].trim() : fallback;
}

async function downloadBundle(path: string, fallbackName: string): Promise<void> {
  activity.begin();
  let res: Response;
  try {
    res = await fetch(BASE + path);
  } finally {
    activity.end();
  }
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`Export failed: ${res.status} ${text}`);
  }
  const blob = await res.blob();
  const filename = parseFilename(res.headers.get('Content-Disposition'), fallbackName);
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  // Defer revoke so the click finishes navigating; instant revoke can race.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

export const exportProject = (projectId: string) =>
  downloadBundle(`/projects/${projectId}/export`, 'codedistill-project.zip');

export const exportScratchpad = (scratchpadId: string) =>
  downloadBundle(`/scratchpads/${scratchpadId}/export`, 'codedistill-scratchpad.zip');

// Import — POST a previously-exported zip. Always creates a new project
// named "<original> (imported)"; never merges into existing data. The
// server returns the new project plus a summary of what landed.
export interface ImportCounts {
  scratchpads: number;
  items: number;
  todos: number;
  bugs: number;
  kb: number;
  anchors: number;
}
export interface ImportResult {
  project: Project;
  counts: ImportCounts;
}

export async function importBundle(file: File): Promise<ImportResult> {
  const fd = new FormData();
  fd.append('bundle', file);
  activity.begin();
  let res: Response;
  try {
    res = await fetch(BASE + '/import', { method: 'POST', body: fd });
  } finally {
    activity.end();
  }
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`Import failed: ${res.status} ${text}`);
  }
  return res.json() as Promise<ImportResult>;
}

// Settings (Phase 2 Stage 3 — generic per-user / per-project key/value).
// Values are arbitrary JSON; clients can store primitives or structured
// objects under any key. The list endpoint returns a {key: value} map; the
// get endpoint returns a richer SettingResponse with updated_at.

export interface SettingResponse {
  key: string;
  value: unknown;
  updated_at: string;
}


/** Resolves to undefined when the key has never been set — the server answers
 *  204 No Content rather than 404, because asking for an unwritten setting is
 *  not an error. Callers supply their own default (CE-review item 43). */
export const getUserSetting = (userId: string, key: string) =>
  req<SettingResponse | undefined>('GET', `/users/${userId}/settings/${encodeURIComponent(key)}`);

export const setUserSetting = (userId: string, key: string, value: unknown) =>
  req<SettingResponse>('PUT', `/users/${userId}/settings/${encodeURIComponent(key)}`, { value });

export const deleteUserSetting = (userId: string, key: string) =>
  req<void>('DELETE', `/users/${userId}/settings/${encodeURIComponent(key)}`);


export const getProjectSetting = (projectId: string, key: string) =>
  req<SettingResponse | undefined>('GET', `/projects/${projectId}/settings/${encodeURIComponent(key)}`);

export const setProjectSetting = (projectId: string, key: string, value: unknown) =>
  req<SettingResponse>('PUT', `/projects/${projectId}/settings/${encodeURIComponent(key)}`, { value });

export const deleteProjectSetting = (projectId: string, key: string) =>
  req<void>('DELETE', `/projects/${projectId}/settings/${encodeURIComponent(key)}`);

// Example verification commands for the project's detected language (Go/Java/
// Node/Python/…), derived from repo_root marker files. `language === ''` means
// undetected — the UI falls back to neutral placeholder hints.
export interface VerifyDefaults {
  language: string;
  test: string;
  lint: string;
  types: string;
  sast: string;
  vuln: string;
}
export const getVerifyDefaults = (projectId: string) =>
  req<VerifyDefaults>('GET', `/projects/${projectId}/verify-defaults`);

// --- Code Analysis (UC-14, paid `analysis`) ---
// Static analyzers (gosec/staticcheck in v1) detect; findings render on a
// results page where the user pushes them into a scratchpad or dismisses them.
export interface CodeFinding {
  id: string;
  project_id: string;
  analyzer: string;
  rule_id: string;
  severity: 'high' | 'medium' | 'low' | 'info' | string;
  file_path: string;
  line_start: number;
  line_end: number;
  title: string;
  detail: string;
  status: string;
}
export interface AnalysisScan {
  id: string;
  trigger: string;
  started_at: string;
  finished_at?: string;
  files_scanned: number;
  findings_new: number;
  findings_resolved: number;
  skipped?: string;
  error?: string;
}
export interface LatestScan {
  scan: AnalysisScan | null;
  running: boolean;
}
export const startCodeScan = (projectId: string) =>
  req<{ status: string }>('POST', `/projects/${projectId}/analysis/scan`);
export const listCodeFindings = (projectId: string) =>
  req<CodeFinding[]>('GET', `/projects/${projectId}/analysis/findings`);
export const latestCodeScan = (projectId: string) =>
  req<LatestScan>('GET', `/projects/${projectId}/analysis/scan/latest`);
export const pushCodeFinding = (findingId: string, scratchpadId: string) =>
  req<{ item_id: string }>('POST', `/analysis/findings/${findingId}/push`, { scratchpad_id: scratchpadId });
export const dismissCodeFinding = (findingId: string) =>
  req<void>('POST', `/analysis/findings/${findingId}/dismiss`);

// --- Custom fields (backlog item #1) ---
export type CustomFieldType = 'text' | 'number' | 'select' | 'date';
export type CustomFieldOwner = 'todo_item' | 'bug_item' | 'use_case_item' | 'knowledge_entry';
export interface CustomFieldDef {
  id: string;
  project_id: string;
  name: string;
  field_type: CustomFieldType;
  options: string[];
  applies_to: CustomFieldOwner[];
  position: number;
}
export interface CustomFieldView extends CustomFieldDef {
  value: string;
}
// URL prefix per owner type (matches the API routes).
export const customFieldPrefix: Record<CustomFieldOwner, string> = {
  todo_item: 'todos',
  bug_item: 'bugs',
  use_case_item: 'use-cases',
  knowledge_entry: 'kb',
};
export const listCustomFieldDefs = (projectId: string) =>
  req<CustomFieldDef[]>('GET', `/projects/${projectId}/custom-fields`);
export const createCustomFieldDef = (projectId: string, body: Partial<CustomFieldDef>) =>
  req<CustomFieldDef>('POST', `/projects/${projectId}/custom-fields`, body);
export const deleteCustomFieldDef = (id: string) =>
  req<void>('DELETE', `/custom-fields/${id}`);
// --- Skills (backlog item #16): reusable LLM instruction sets, served over MCP ---
export interface Skill {
  id: string;
  project_id: string;
  name: string;
  content: string;
  enabled: boolean;
  position: number;
  version: number;
}
export const listSkills = (projectId: string) =>
  req<Skill[]>('GET', `/projects/${projectId}/skills`);
export const createSkill = (projectId: string, body: Partial<Skill>) =>
  req<Skill>('POST', `/projects/${projectId}/skills`, body);
export const updateSkill = (id: string, body: Partial<Skill>) =>
  req<Skill>('PATCH', `/skills/${id}`, body);
export const deleteSkill = (id: string) =>
  req<void>('DELETE', `/skills/${id}`);

// Skill versioning + evidence-gated throughline tie (Pro).
// See docs/design/skill-provenance-standard.md.
export interface SkillVersion {
  id: string;
  skill_id: string;
  version: number;
  name: string;
  content: string;
  author: string;
  created_at: string;
}
// A skill version bound to a specific change, labelled by how we know it was
// used ('attested' — the agent declared it; 'retrieved' — inferred from a fetch).
export interface SkillApplication {
  id: string;
  skill_id: string;
  skill_version: number;
  owner_type: string;
  owner_id: string;
  commit_sha: string;
  evidence: 'attested' | 'retrieved';
  created_at: string;
}
export const listSkillVersions = (skillId: string) =>
  req<SkillVersion[]>('GET', `/skills/${skillId}/versions`);
// version 0 / '' = any; evidence '' = any.
export const listSkillApplications = (skillId: string, version = 0, evidence = '') =>
  req<SkillApplication[]>(
    'GET',
    `/skills/${skillId}/applications?version=${version}&evidence=${encodeURIComponent(evidence)}`,
  );
// Pass 2: remediate the changes made under a flawed skill version — flag them for
// review and/or re-run verification.
export interface RemediateResult {
  version: number;
  action: string;
  items: number;
  flagged: number;
  reverified: number;
  reverify_available: boolean;
}
export const remediateSkillVersion = (
  skillId: string,
  version: number,
  action: 'review' | 'reverify' | 'both',
) => req<RemediateResult>('POST', `/skills/${skillId}/versions/${version}/remediate`, { action });

// Summarize a web page into a new scratchpad item (summary + source link).
export const summarizeUrl = (scratchpadId: string, url: string) =>
  req<unknown>('POST', `/scratchpads/${scratchpadId}/summarize-url`, { url });

export const listItemCustomFields = (prefix: string, id: string) =>
  req<CustomFieldView[]>('GET', `/${prefix}/${id}/custom-fields`);
export const setItemCustomField = (prefix: string, id: string, fieldId: string, value: string) =>
  req<unknown>('PUT', `/${prefix}/${id}/custom-fields/${fieldId}`, { value });

// --- MCP export (#9 outbound client, v0.8.2) ---
//
// The configure wizard probes a candidate destination and asks the
// LLM to suggest a mapping. Persistence is via the generic
// user_settings endpoints under the key mcp.export.<short>.

export interface McpCredentials {
  type: 'bearer' | 'header';
  name?: string; // header name when type === 'header'
  token: string;
}

/** Mirrors mcpclient.Endpoint in internal/mcpclient/client.go.
 *  `transport` absent means http, so endpoints saved before stdio existed keep
 *  working. url/credentials/headers apply to http; command/args/env to stdio. */
export interface McpEndpoint {
  transport?: 'http' | 'stdio';
  url?: string;
  credentials?: McpCredentials;
  headers?: Record<string, string>;
  command?: string;
  args?: string[];
  env?: string[];
}

export interface McpTool {
  name: string;
  description: string;
  input_schema: unknown;
}

export type McpItemType = 'todo' | 'bug' | 'kb' | 'use_case';
export type McpOperation = 'create' | 'update' | 'status_change' | 'delete';
export type McpMapping = Record<McpOperation, string>;

export interface McpExportConfig {
  endpoint: McpEndpoint;
  mapping: McpMapping;
}

export const discoverMcpDestination = (endpoint: McpEndpoint) =>
  req<{ tools: McpTool[] }>('POST', '/mcp-export/discover', { endpoint });

export const suggestMcpMapping = (itemType: McpItemType, catalog: McpTool[]) =>
  req<{ mapping: McpMapping }>('POST', '/mcp-export/suggest-mapping', {
    item_type: itemType,
    catalog,
  });

export const mcpExportSettingsKey = (itemType: McpItemType) =>
  `mcp.export.${itemType}`;

// Filesystem directory browse for the repo-root picker (local
// single-user only — see fs_browse.go's security note).
export interface FsBrowseDir {
  name: string;
  is_git_repo: boolean;
}
export interface FsBrowse {
  path: string;
  parent?: string;
  dirs: FsBrowseDir[];
}
export const fsBrowse = (path?: string) =>
  req<FsBrowse>('GET', `/fs/browse${path ? `?path=${encodeURIComponent(path)}` : ''}`);

// Bulk-push pre-existing local-only items of one type to the freshly
// configured destination (the cache otherwise only populates on new
// writes). skipped = items that already had sync state.
// Duplicate intelligence — on-demand cluster + cross-pad report (paid).
export type DedupScope = 'scratchpad' | 'project' | 'global';
export interface CrossPadMatch {
  a_id: string; a_title: string; a_pad: string;
  b_id: string; b_title: string; b_pad: string;
  score: number;
}
export interface DedupScanResult {
  items_flagged: number;
  pads_scanned: number;
  cross_pad: CrossPadMatch[];
  truncated: boolean;
}
export const dedupScan = (scope: DedupScope, scopeId?: string) =>
  req<DedupScanResult>('POST', '/dedup/scan', { scope, scope_id: scopeId ?? '' });

export const migrateMcpExport = (itemType: McpItemType) =>
  req<{ enqueued: number; skipped: number }>('POST', '/mcp-export/migrate', {
    item_type: itemType,
  });

// ── Model providers and roles ────────────────────────────────────────────────
// A provider is a connection; a role is a job. The API never returns an API
// key — has_api_key is all the UI gets, and all it needs.

/** OPEN vocabulary, matching the database (migration 0067 dropped the CHECK).
 *  Go validates the value; a closed union here silently mislabelled every
 *  adapter added after it was written — Anthropic and Gemini were both cast
 *  through it as 'openai'. The known values are documented, not enforced:
 *  'ollama' | 'openai' | 'anthropic' | 'gemini'. */
export type ModelProtocol = string;

export interface ModelProvider {
  id: string;
  name: string;
  protocol: ModelProtocol;
  endpoint: string;
  model: string;
  has_api_key: boolean;
  context_tokens: number;
  is_local: boolean;
  enabled: boolean;
  last_ok_at?: string;
  last_error?: string;
}

/** Five outcomes, not a boolean. context_short is the dangerous one:
 *  reachable, generating, and silently truncating long prompts. */
export type ProbeOutcome =
  | 'unreachable' | 'model_unusable' | 'working' | 'verified' | 'context_short';

export interface ProbeResult {
  outcome: ProbeOutcome;
  detail: string;
  reachable: boolean;
  can_generate: boolean;
  context_asked?: number;
  context_seen?: number;
  latency_ms: number;
  model?: string;
}

export interface WorkerBindings {
  /** worker type -> provider id */
  workers: Record<string, string>;
  known_types: string[];
  /** Solutioner and challenger on one provider: a challenger that shares the
   *  solutioner's blind spots. A warning, never a block. */
  epistemic_pair_shared: boolean;
}

export const listModelProviders = () =>
  req<ModelProvider[]>('GET', '/model-providers');

export const createModelProvider = (p: Partial<ModelProvider> & { api_key?: string }) =>
  req<ModelProvider>('POST', '/model-providers', p);

export const updateModelProvider = (id: string, p: Partial<ModelProvider> & { api_key?: string }) =>
  req<void>('PATCH', `/model-providers/${id}`, p);

export const deleteModelProvider = (id: string) =>
  req<void>('DELETE', `/model-providers/${id}`);

export const testModelProvider = (id: string) =>
  req<ProbeResult>('POST', `/model-providers/${id}/test`);

export const listWorkflowWorkers = (projectId?: string) =>
  req<WorkerBindings>('GET', `/workflow-workers${projectId ? `?project_id=${encodeURIComponent(projectId)}` : ''}`);

export const setWorkerModel = (workerType: string, providerId: string, projectId?: string) =>
  req<void>('PUT', `/workflow-workers/${workerType}`, {
    provider_id: providerId,
    project_id: projectId ?? '',
  });

export interface ModelSummary {
  id: string;
  context_length?: number;
  parameter_size?: string;
  family?: string;
}

export interface ModelDescription {
  context_length?: number;
  family?: string;
  parameter_size?: string;
  quantization?: string;
  capabilities?: string[];
  /** How this was learned: a fact the server stated, not a number typed by a
   *  human. Absent when the provider would not say. */
  source?: string;
}

export interface DiscoverResult {
  models: ModelSummary[];
  described?: ModelDescription;
  error?: string;
  describe_error?: string;
}

/** Query a provider — saved or not — for the models it offers, and optionally
 *  describe one. Pass provider_id to reuse a stored credential the browser
 *  never sees. */
export const discoverModels = (body: {
  provider_id?: string; protocol?: string; endpoint?: string;
  api_key?: string; model?: string;
}) => req<DiscoverResult>('POST', '/model-providers/discover', body);

export interface Vendor {
  id: string;
  name: string;
  protocol?: string;
  default_endpoint?: string;
  needs_key: boolean;
  lists_models: boolean;
  /** False means a typed context window is user-supplied, not verified — the
   *  UI must not present it as a fact the provider stated. */
  reports_context: boolean;
  /** False means this product cannot speak the vendor's protocol at all.
   *  Listed anyway, with a note saying what to do instead. */
  supported: boolean;
  note?: string;
}

export const listVendors = () => req<Vendor[]>('GET', '/model-vendors');

// ── Jobs ─────────────────────────────────────────────────────────────────────

export interface Job {
  id: string;
  project_id: string;
  /** The workflow: 'decompose', 'arch_draft', … */
  type: string;
  scope_label?: string;
  /** paused is distinct from cancelled (abandoned) and interrupted (the
   *  process died): a human stopped it and expects it back. */
  status: 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted' | 'paused';
  scope_kind?: string;
  scope_id?: string;
  phase?: string;
  done: number;
  total: number;
  /** -1 when the total is not yet known. Render indeterminate, never 0: a bar
   *  pinned at zero reads as broken, one that jumps reads as a lie. */
  percent: number;
  eta_seconds: number;
  elapsed_seconds: number;
  worker_type?: string;
  provider_name?: string;
  provider_local?: boolean;
  model?: string;
  tokens_in: number;
  tokens_out: number;
  error?: string;
  started_at: string;
  finished_at?: string;

  /** What the run produced, for workflows whose output waits on a person.
   *  A decompose that "succeeded" has not finished doing anything useful
   *  until someone has judged what it found. */
  proposals?: number;
  awaiting_review?: number;
}

export const listJobs = (projectId: string, status?: string, limit = 50) =>
  req<Job[]>(
    'GET',
    `/jobs?project_id=${encodeURIComponent(projectId)}` +
      (status ? `&status=${status}` : '') +
      `&limit=${limit}`,
  );

export const cancelJob = (id: string) => req<void>('POST', `/jobs/${id}/cancel`);

export interface JobPhase {
  id: string;
  ordinal: number;
  phase: string;
  worker_type?: string;
  /** The exchange number when workers take turns. 0 for a single-pass phase. */
  round?: number;
  done: number;
  total: number;
  tokens_in: number;
  tokens_out: number;
  error?: string;
  started_at: string;
  finished_at?: string;
  seconds: number;
  running: boolean;
}

/** What a phase usually costs, as a median over SUCCEEDED runs. This is what
 *  turns "12 minutes elapsed" into a verdict. */
export interface PhaseNorm {
  phase: string;
  median_sec: number;
  runs: number;
}

export interface JobDetail {
  job: Job;
  phases: JobPhase[];
  norms: PhaseNorm[];
}

export const getJob = (id: string) => req<JobDetail>('GET', `/jobs/${id}`);

/** Pausing is not instant: a model call is atomic, so the stop lands at the
 *  next checkpoint. Refused (409) for a workflow that cannot resume. */
export const pauseJob = (id: string) =>
  req<{ status: string; note: string }>('POST', `/jobs/${id}/pause`);

export interface WorkflowStep {
  ordinal: number;
  worker_type: string;
  label?: string;
  needs_context?: number;
  provider_id?: string;
  provider_name?: string;
  provider_local?: boolean;
  context_tokens?: number;
}

export interface Workflow {
  id: string;
  name: string;
  description?: string;
  builtin: boolean;
  /** Whether a stopped run continues where it left off. Drives whether Pause
   *  is offered at all — a Pause that silently means Cancel is worse than none. */
  resumable: boolean;
  enabled: boolean;
  steps: WorkflowStep[];
  /** What the submission dialog asks for. Declared as data, so a workflow
   *  nobody has written yet gets a working form with no front-end change. */
  params: WorkflowParam[];
}

export interface WorkflowParam {
  key: string;
  label: string;
  help?: string;
  /** Open vocabulary. An unrecognised type renders as text rather than being
   *  dropped: a param that silently vanishes submits a job missing an argument. */
  type: string;
  required: boolean;
  default?: string;
  options?: { value: string; label: string }[];
  multiple?: boolean;
}

export const listWorkflows = () => req<Workflow[]>('GET', '/workflows');

export interface SubmitResult {
  job_id: string;
  label?: string;
  model?: string;
  provider?: string;
  /** True when the chosen model is not on this machine. Surfaced every time:
   *  a document leaving the machine must never be discovered afterwards. */
  leaves_machine?: boolean;
  note?: string;
}

export const submitJob = (workflowId: string, projectId: string, params: Record<string, string>) =>
  req<SubmitResult>('POST', '/jobs', {
    workflow_id: workflowId, project_id: projectId, params,
  });
export const setStepProvider = (workflowId: string, ordinal: number, providerId: string) =>
  req<void>('PUT', `/workflows/${workflowId}/steps/${ordinal}/provider`, {
    provider_id: providerId,
  });


// ── Decompose review ────────────────────────────────────────────────────────
//
// A run produces PROPOSALS, never items. Turning one into work is a human act,
// which until now could only be performed from the command line.

export interface DecomposeRunSummary {
  job_id: string;
  source_item_id: string;
  source_label: string;
  sentences: number;
  model?: string;
  created_at: string;
  total: number;
  pending: number;
  accepted: number;
  rejected: number;
  linked: number;
}

export interface DecomposeProposal {
  id: string;
  job_id: string;
  kind: string;
  subject: string;
  body?: string;
  origin: string;
  corroborated: boolean;
  external_ref?: string;
  priority?: string;
  /** Line spans in the source document. This is the proposal's whole claim to
   *  legitimacy — computed before any model ran, so a citation cannot be
   *  invented. */
  lines: [number, number][];
  status: 'pending' | 'accepted' | 'rejected' | 'linked';
  created_item_id?: string;
  linked_item_id?: string;
  reject_reason?: string;
}

export interface DecomposeRunDetail {
  run: DecomposeRunSummary & {
    corroborated: number;
    table_only: number;
    prose_only: number;
    tables_read: number;
    declined_nodes: number;
  };
  proposals: DecomposeProposal[];
  /** The document itself, so a citation can be read rather than trusted.
   *  Absent when the source item has been deleted. */
  source_lines?: string[];
}

export const listDecomposeRuns = (projectId: string, limit = 50) =>
  req<DecomposeRunSummary[]>('GET', `/decompose/runs?project_id=${encodeURIComponent(projectId)}&limit=${limit}`);

export const getDecomposeRun = (jobId: string) =>
  req<DecomposeRunDetail>('GET', `/decompose/runs/${jobId}`);

export interface Decision {
  id: string;
  status: 'accepted' | 'rejected' | 'linked';
  item_id?: string;
  reason?: string;
}

export interface BatchResult {
  accepted: number;
  rejected: number;
  linked: number;
  items: Record<string, string>;
  failed: { id: string; subject?: string; error: string }[];
}

/** Applies a whole review in one call. Forty separate requests would make a
 *  partial failure invisible; this returns a verdict per proposal. */
export const decideProposals = (jobId: string, decisions: Decision[]) =>
  req<BatchResult>('POST', `/decompose/runs/${jobId}/decide`, { decisions });
