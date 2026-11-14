<!-- =============================================================================
  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.

  CodeDistill

  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
  Public License v3.0 (see the LICENSE file) and, separately, a commercial
  license available from Nyx Software, Inc. Use outside the terms of one of those
  licenses is prohibited.

  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
============================================================================= -->

<script lang="ts">
  import { onMount } from 'svelte';
  import { collapseOnOverflow } from './lib/overflowCollapse';
  import * as api from './lib/api';
  import { syncDrafting } from './lib/draftingStore';
  import { loadUserBool, saveUserBool, loadUserNum, saveUserNum, LOCAL_USER } from './lib/userSettings';
  import { loadSubmitShortcut } from './lib/submitShortcut';
  import { SSEChannel, EventNames } from './lib/events';
  import { initTheme } from './lib/theme';
  import { initFonts } from './lib/fonts';
  import { useRegisterSW } from 'virtual:pwa-register/svelte';
  import { isTodoDone, isBugDone, isKnowledgeDone, isUseCaseDone } from './lib/lifecycle';
  import type {
    ScratchpadItem,
    TodoItem,
    BugItem,
    KnowledgeEntry,
    UseCaseItem,
    Scratchpad,
    Project,
  } from './lib/types';
  import { rankFor, type DerivedStatus } from './lib/types';
  import ScratchpadPane from './components/ScratchpadPane.svelte';
  import Drawer from './components/Drawer.svelte';
  import DialogHost from './components/DialogHost.svelte';
  import SetupScreen from './components/SetupScreen.svelte';
  import NeedsReviewPanel from './components/NeedsReviewPanel.svelte';
  import Modal from './components/Modal.svelte';
  import Settings from './components/Settings.svelte';
  import ProfileMenu from './components/ProfileMenu.svelte';
  import AdminConsole from './components/AdminConsole.svelte';
  import StatsModal from './components/StatsModal.svelte';
  import SystemInfoModal from './components/SystemInfoModal.svelte';
  import { activity } from './lib/activity.svelte';
  import { uiPrefs, initUiPrefs } from './lib/uiPrefs.svelte';
  import PowerBadge from './components/PowerBadge.svelte';
  import CodeCanvas, { type CodeTab } from './components/CodeCanvas.svelte';
  import type { ChangesetPick } from './components/ChangesetPicker.svelte';
  import ContextMenu from './components/ContextMenu.svelte';
  import ChangesetPane from './components/ChangesetPane.svelte';
  import TodoDetail from './components/TodoDetail.svelte';
  import BugDetail from './components/BugDetail.svelte';
  import UseCaseDetail from './components/UseCaseDetail.svelte';
  import DockedItems from './components/DockedItems.svelte';
  import { dock, dockItem, type DockedItem } from './lib/dock.svelte';
  import KbDetail from './components/KbDetail.svelte';
  import Picker from './components/Picker.svelte';
  import Icon from './components/Icon.svelte';
  import ListsPanel from './components/ListsPanel.svelte';
  import SearchPanel from './components/SearchPanel.svelte';
  import QAPanel from './components/QAPanel.svelte';
  import DashboardPanel from './components/DashboardPanel.svelte';
  import ArchitecturePanel from './components/ArchitecturePanel.svelte';
  import DataModelPanel from './components/DataModelPanel.svelte';
  import JobMonitor from './components/JobMonitor.svelte';
  import JobSubmit from './components/JobSubmit.svelte';
  import { orderProjects } from './lib/uiPrefs.svelte';
  import ProposalReview from './components/ProposalReview.svelte';
  import PaneFrame from './components/PaneFrame.svelte';
  import MenuBar, { type Menu } from './components/MenuBar.svelte';
  import CommandPalette from './components/CommandPalette.svelte';
  import { paneDef, SLOTS, type SlotID } from './lib/layout';
  import {
    initLayout, visibleSlots, activePane, paneSize as paneWidth,
    openPane as openPaneById, closePane as closePaneById,
    draggingPane, endDrag, movePane as movePaneTo,
    setPaneSize as setPaneWidth, flushPaneSize, togglePane, isPaneOpen, paneSlot,
  } from './lib/layoutStore.svelte';
  import CodeAnalysisPanel from './components/CodeAnalysisPanel.svelte';
  import HelpPanel from './components/HelpPanel.svelte';
  import type { IconName } from './components/Icon.svelte';
  import { promptDialog } from './lib/dialog';
  import {
    registerCommands, commandsById, runCommand, isEnabled, isVisible,
    shortcutLabel, setCommandContext, installShortcuts, type Command,
  } from './lib/commands.svelte';
  import { resetLayout } from './lib/layoutStore.svelte';
  import CreditsModal from './components/CreditsModal.svelte';
  import type { CodeAnchorWithOwner, CodeAnchor } from './lib/types';

  // Drawer on the LEFT, toggled by a hamburger in the header. Three major
  // tabs: Workspace (project list + CRUD), Project (Files), Scratchpad
  // (Items / Todos / Bugs / KB / Inbox). Persisted via user_settings —
  // ui.drawer.open + ui.drawer.scope (kept the legacy key for compatibility).

  // SSE-driven refresh is the primary update path; polling is a
  // fallback for when the SSE connection drops. Cadence reflects
  // that role: 30s is fine for "the live channel is broken"
  // recovery, vs the old 3s when polling was the only signal.
  const POLL_MS = 30_000;
  // Debounce window for SSE-triggered refreshes — a burst of events
  // (e.g. agent classifying 5 items in 2 seconds) collapses to one
  // refresh.
  const SSE_DEBOUNCE_MS = 250;
  const DRAWER_KEY = 'ui.drawer.open';
  const DRAWER_WIDTH_KEY = 'ui.drawer.width';
  const ACTIVE_PROJECT_KEY = 'ui.active_project';
  const CODE_CANVAS_WIDTH_KEY = 'ui.codecanvas.width';
  // Per-project state lives at `ui.codecanvas.<projectId>`; saved on every
  // tab/visibility change, loaded on project switch.
  const CODE_CANVAS_KEY_PREFIX = 'ui.codecanvas.';

  const DRAWER_WIDTH_DEFAULT = 380;
  const DRAWER_WIDTH_MIN = 120;
  const DRAWER_WIDTH_MAX = 800;
  const CODE_CANVAS_WIDTH_DEFAULT = 600;
  const CODE_CANVAS_WIDTH_MIN = 240;
  const CODE_CANVAS_WIDTH_MAX = 1600;


  let buildInfo = $state<api.VersionInfo | null>(null);

  // License state drives the renewal banner + hides gated affordances
  // (e.g. the matcher's "Scan now"). null until the fetch resolves —
  // affordances stay visible during that window to avoid a flash of
  // missing UI on licensed installs; the server still enforces.
  let license = $state<api.LicenseInfo | null>(null);
  // Who the app is acting as (attribution). Single-user = the local user, named
  // after the license holder.
  let currentUser = $state<api.Me | null>(null);
  let licenseBannerDismissed = $state(false);
  let expiryNoticeDismissed = $state(false);
  const licensed = (feature: string): boolean =>
    license === null || license.features.includes(feature);
  // Days until a still-valid license expires (null when not applicable). Drives
  // the heads-up notice that fires BEFORE expiry — distinct from the grace/
  // expired banner that fires after.
  const daysToExpiry = $derived.by(() => {
    if (!license || license.state !== 'valid' || !license.expires_at) return null;
    const ms = new Date(license.expires_at).getTime() - Date.now();
    if (Number.isNaN(ms)) return null;
    return Math.ceil(ms / 86_400_000);
  });
  // Warn inside the renewal window (30 days). In multi-user only the admin sees
  // the renew CTA; other members get an FYI to nudge their admin.
  const EXPIRY_WARN_DAYS = 30;
  const expiringSoon = $derived(daysToExpiry !== null && daysToExpiry <= EXPIRY_WARN_DAYS && daysToExpiry >= 0);
  let projects = $state<Project[]>([]);
  let activeProjectId = $state<string>('');
  let scratchpads = $state<Scratchpad[]>([]);
  // Empty until refresh() resolves a real id. The literal 'default' used to
  // leak across projects: if the active project had zero scratchpads but a
  // 'default' scratchpad existed under another project, listItems('default')
  // would silently return that other project's items.
  let activeScratchpadId = $state<string>('');
  let items = $state<ScratchpadItem[]>([]);

  // Scratchpad-scoped derived data — fetched alongside items each refresh.
  let padTodos = $state<TodoItem[]>([]);
  let padBugs = $state<BugItem[]>([]);
  let padKb = $state<KnowledgeEntry[]>([]);
  let padUseCases = $state<UseCaseItem[]>([]);
  // Canvas rework slice B: latest note per owner (source item + its derived
  // item), used for the card "pulse" line.
  let latestNotes = $state<import('./lib/api').ItemEvent[]>([]);

  // Header tally. A single partition of the scratchpad cards so the numbers
  // reconcile (total == inbox + todos + bugs + kb + use-cases + other).
  // Group frames are containers, not cards, so they're excluded. Buckets are
  // mutually exclusive: a card awaiting triage counts as inbox regardless of
  // the category the classifier proposed; everything else falls to its
  // effective category, with the remainder unclassified ("other"). This is
  // the same single-array logic the FilterPanel chips use — unlike the old
  // banner, which summed a raw card count against separate derived-entity
  // tables (double-counting) plus a global cross-pad inbox.
  let bannerCounts = $derived.by(() => {
    const cat = (i: ScratchpadItem) =>
      i.classification_override || i.proposed_category || '';
    let total = 0, inbox = 0, todos = 0, bugs = 0, kb = 0, useCases = 0, other = 0;
    for (const i of items) {
      if (i.content_type === 'group') continue;
      total++;
      if (i.classification_state === 'pending-review') { inbox++; continue; }
      switch (cat(i)) {
        case 'todo': todos++; break;
        case 'bug': bugs++; break;
        case 'kb': kb++; break;
        case 'use_case': useCases++; break;
        default: other++; break;
      }
    }
    return { total, inbox, todos, bugs, kb, useCases, other };
  });

  // Set of scratchpad item ids whose every derived item (todo/bug/kb/
  // use_case) is in a terminal state. ScratchpadGrid uses this to fade
  // and strikethrough cards whose work is complete. Empty = not done
  // (or no derived items at all — an unclassified card isn't "done").
  // Per-card derived-item status for the canvas chip (Backlog bug #3 /
  // UC-13). A scratchpad item derives at most one item, so the map is
  // 1:1: source item id -> the derived item's kind + status.
  // Canvas rework slice A: also carry the derived work item's subject so the
  // card can reflect the living work item's title, not the raw paste's first
  // line. KB uses `title`; the others use `subject`.
  // `rank` is the comparable priority position (rankFor) so views can sort a
  // mixed list without knowing that bugs rank by severity while todos and use
  // cases rank by priority. KB never ranks.
  let derivedStatusBySource = $derived.by<Map<string, DerivedStatus>>(() => {
    const m = new Map<string, DerivedStatus>();
    for (const t of padTodos) if (t.source_item_id) m.set(t.source_item_id, { kind: 'todo', status: t.status, subject: t.subject, number: t.number, rank: rankFor('todo', t.priority) });
    for (const b of padBugs) if (b.source_item_id) m.set(b.source_item_id, { kind: 'bug', status: b.status, subject: b.subject, number: b.number, rank: rankFor('bug', b.severity) });
    for (const k of padKb) if (k.source_item_id) m.set(k.source_item_id, { kind: 'kb', status: k.status, subject: k.title, body: k.content, rank: rankFor('kb', undefined) });
    for (const u of padUseCases) if (u.source_item_id) m.set(u.source_item_id, { kind: 'use case', status: u.status, subject: u.subject, number: u.number, rank: rankFor('use case', u.priority) });
    return m;
  });

  // Canvas rework slice B: the newest note across a source item AND its derived
  // work item → the card's pulse line. ISO timestamps compare lexicographically.
  let latestNoteBySource = $derived.by<Map<string, string>>(() => {
    const byOwner = new Map<string, { summary: string; at: string }>();
    for (const n of latestNotes) byOwner.set(n.owner_type + ':' + n.owner_id, { summary: n.summary, at: n.created_at });
    const best = new Map<string, { summary: string; at: string }>();
    const consider = (sourceId: string | undefined, ownerType: string, ownerId: string) => {
      if (!sourceId) return;
      const n = byOwner.get(ownerType + ':' + ownerId);
      if (!n) return;
      const cur = best.get(sourceId);
      if (!cur || n.at > cur.at) best.set(sourceId, n);
    };
    for (const it of items) consider(it.id, 'scratchpad_item', it.id);
    for (const t of padTodos) consider(t.source_item_id, 'todo_item', t.id);
    for (const b of padBugs) consider(b.source_item_id, 'bug_item', b.id);
    for (const k of padKb) consider(k.source_item_id, 'knowledge_entry', k.id);
    for (const u of padUseCases) consider(u.source_item_id, 'use_case_item', u.id);
    const out = new Map<string, string>();
    for (const [sid, n] of best) out.set(sid, n.summary);
    return out;
  });

  // Canvas rework C3: source item id -> its derived work item's tags. Tags
  // moved off the immutable source onto the living work record, so the canvas
  // card reflects the work item's tags (editable in the detail modal), not the
  // frozen capture. Empty arrays are skipped so the card shows nothing.
  let tagsBySource = $derived.by<Map<string, string[]>>(() => {
    const m = new Map<string, string[]>();
    const put = (sid: string | undefined, tags: string[] | undefined) => {
      if (sid && tags && tags.length) m.set(sid, tags);
    };
    for (const t of padTodos) put(t.source_item_id, t.tags);
    for (const b of padBugs) put(b.source_item_id, b.tags);
    for (const k of padKb) put(k.source_item_id, k.tags);
    for (const u of padUseCases) put(u.source_item_id, u.tags);
    return m;
  });

  // UC-4 / UC-46 Calendar view: source item id -> its derived item's due_date.
  // Todos, bugs, and use-cases can each carry a due date.
  let dueDateBySource = $derived.by<Map<string, string>>(() => {
    const m = new Map<string, string>();
    for (const t of padTodos)    if (t.source_item_id && t.due_date) m.set(t.source_item_id, t.due_date);
    for (const b of padBugs)     if (b.source_item_id && b.due_date) m.set(b.source_item_id, b.due_date);
    for (const u of padUseCases) if (u.source_item_id && u.due_date) m.set(u.source_item_id, u.due_date);
    return m;
  });

  let doneSourceIds = $derived.by<Set<string>>(() => {
    const groups = new Map<string, { total: number; done: number }>();
    const tally = (sid: string | undefined, isDone: boolean) => {
      if (!sid) return;
      const g = groups.get(sid) ?? { total: 0, done: 0 };
      g.total += 1;
      if (isDone) g.done += 1;
      groups.set(sid, g);
    };
    for (const t of padTodos)    tally(t.source_item_id, isTodoDone(t.status));
    for (const b of padBugs)     tally(b.source_item_id, isBugDone(b.status));
    for (const k of padKb)       tally(k.source_item_id, isKnowledgeDone(k.status));
    for (const u of padUseCases) tally(u.source_item_id, isUseCaseDone(u.status));
    const out = new Set<string>();
    for (const [sid, g] of groups) {
      if (g.total > 0 && g.done === g.total) out.add(sid);
    }
    return out;
  });

  let activeProject = $derived(projects.find((p) => p.id === activeProjectId) ?? null);
  let activePad = $derived(scratchpads.find((s) => s.id === activeScratchpadId) ?? null);
  // Hidden easter-egg theme: scratchpad named "42" flips the app into a
  // matrix-green monospace mode. Cleared the moment you switch away.
  // Clicking the logo while it's on dismisses it for the rest of the
  // browser session (sessionStorage); refresh re-enables.
  const MATRIX_DISMISS_KEY = 'cd.matrixDismissed';
  let matrixDismissed = $state(
    typeof sessionStorage !== 'undefined' && sessionStorage.getItem(MATRIX_DISMISS_KEY) === '1'
  );
  let matrixTriggered = $derived(
    !!(activePad && activePad.name.trim().toLowerCase() === '42')
  );
  let easterTheme = $derived(matrixTriggered && !matrixDismissed ? 'theme-matrix' : '');
  function onLogoClick() {
    // Only meaningful while the theme is actually on — otherwise the
    // logo is just a logo. Keeps the "easter egg" feel: nothing happens
    // unless you've already found it.
    if (matrixTriggered && !matrixDismissed) {
      matrixDismissed = true;
      try { sessionStorage.setItem(MATRIX_DISMISS_KEY, '1'); } catch {}
    }
  }
  // If the user navigates away from the "42" scratchpad, clear the
  // dismissal flag so coming back is a fresh discovery rather than a
  // stuck-off state.
  $effect(() => {
    if (!matrixTriggered && matrixDismissed) {
      matrixDismissed = false;
      try { sessionStorage.removeItem(MATRIX_DISMISS_KEY); } catch {}
    }
  });

  // About dialog — the version chip opens it on a SINGLE click. It used to be a
  // triple-click easter egg, which is the opposite of prominent: AGPL-3.0 §13
  // requires users interacting over a network to be prominently offered the
  // Corresponding Source, and §5(d) requires interactive interfaces to show
  // Appropriate Legal Notices. CodeDistill serves its UI over HTTP, so that
  // dialog has to be reachable without knowing a secret gesture.
  let creditsOpen = $state(false);
  let errorMsg = $state<string>('');
  let hiddenCount = $derived(items.filter((i) => i.hidden).length);

  // Likewise no drawer mirror — the hamburger reads the layout. DRAWER_KEY is
  // still written so an older build downgraded to still finds its setting.
  let drawerWidth = $state(DRAWER_WIDTH_DEFAULT);
  let settingsOpen = $state(false);
  let settingsSection = $state<'appearance' | 'security'>('appearance');
  /** Security knowledge sources out of date, by name: flagged in the header
   *  until they are fresh again. */
  let securityStale = $state<string[]>([]);
  let adminOpen = $state(false);
  let statsOpen = $state(false);
  let systemOpen = $state(false);
  // First-run setup: shown until Ollama answers and both models exist, or the
  // user skips for this session. null = not needed / not yet checked.
  let setupStatus = $state<api.SetupStatus | null>(null);
  // Bumped when the settings modal closes; ScratchpadPane reloads
  // canvas-affecting preferences (type colors) off it.
  let settingsTick = $state(0);

  // Detail modal state — lifted from each Pane so code-canvas anchor
  // clicks can open a detail from outside the drawer. Exactly one detail
  // is open at a time across todo / bug / use_case. KB has no modal; we
  // route KB clicks via kbExpandRequest below.
  let openTodo = $state<TodoItem | null>(null);
  let openBug = $state<BugItem | null>(null);
  let openUseCase = $state<UseCaseItem | null>(null);
  let openKb = $state<KnowledgeEntry | null>(null);

  // Lists slide-out panel state — project-wide derived items. Data is
  // fetched from /projects/{pid}/{type} when the panel opens and on each
  // refresh tick while it stays open. KB navigation from a code-canvas
  // anchor click pops Lists open at the KB tab + sets kbExpandId.
  let listsOpen = $state(false);
  let listsActiveTab = $state<'todos' | 'bugs' | 'kb' | 'use-cases'>('todos');
  let projectTodos = $state<TodoItem[]>([]);
  let projectBugs = $state<BugItem[]>([]);
  let projectKb = $state<KnowledgeEntry[]>([]);
  let projectUseCases = $state<UseCaseItem[]>([]);

  async function loadProjectLists() {
    if (!activeProjectId) {
      projectTodos = [];
      projectBugs = [];
      projectKb = [];
      projectUseCases = [];
      return;
    }
    try {
      const [t, b, k, u] = await Promise.all([
        api.listTodos(activeProjectId),
        api.listBugs(activeProjectId),
        api.listKB(activeProjectId),
        api.listUseCases(activeProjectId),
      ]);
      projectTodos = t;
      projectBugs = b;
      projectKb = k;
      projectUseCases = u;
    } catch (e) {
      errorMsg = String(e);
    }
  }

  function openLists(tab: 'todos' | 'bugs' | 'kb' | 'use-cases' = 'todos') {
    listsActiveTab = tab;
    listsOpen = true;
    void loadProjectLists();
  }
  function closeLists() {
    listsOpen = false;
  }

  // Search panel — open/close is symmetric with Lists. Both can be open
  // simultaneously since they slide over different stacking contexts;
  // not great UX but not actively broken. v1 keeps it that way.
  let searchOpen = $state(false);
  function toggleSearch() { searchOpen = !searchOpen; }
  function closeSearch() { searchOpen = false; }

  // Search hit click handlers — fetch the full row via existing get*
  // endpoints, then route through the same modal/highlight paths the
  // code-canvas anchor click already uses.
  async function searchOpenScratchpadItem(id: string, scratchpadId: string) {
    if (scratchpadId && scratchpadId !== activeScratchpadId) {
      await selectScratchpad(scratchpadId);
    }
    flashItemId = null;
    await Promise.resolve();
    flashItemId = id;
    closeSearch();
  }
  async function searchOpenTodo(id: string) {
    try {
      openTodo = await api.getTodo(id);
      closeSearch();
    } catch (e) { errorMsg = String(e); }
  }
  async function searchOpenBug(id: string) {
    try {
      openBug = await api.getBug(id);
      closeSearch();
    } catch (e) { errorMsg = String(e); }
  }
  async function searchOpenUseCase(id: string) {
    try {
      openUseCase = await api.getUseCase(id);
      closeSearch();
    } catch (e) { errorMsg = String(e); }
  }
  // Canvas / List / Calendar: open a classified item's DERIVED detail modal
  // (todo/bug/use-case/KB) — the same modal the Lists drawer uses — so there's
  // one modal per type regardless of entry point.
  async function openDerivedFromItem(item: ScratchpadItem) {
    const cat = item.classification_override || item.proposed_category;
    const id = item.derived_item_id;
    if (!id) return;
    try {
      if (cat === 'todo') openTodo = await api.getTodo(id);
      else if (cat === 'bug') openBug = await api.getBug(id);
      else if (cat === 'use_case') openUseCase = await api.getUseCase(id);
      else if (cat === 'kb') openKb = await api.getKB(id);
    } catch (e) { errorMsg = String(e); }
  }
  // Q&A panel — symmetric with Search; reuses Search's per-kind
  // navigation, adds an "open file at line range" handler for
  // code-chunk citations.
  let qaOpen = $state(false);
  function toggleQA() { qaOpen = !qaOpen; }
  function closeQA() { qaOpen = false; }

  // Dashboard panel — Phase A panel #1 (throughput + open backlog).
  // Slide-out like Search; reloads on its own when opened.
  let dashboardOpen = $state(false);
  function toggleDashboard() { dashboardOpen = !dashboardOpen; }
  function closeDashboard() { dashboardOpen = false; }

  // Needs Review — the unified attention surface (classification + sign-off).
  // The badge count is refreshed alongside the normal data cycle; both halves
  // are cheap reads and failures stay silent (the panel shows its own errors).
  let needsReviewCount = $state(0);
  // Bumped at the end of every data cycle so the Needs review PANE reloads. A
  // modal reloaded each time it was opened; one that stays open has to be told
  // the world moved. The cycle is already SSE-debounced, so this is not a poll.
  let reviewTick = $state(0);
  // Sync the shared "drafting criteria" set from the server (the authoritative
  // list of items whose background Draft-with-AI job is still running). Runs on
  // every refresh — completion publishes ItemsChanged, so this clears finished
  // ones and the criteria they produced show up on the same cycle.
  async function refreshDrafting() {
    try {
      const refs = await api.listDraftingCriteria();
      syncDrafting(refs.map((r) => r.owner_id));
    } catch {
      /* best-effort — the badge is advisory */
    }
  }

  // Dependency readiness. Project-scoped and cheap, and it is the single
  // source for "is this blocked" across every view — canvas, list and kanban
  // all read the same map rather than each deciding for itself.
  let readiness = $state<api.ReadinessResponse | null>(null);
  async function refreshReadiness() {
    if (!activeProjectId) { readiness = null; return; }
    try {
      readiness = await api.getReadiness(activeProjectId);
    } catch {
      readiness = null; // advisory — a card simply shows no blocker chip
    }
  }

  /** source scratchpad item id -> its hard blockers.
   *
   *  Two hops, because dependencies link DERIVED items (todo/bug/kb/use_case)
   *  while the canvas draws scratchpad items: source -> derived id -> blockers.
   *  A pending-review card has no derived item yet, so it can carry no blocker
   *  chip — correct, since nothing can depend on an item that has not been
   *  classified into anything. */
  const blockedBySource = $derived.by<Map<string, api.Blocker[]>>(() => {
    const out = new Map<string, api.Blocker[]>();
    if (!readiness?.has_ordering) return out;
    const byDerived = new Map<string, api.Blocker[]>();
    for (const it of [...(readiness.blocked ?? []), ...(readiness.ready ?? [])]) {
      // Only HARD blockers that are still unsettled. 'informs' stays out of the
      // card per Rich — if both showed, most cards would carry a chip and the
      // chip would stop meaning anything.
      const hard = (it.blocked_by ?? []).filter((b) => !b.advisory);
      if (hard.length) byDerived.set(it.id, hard);
    }
    const link = (src: string | undefined, id: string) => {
      if (!src) return;
      const b = byDerived.get(id);
      if (b) out.set(src, b);
    };
    for (const t of padTodos) link(t.source_item_id, t.id);
    for (const b of padBugs) link(b.source_item_id, b.id);
    for (const k of padKb) link(k.source_item_id, k.id);
    for (const u of padUseCases) link(u.source_item_id, u.id);
    return out;
  });

  async function refreshNeedsReviewCount() {
    try {
      const [ib, q, pads, rv] = await Promise.all([
        api.listInbox(),
        activeProjectId ? api.getReviewQueue(activeProjectId) : Promise.resolve(null),
        activeProjectId ? api.listScratchpads(activeProjectId) : Promise.resolve([]),
        activeProjectId ? api.listReviews(activeProjectId, true).catch(() => ({ reviews: [] })) : Promise.resolve({ reviews: [] }),
      ]);
      // Scope the inbox count to THIS project's scratchpads — the panel filters
      // the same way, so a global count made the badge disagree with what opens
      // (Rich: IQOS badge said 3, panel was empty — those were other projects').
      const mine = new Set(pads.map((p) => p.id));
      const projectInbox = ib.filter((it) => mine.has(it.scratchpad_id)).length;
      // Open code reviews count too: a changeset waiting for a reader is as
      // much a call on your judgment as an escalated item.
      needsReviewCount =
        projectInbox + (q?.items ?? []).filter((i) => i.needs_review && !i.decision).length
        + rv.reviews.filter((v) => v.review.status !== 'approved').length;
    } catch {
      /* badge is best-effort */
    }
  }
  // The job monitor is a main view, not a settings tab: watching a 40-minute
  // decomposition is not configuration.
  // No `jobsOpen` mirror: the toolbar reads isPaneOpen('jobs') directly. A
  // mirrored copy went stale the moment the pane was closed from its own ×,
  // which is exactly what happened — the button stayed lit over a closed pane.
  function toggleJobs() {
    togglePane('jobs');
  }
  // Starting a job is a separate act from watching one, so it is a separate
  // window: the panel is glanced at constantly, the dialog is used rarely.
  let submitOpen = $state(false);
  /** Pre-selects the document when the dialog is opened from a card, so the
   *  gesture finishes where it started rather than making you find the thing
   *  you just dropped in a picker. */
  let submitPrefill = $state<Record<string, string>>({});
  /** The workflow the start dialog opens on, when opened from a context. */
  let submitWorkflow = $state('');
  let submitLabels = $state<Record<string, string>>({});
  /** Scratchpad items that came from a scanner finding, by item id: the ones
   *  an agent can fix. */
  let fixable = $state<Record<string, api.FixableItem>>({});

  /** Open the start dialog on Fix scanner findings: for these items, or for
   *  every finding on the scratchpad when none are given. */
  function fixWithAgent(scratchpadId: string, itemIds: string[], labels: Record<string, string> = {}) {
    submitWorkflow = 'fix_findings';
    submitPrefill = { scratchpad: scratchpadId, ...(itemIds.length ? { items: itemIds.join(',') } : {}) };
    submitLabels = Object.fromEntries(itemIds.map((id) => [id, labels[id] ?? fixable[id]?.label ?? id]));
    submitOpen = true;
  }
  let reviewJobId = $state<string | null>(null);
  let jobsReload = $state(0);

  // A running count on the nav button, so a 40-minute decomposition is visible
  // without opening anything. Closed-by-default only works if something tells
  // you there is work to look at; otherwise a job runs for an hour with no
  // indication anywhere until you happen to go looking.
  //
  // Paused counts too: a run someone stopped and expects back is exactly the
  // thing that gets forgotten.
  let jobsRunningCount = $state(0);

  async function refreshJobsCount() {
    if (!activeProjectId) {
      jobsRunningCount = 0;
      return;
    }
    try {
      const js = await api.listJobs(activeProjectId, undefined, 100);
      jobsRunningCount = (js ?? []).filter(
        (j) => j.status === 'running' || j.status === 'paused',
      ).length;
    } catch {
      /* badge is best-effort — a failed count must not break the shell */
    }
  }

  let architectureOpen = $state(false);
  function toggleArchitecture() { architectureOpen = !architectureOpen; }
  function closeArchitecture() { architectureOpen = false; }
  let dataModelOpen = $state(false);
  function toggleDataModel() { dataModelOpen = !dataModelOpen; }
  function closeDataModel() { dataModelOpen = false; }
  // Code Analysis (UC-14) — the baseline scan is FREE; the button always shows.
  let analysisOpen = $state(false);
  function toggleAnalysis() { analysisOpen = !analysisOpen; }
  function closeAnalysis() { analysisOpen = false; }
  // In-app Help — renders the bundled Glassbox guide.
  let helpOpen = $state(false);
  function toggleHelp() { helpOpen = !helpOpen; }
  function closeHelp() { helpOpen = false; }

  // ---- Commands ------------------------------------------------------------
  // Every shell action is declared ONCE here; the header, the profile menu and
  // the keyboard all read this registry (docs/design/commands.md). The state a
  // command toggles still lives in this file — the closures below are how the
  // registry reaches it without owning it. Order of registration is not order
  // of display: each surface states its own order (HEADER_COMMANDS below).
  const adminVisible = () => !!currentUser?.multi_user && !!currentUser?.is_admin;
  registerCommands([
    {
      id: 'view.drawer', label: 'Project drawer', category: 'View',
      description: 'Show or hide the navigation drawer (workspace, project files, scratchpad lists)',
      icon: 'menu', shortcut: 'Mod+B',
      run: toggleDrawer, active: () => isPaneOpen('tree'),
    },
    {
      id: 'view.needsReview', label: 'Needs review', category: 'View',
      description: 'Items waiting on your judgment — classification accepts and implementation sign-offs',
      icon: 'check', color: '#22c55e', shortcut: 'Mod+Shift+E', scope: 'project',
      run: () => togglePane('inbox'), active: () => isPaneOpen('inbox'), badge: () => needsReviewCount,
    },
    {
      id: 'view.jobs', label: 'Jobs', category: 'View',
      description: 'Jobs — what is running, on which model, and how far along',
      icon: 'jobs', color: '#38bdf8', scope: 'project',
      run: toggleJobs, active: () => isPaneOpen('jobs'), badge: () => jobsRunningCount,
    },
    {
      id: 'view.changes', label: 'Changes', category: 'View',
      description: 'Every file a branch, commit or the working copy changed, hunk by hunk — the changeset pane',
      icon: 'diff', color: '#84cc16', scope: 'project',
      run: () => togglePane('review'), active: () => isPaneOpen('review'),
    },
    {
      id: 'view.architecture', label: 'Architecture', category: 'View',
      description: 'Architecture diagram — LLM-drafted, you ratify',
      icon: 'architecture', color: '#8b5cf6', scope: 'project',
      run: toggleArchitecture, active: () => architectureOpen,
    },
    {
      id: 'view.dataModel', label: 'Data model', category: 'View',
      description: 'Data-model diagram — auto-generated from your SQL schema',
      icon: 'datamodel', color: '#f59e0b', scope: 'project',
      run: toggleDataModel, active: () => dataModelOpen,
    },
    {
      id: 'view.analysis', label: 'Analysis', category: 'View',
      description: 'Code analysis — static-analyzer findings you can push to a scratchpad',
      icon: 'analysis', color: '#14b8a6', scope: 'project',
      run: toggleAnalysis, active: () => analysisOpen,
    },
    {
      id: 'view.dashboard', label: 'Dashboard', category: 'View',
      description: 'Project metrics — throughput, open backlog',
      icon: 'dashboard', color: '#3b82f6', scope: 'project',
      run: toggleDashboard, active: () => dashboardOpen,
    },
    {
      id: 'view.lists', label: 'Lists', category: 'View',
      description: 'Project lists (todos / bugs / KB / use cases)',
      icon: 'lists', color: '#06b6d4', shortcut: 'Mod+Shift+L', scope: 'project',
      run: () => (listsOpen ? closeLists() : openLists()), active: () => listsOpen,
    },
    {
      id: 'view.search', label: 'Search', category: 'View',
      description: 'Search project items by meaning',
      icon: 'search', color: '#6366f1', shortcut: 'Mod+Shift+F', scope: 'project',
      run: toggleSearch, active: () => searchOpen,
    },
    {
      id: 'view.ask', label: 'Ask', category: 'View',
      description: 'Ask a question about this project (items + code)',
      icon: 'ask', color: '#ec4899', scope: 'project',
      run: toggleQA, active: () => qaOpen,
    },
    {
      id: 'view.resetLayout', label: 'Reset pane layout', category: 'View',
      description: 'Put every pane back where it started',
      run: resetLayout,
    },
    {
      id: 'project.new', label: 'New project…', category: 'Project',
      description: 'Create a project and switch to it',
      run: async () => {
        const name = await promptDialog('Project name', { title: 'New project', placeholder: 'Name' });
        if (name?.trim()) await pickerCreateProject(name.trim());
      },
    },
    {
      id: 'scratchpad.new', label: 'New scratchpad…', category: 'Scratchpad',
      description: 'Create a scratchpad in the current project',
      scope: 'project',
      run: async () => {
        const name = await promptDialog('Scratchpad name', { title: 'New scratchpad', placeholder: 'Name' });
        if (name?.trim()) await pickerCreateScratchpad(name.trim());
      },
    },
    {
      id: 'account.settings', label: 'Settings', category: 'Account',
      description: 'Models, appearance, input, indexing, workflow',
      shortcut: 'Mod+,',
      run: () => (settingsOpen = true), active: () => settingsOpen,
    },
    {
      id: 'account.stats', label: 'Stats', category: 'Account',
      description: 'Your activity in this workspace',
      run: () => (statsOpen = true), active: () => statsOpen,
    },
    {
      id: 'account.admin', label: 'Admin', category: 'Account',
      description: 'Workspace roster, seats and invitations',
      hidden: () => !adminVisible(),
      run: () => (adminOpen = true), active: () => adminOpen,
    },
    {
      id: 'help.guide', label: 'Help', category: 'Help',
      description: 'Help — what CodeDistill is and how to use every part of it',
      icon: 'help', color: '#22c55e', shortcut: 'F1',
      run: toggleHelp, active: () => helpOpen,
    },
    {
      id: 'help.systemInfo', label: 'About CodeDistill', category: 'Help',
      description: 'Version, build, platform and model details',
      hidden: () => !!currentUser?.multi_user && !currentUser?.is_admin,
      run: () => (systemOpen = true), active: () => systemOpen,
    },
    {
      id: 'help.credits', label: 'Credits & licenses', category: 'Help',
      description: 'Who built CodeDistill and what it is built on',
      run: () => (creditsOpen = true), active: () => creditsOpen,
    },
    {
      id: 'view.palette', label: 'Command palette…', category: 'View',
      description: 'Find any command by typing part of its name',
      shortcut: 'Mod+K',
      run: () => (paletteOpen = true), active: () => paletteOpen,
    },
    {
      id: 'view.leftPanel', label: 'Left panel', category: 'View',
      description: 'Show or hide whatever is docked on the left',
      run: () => toggleSlot('left'), active: () => visibleSlots().includes('left'),
    },
    {
      id: 'view.bottomPanel', label: 'Bottom panel', category: 'View',
      description: 'Show or hide whatever is docked at the bottom',
      run: () => toggleSlot('bottom'), active: () => visibleSlots().includes('bottom'),
    },
    {
      id: 'view.rightPanel', label: 'Right panel', category: 'View',
      description: 'Show or hide whatever is docked on the right',
      run: () => toggleSlot('right'), active: () => visibleSlots().includes('right'),
    },
    {
      id: 'agents.newJob', label: 'New job…', category: 'View',
      description: 'Start a workflow: fix findings, decompose a document, draft the architecture',
      scope: 'project',
      run: () => { submitWorkflow = ''; submitPrefill = {}; submitOpen = true; },
    },
    {
      id: 'agents.fixPad', label: 'Fix this scratchpad\'s findings…', category: 'View',
      description: 'Agents fix every open scanner finding on this scratchpad, highest severity first, on a branch of their own',
      scope: 'scratchpad', feature: 'mcp',
      enabled: () => padFindingCount > 0, badge: () => padFindingCount,
      run: () => fixWithAgent(activeScratchpadId, []),
    },
    {
      id: 'agents.securityKnowledge', label: 'Security knowledge', category: 'View',
      description: 'The sources the security agent checks against, and how fresh each is',
      feature: 'mcp', badge: () => securityStale.length,
      run: () => { settingsSection = 'security'; settingsOpen = true; },
    },
    {
      id: 'help.shortcuts', label: 'Keyboard shortcuts', category: 'Help',
      description: 'Every command and its shortcut, in the command palette',
      run: () => (paletteOpen = true),
    },
    // Code review — the code pane's own commands. They also make up its
    // right-click menu (CODE_CONTEXT_COMMANDS).
    {
      id: 'code.pickChangeset', label: 'Pick changeset…', category: 'View',
      description: 'Choose a branch, commit or the working copy to review the open file against',
      icon: 'branch', scope: 'project',
      run: openChangesetPicker, active: () => changesetPickerOpen,
    },
    {
      id: 'code.workingChanges', label: 'Show working-copy changes', category: 'View',
      description: 'The open file as the working copy against HEAD, changes marked',
      icon: 'diff', scope: 'project',
      enabled: () => !!activeCodeTab && !activeCodeTab.diff,
      run: () => { if (activeCodeTab) openDiffTab(activeCodeTab.path, { base: '', head: 'working', label: 'working copy' }); },
    },
    {
      id: 'code.plainView', label: 'Show file without diff', category: 'View',
      description: 'The open file as it is at the head of the current diff',
      scope: 'project',
      enabled: () => !!activeCodeTab?.diff,
      run: () => { if (activeCodeTab?.diff) openFileInCanvas(activeCodeTab.path, activeCodeTab.revision); },
    },
    {
      id: 'code.openChangeset', label: 'Show whole changeset', category: 'View',
      description: 'Every file of this tab\'s changeset in the Changes pane',
      icon: 'diff', scope: 'project',
      enabled: () => !!activeCodeTab?.diff || !!activeChangeset,
      run: () => { if (activeCodeTab?.diff) showChangeset(activeCodeTab.diff); else if (activeChangeset) openPaneById('review'); },
    },
    {
      id: 'code.closeTab', label: 'Close tab', category: 'View',
      description: 'Close the active code tab',
      scope: 'project',
      enabled: () => !!activeCodeTab,
      run: () => closeCodeTab(codeActiveIndex),
    },
  ]);
  // Header tools, Rich's order (2026-08-04): action first (Needs Review,
  // badge-carrying, leftmost), then the artifact windows, then read-only
  // surfaces, Help anchored last.
  // Only the queues stay in the strip (docs/design/menubar.md): what waits on
  // you, and what is running. Everything else is a menu, the palette or a
  // shortcut away.
  const HEADER_COMMANDS = ['view.needsReview', 'view.jobs'];
  // The menubar states its own order; '-' is a separator. An id nothing has
  // registered (yet) is left out, so a menu fills in as commands register.
  const MENUS: Menu[] = [
    { label: 'File', items: ['project.new', 'scratchpad.new', '-', 'scratchpad.upload', 'scratchpad.export', '-', 'account.settings'] },
    { label: 'Edit', items: ['scratchpad.group', 'scratchpad.clearSelection', '-', 'scratchpad.findDuplicates', 'scratchpad.archived', 'scratchpad.showHidden'] },
    { label: 'View', items: ['view.palette', '-', 'view.drawer', 'view.needsReview', 'view.jobs', 'view.changes', 'view.lists', 'view.search', 'view.ask',
      '-', 'scratchpad.viewCanvas', 'scratchpad.viewList', 'scratchpad.viewCalendar', 'scratchpad.viewKanban', 'scratchpad.filters',
      '-', 'view.leftPanel', 'view.bottomPanel', 'view.rightPanel', 'view.resetLayout'] },
    { label: 'Project', items: ['view.analysis', 'view.architecture', 'view.dataModel', 'view.dashboard'] },
    { label: 'Scratchpad', items: ['scratchpad.restack', 'scratchpad.restack.tidy', 'scratchpad.restack.fit', 'scratchpad.restack.cols2', 'scratchpad.restack.cols3',
      'scratchpad.autoTidy', '-', 'scratchpad.filters', 'scratchpad.showHidden', 'scratchpad.archived', '-', 'scratchpad.export', 'scratchpad.upload'] },
    { label: 'Agents', items: ['agents.newJob', 'agents.fixPad', '-', 'view.jobs', '-', 'agents.securityKnowledge'] },
    { label: 'Help', items: ['help.guide', 'help.shortcuts', '-', 'help.systemInfo', 'help.credits'] },
  ];
  /** Which menu a command is in, for the palette. */
  function menuOf(id: string): string {
    return MENUS.find((m) => m.items.includes(id))?.label ?? '';
  }
  let paletteOpen = $state(false);
  // The strip steps down as the window narrows (docs/design/menubar.md):
  // labels first, then the wordmark and the menus fold into one, then the
  // panel toggles. Measured, not a viewport breakpoint: the panes beside it
  // are what decide how much room it has.
  let stripW = $state(1600);
  function measureWidth(node: HTMLElement) {
    const ro = new ResizeObserver(([e]) => (stripW = e.contentRect.width));
    ro.observe(node);
    return { destroy: () => ro.disconnect() };
  }
  /** Open scanner findings on the active scratchpad: what Agents → Fix would work. */
  const padFindingCount = $derived(
    Object.values(fixable).filter((f) => f.open && f.scratchpad_id === activeScratchpadId).length,
  );
  // The layout toggles show or hide a whole edge. Hiding remembers what was
  // there; showing brings it back, or a sensible default the first time.
  const lastInSlot: Record<SlotID, string> = { left: 'tree', right: 'dock', bottom: 'review' };
  function toggleSlot(s: SlotID) {
    const a = activePane(s);
    if (a) {
      lastInSlot[s] = a;
      closePaneById(a);
      return;
    }
    const id = lastInSlot[s];
    openPaneById(id);
    if (paneSlot(id) !== s) movePaneTo(id, s);
  }
  const headerCommands = $derived(commandsById(HEADER_COMMANDS));
  // Tooltip = description, plus the shortcut when there is one.
  function commandTitle(cmd: Command): string {
    const k = shortcutLabel(cmd);
    return k ? `${cmd.description ?? cmd.label} (${k})` : (cmd.description ?? cmd.label);
  }
  // Enablement that is the same for every command is data the registry
  // decides against; this keeps it current.
  $effect(() => {
    setCommandContext({
      hasProject: !!activeProjectId,
      hasScratchpad: !!activeScratchpadId,
      licensed,
    });
  });

  function qaOpenCodeChunk(filePath: string, lineStart: number, lineEnd: number) {
    openFileInCanvas(filePath, '');
    pendingHighlight = null;
    void Promise.resolve().then(() => {
      pendingHighlight = {
        path: filePath,
        revision: '',
        lineStart: lineStart || 1,
        lineEnd: lineEnd || lineStart || 1,
      };
    });
    closeQA();
  }

  // Dedup banner "Open match" — fetch the similar item, switch to its
  // scratchpad if needed, then flash the card.
  async function openSimilarItem(similarToId: string) {
    try {
      const target = await api.getItem(similarToId);
      if (target.scratchpad_id && target.scratchpad_id !== activeScratchpadId) {
        await selectScratchpad(target.scratchpad_id);
      }
      flashItemId = null;
      await Promise.resolve();
      flashItemId = target.id;
    } catch (e) {
      errorMsg = `Couldn't open match: ${e}`;
    }
  }

  async function searchOpenKB(id: string) {
    closeSearch();
    try {
      openKb = await api.getKB(id);
    } catch (e) {
      errorMsg = `Couldn't open KB: ${e}`;
    }
  }

  // Cross-scratchpad highlight target — set when an anchor click resolves
  // to a scratchpad item. ScratchpadGrid latches it, flashes the card,
  // and ignores subsequent identical values until it changes again.
  let flashItemId = $state<string | null>(null);

  // Per-scratchpad-item anchor map for the active scratchpad. Refreshed
  // alongside items so each card knows whether to show its anchor badge
  // and which file/range to open on click. Built by grouping a single
  // GET /scratchpads/{id}/code-anchors response.
  let itemAnchors = $state<Record<string, CodeAnchor[]>>({});

  // Pending highlight for the code canvas — set when a scratchpad-card
  // anchor click opens a file at a specific line range. The active
  // CodeViewer applies it as a selection + scrolls into view, then
  // signals consumed so we clear the request.
  let pendingHighlight = $state<{
    path: string;
    revision: string;
    lineStart: number;
    lineEnd: number;
  } | null>(null);

  // Hovered scratchpad-card anchor — drives the reverse-hover tint in
  // the active CodeViewer (only fires when (path, revision) match).
  let hoveredCardAnchor = $state<CodeAnchor | null>(null);

  // Code canvas — middle column. Conditional: rendered only when there's
  // at least one tab AND the user hasn't hidden it. Per-project tabs +
  // visibility are persisted under ui.codecanvas.<projectId>; the column
  // width is global (consistent across projects).
  let codeTabs = $state<CodeTab[]>([]);
  let codeActiveIndex = $state(0);
  let codeVisible = $state(false);
  let codeCanvasWidth = $state(CODE_CANVAS_WIDTH_DEFAULT);
  let codeCanvasOpen = $derived(codeTabs.length > 0 && codeVisible);

  // Pane grid columns, composed left→right: drawer │ code │ main(1fr) │ dock.
  // The dock (item cards kept beside the code you followed a link into) sits on
  // the far right — opposite the code window.
  const DOCK_WIDTH = 280;
  const JOBS_WIDTH = 420;
  // The dock appears when something is docked and leaves when nothing is — it
  // was never a thing you toggled. Kept that way: an $effect syncs the pane to
  // the item count rather than the user opening it.
  const dockOpen = $derived(dock.items.length > 0);
  $effect(() => {
    if (dockOpen && !isPaneOpen('dock')) openPaneById('dock');
    if (!dockOpen && isPaneOpen('dock')) closePaneById('dock');
  });
  // Order left→right: drawer │ code │ dock │ canvas(1fr). The dock sits right
  // of the code window and left of the canvas, so the item you followed a link
  // into stays adjacent to its code.
  // Grid columns, derived from the layout rather than from a boolean per panel.
  //
  // Left slot, canvas, right slot. The bottom slot is a ROW below this grid, not
  // a column, so it is handled by the wrapper rather than here.
  // One resizer for every pane, parameterised. There were two near-identical
  // pairs before (drawer, code canvas); a third and fourth pane would have made
  // it four. Axis matters: the bottom slot sizes by HEIGHT and grows upward, so
  // dragging up must increase it.
  let resizingPane = $state<string | null>(null);
  // Tracked so the grab cursor matches the axis: dragging the bottom handle
  // must not put col-resize over the whole panes row.
  let resizingAxis = $state<'x' | 'y'>('x');
  let resizeStart = 0;
  let resizeFrom = 0;

  function startPaneResize(e: PointerEvent, id: string, axis: 'x' | 'y') {
    resizingPane = id;
    resizingAxis = axis;
    resizeStart = axis === 'x' ? e.clientX : e.clientY;
    resizeFrom = paneWidth(id);
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function paneResizeMove(e: PointerEvent, axis: 'x' | 'y', invert: boolean) {
    if (!resizingPane) return;
    const now = axis === 'x' ? e.clientX : e.clientY;
    const delta = (now - resizeStart) * (invert ? -1 : 1);
    setPaneWidth(resizingPane, resizeFrom + delta);
  }

  function endPaneResize(e: PointerEvent) {
    if (!resizingPane) return;
    resizingPane = null;
    // Commit now rather than waiting out the debounce: letting go should save.
    flushPaneSize();
    try { (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId); } catch { /* already gone */ }
  }

  // A slot with no panes does not render — correct for a normal layout, and a
  // dead end while dragging: you cannot drop onto something that is not there,
  // so the bottom slot would be unreachable by drag (the Move menu could still
  // get you there, which is why this only showed up now). While a drag is in
  // flight, every empty slot offers itself as a strip.
  const emptySlots = $derived.by(() =>
    draggingPane() ? SLOTS.filter((s) => !visibleSlots().includes(s)) : [],
  );

  function dropInto(e: DragEvent, s: SlotID) {
    e.preventDefault();
    const id = e.dataTransfer?.getData('application/x-codedistill-pane') || draggingPane();
    endDrag();
    if (id) movePaneTo(id, s);
  }

  function allowDrop(e: DragEvent) {
    if (!draggingPane()) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
  }

  const paneColumns = $derived.by(() => {
    const vis = visibleSlots();
    const cols: string[] = [];
    if (vis.includes('left')) {
      const id = activePane('left');
      cols.push(`${id ? paneWidth(id) : 240}px`, '5px');
    }
    cols.push('1fr');
    if (vis.includes('right')) {
      const id = activePane('right');
      cols.push('5px', `${id ? paneWidth(id) : 400}px`);
    }
    return cols.join(' ');
  });
  // Follow a code link from an item detail: dock the item (keep it visible)
  // instead of losing it, then open the file. Reopening the full modal is a
  // click away from the docked card.
  function followItemCode(ownerType: DockedItem['ownerType'], ownerId: string, path: string, revision: string) {
    dockItem(ownerType, ownerId);
    openFileInCanvas(path, revision);
  }
  async function openDockedFull(ownerType: DockedItem['ownerType'], ownerId: string) {
    try {
      if (ownerType === 'todo_item') openTodo = await api.getTodo(ownerId);
      else if (ownerType === 'bug_item') openBug = await api.getBug(ownerId);
      else openUseCase = await api.getUseCase(ownerId);
    } catch (e) { errorMsg = String(e); }
  }

  // Drag-resize state for the drawer's right edge.
  let resizing = $state(false);
  let resizeStartX = 0;
  let resizeStartWidth = 0;

  function startResize(e: PointerEvent) {
    resizing = true;
    resizeStartX = e.clientX;
    resizeStartWidth = drawerWidth;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function onResizeMove(e: PointerEvent) {
    if (!resizing) return;
    const next = resizeStartWidth + (e.clientX - resizeStartX);
    drawerWidth = Math.max(DRAWER_WIDTH_MIN, Math.min(DRAWER_WIDTH_MAX, next));
  }

  function endResize(e: PointerEvent) {
    if (!resizing) return;
    resizing = false;
    try { (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId); } catch {}
    saveUserNum(DRAWER_WIDTH_KEY, drawerWidth);
  }

  // Drag-resize state for the code-canvas / scratchpad splitter.
  let resizingCode = $state(false);
  let resizeCodeStartX = 0;
  let resizeCodeStartWidth = 0;

  function startResizeCode(e: PointerEvent) {
    resizingCode = true;
    resizeCodeStartX = e.clientX;
    resizeCodeStartWidth = codeCanvasWidth;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function onResizeCodeMove(e: PointerEvent) {
    if (!resizingCode) return;
    const next = resizeCodeStartWidth + (e.clientX - resizeCodeStartX);
    codeCanvasWidth = Math.max(CODE_CANVAS_WIDTH_MIN, Math.min(CODE_CANVAS_WIDTH_MAX, next));
  }

  function endResizeCode(e: PointerEvent) {
    if (!resizingCode) return;
    resizingCode = false;
    try { (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId); } catch {}
    saveUserNum(CODE_CANVAS_WIDTH_KEY, codeCanvasWidth);
  }

  // Set when a canvas-state LOAD fails, to block saves until a load succeeds —
  // otherwise a transient read failure (which clears codeTabs) would let the
  // next save persist the empty list over the user's real saved tabs (audit
  // M26 — the one silent-failure that actually loses data).
  let codeCanvasLoadFailed = $state(false);
  function saveCodeCanvasState() {
    if (!activeProjectId || codeCanvasLoadFailed) return;
    api.setUserSetting(LOCAL_USER, `${CODE_CANVAS_KEY_PREFIX}${activeProjectId}`, {
      tabs: codeTabs,
      activeIndex: codeActiveIndex,
      visible: codeVisible,
    }).catch(() => {});
  }

  async function loadCodeCanvasState(pid: string) {
    if (!pid) {
      codeTabs = [];
      codeActiveIndex = 0;
      codeVisible = false;
      return;
    }
    try {
      const r = await api.getUserSetting(LOCAL_USER, `${CODE_CANVAS_KEY_PREFIX}${pid}`);
      if (!r) {
        // 204 — nothing saved for this project yet. That is not a failure, so
        // clear the view AND clear the failed flag; leaving it latched would
        // keep blocking saves.
        codeTabs = [];
        codeActiveIndex = 0;
        codeVisible = false;
        codeCanvasLoadFailed = false;
        return;
      }
      const v = r.value as { tabs?: CodeTab[]; activeIndex?: number; visible?: boolean } | null;
      const tabs = Array.isArray(v?.tabs)
        ? v.tabs.filter((t) => t && typeof t.path === 'string' && typeof t.revision === 'string')
        : [];
      codeTabs = tabs;
      codeActiveIndex = typeof v?.activeIndex === 'number' && v.activeIndex < tabs.length
        ? v.activeIndex
        : 0;
      codeVisible = typeof v?.visible === 'boolean' ? v.visible : tabs.length > 0;
      // A project opens on its canvas: the canvas is the product, and its
      // saved file tabs wait beside it (docs/design/center-tabs.md).
      codeVisible = false;
      codeCanvasLoadFailed = false;
    } catch {
      // Load failed (not "no saved state" — that returns null, handled above).
      // Clear the view but block saves so we can't overwrite the persisted tabs.
      codeCanvasLoadFailed = true;
      codeTabs = [];
      codeActiveIndex = 0;
      codeVisible = false;
    }
  }

  // The code opens in the centre, as tabs beside the canvas
  // (docs/design/center-tabs.md). codeVisible says the centre is showing the
  // code rather than the canvas; it is what ui.codecanvas.<pid> persists.
  function openFileInCanvas(path: string, revision: string = '') {
    const idx = codeTabs.findIndex((t) => t.path === path && t.revision === revision && !t.diff);
    if (idx >= 0) {
      codeActiveIndex = idx;
    } else {
      codeTabs = [...codeTabs, { path, revision }];
      codeActiveIndex = codeTabs.length - 1;
    }
    codeVisible = true;
    saveCodeCanvasState();
  }

  // ---- Code review: changesets in the code pane (docs/design/code-review.md) ----
  // The last picked changeset. The single-file diff mode uses it now; the
  // changeset pane (slice 3) will read the same value.
  let activeChangeset = $state<ChangesetPick | null>(null);
  let changesetPickerOpen = $state(false);
  let codeContextMenu = $state<{ x: number; y: number } | null>(null);
  const CODE_CONTEXT_COMMANDS = ['code.pickChangeset', 'code.workingChanges', 'code.plainView', 'code.openChangeset', 'code.closeTab'];
  const activeCodeTab = $derived(
    codeActiveIndex >= 0 && codeActiveIndex < codeTabs.length ? codeTabs[codeActiveIndex] : null,
  );

  /** A diff tab: the file as base..head with changes marked. One per
   *  (path, changeset); re-picking the same one re-activates it. */
  function openDiffTab(path: string, cs: { base: string; head: string; label: string }) {
    const headRev = cs.head === 'working' ? '' : cs.head;
    const idx = codeTabs.findIndex((t) => t.path === path && t.diff?.base === cs.base && t.diff?.head === cs.head);
    if (idx >= 0) {
      codeActiveIndex = idx;
    } else {
      codeTabs = [...codeTabs, { path, revision: headRev, diff: { base: cs.base, head: cs.head, label: cs.label } }];
      codeActiveIndex = codeTabs.length - 1;
    }
    codeVisible = true;
    saveCodeCanvasState();
  }

  /** The changeset pane, showing the given changeset. A diff tab carries
   *  base/head/label; the pick's kind is read back from them. */
  function showChangeset(d: { base: string; head: string; label: string }) {
    const kind: ChangesetPick['kind'] = d.head === 'working' ? 'working' : d.base ? 'branch' : 'commit';
    activeChangeset = { base: d.base, head: d.head, label: d.label, kind };
    openPaneById('review');
  }

  function openChangesetPicker() {
    // The picker hangs off the code's tab strip, so the code has to be showing.
    codeVisible = true;
    changesetPickerOpen = true;
  }

  async function onChangesetPicked(cs: ChangesetPick) {
    changesetPickerOpen = false;
    activeChangeset = cs;
    if (activeCodeTab) {
      openDiffTab(activeCodeTab.path, cs);
      return;
    }
    // Nothing open: show the changeset's first changed file rather than
    // nothing. Context 0 keeps the listing cheap; the tab fetches its own.
    try {
      const list = await api.changesetDiff(activeProjectId, { base: cs.base, head: cs.head, context: 0 });
      const first = list.files.find((f) => !f.generated) ?? list.files[0];
      if (first) openDiffTab(first.path, cs);
      else errorMsg = `No changes in ${cs.label}`;
    } catch (e) {
      errorMsg = String(e);
    }
  }

  function activateCodeTab(index: number) {
    if (index < 0 || index >= codeTabs.length) return;
    codeActiveIndex = index;
    codeVisible = true;
    saveCodeCanvasState();
  }

  function closeCodeTab(index: number) {
    if (index < 0 || index >= codeTabs.length) return;
    codeTabs = codeTabs.filter((_, i) => i !== index);
    if (codeTabs.length === 0) {
      codeActiveIndex = 0;
      codeVisible = false; // the last file closed: back to the canvas
    } else if (index < codeActiveIndex) {
      codeActiveIndex = codeActiveIndex - 1;
    } else if (index === codeActiveIndex && codeActiveIndex >= codeTabs.length) {
      codeActiveIndex = codeTabs.length - 1;
    }
    saveCodeCanvasState();
  }

  /** Back to the canvas; the files stay open as tabs. */
  function hideCodeCanvas() {
    codeVisible = false;
    saveCodeCanvasState();
  }

  // Code-canvas anchor click → route to the owner. For derived items we
  // open the appropriate detail modal (fetching by id since the item may
  // belong to a different scratchpad than the active one). For scratchpad
  // items we switch scratchpads if needed and flash the matching card.
  // KB clicks navigate the drawer to scratchpad → kb tab and request a
  // row expand.
  async function handleAnchorClick(a: CodeAnchorWithOwner) {
    try {
      switch (a.owner_type) {
        case 'todo_item':
          openTodo = await api.getTodo(a.owner_id);
          break;
        case 'bug_item':
          openBug = await api.getBug(a.owner_id);
          break;
        case 'use_case_item':
          openUseCase = await api.getUseCase(a.owner_id);
          break;
        case 'knowledge_entry':
          openKb = await api.getKB(a.owner_id);
          break;
        case 'scratchpad_item':
          if (a.owner_scratchpad_id && a.owner_scratchpad_id !== activeScratchpadId) {
            await selectScratchpad(a.owner_scratchpad_id);
          }
          flashItemId = null;
          await Promise.resolve();
          flashItemId = a.owner_id;
          break;
      }
    } catch (e) {
      errorMsg = `Failed to open anchor target: ${e}`;
    }
  }

  // Picker callback shims — wrap the existing API + local-state pair so the
  // generic Picker component sees a single `(args) => Promise<void>` shape.
  // Project picker.
  async function pickerCreateProject(name: string): Promise<void> {
    const p = await api.createProject({ name });
    await projectCreated(p);
  }
  async function pickerRenameProject(id: string, name: string): Promise<void> {
    const p = await api.updateProject(id, { name });
    projectRenamed(p);
  }
  async function pickerDeleteProject(id: string): Promise<void> {
    await api.deleteProject(id);
    await projectDeleted(id);
  }
  // Scratchpad picker — newScratchpad / renameScratchpad / removeScratchpad
  // already do the API + local update; just need to return the promise.
  async function pickerCreateScratchpad(name: string): Promise<void> {
    await newScratchpad(name);
  }
  async function pickerRenameScratchpad(id: string, name: string): Promise<void> {
    await renameScratchpad(id, name);
  }
  async function pickerDeleteScratchpad(id: string): Promise<void> {
    await removeScratchpad(id);
  }

  // Reverse direction: an anchor icon on a scratchpad card was clicked.
  // Open the anchor's file in the code canvas (creating the tab if needed)
  // and queue a highlight request the active CodeViewer applies as a
  // selection + scroll-into-view once it loads the file.
  function openCardAnchor(a: CodeAnchor) {
    if (a.kind !== 'file' || !a.path) return;
    const revision = a.revision ?? '';
    openFileInCanvas(a.path, revision);
    pendingHighlight = null;
    queueMicrotask(() => {
      pendingHighlight = {
        path: a.path!,
        revision,
        lineStart: a.line_start ?? 0,
        lineEnd: a.line_end ?? a.line_start ?? 0,
      };
    });
  }

  // Create a scratchpad item from a code-canvas selection: new item on the
  // active scratchpad with the snippet content, then a kind=file anchor
  // bound to the new item. Refreshes data and flashes the new card so the
  // user sees where it landed.
  async function createItemFromCodeSelection(payload: {
    path: string;
    revision: string;
    lineStart: number;
    lineEnd: number;
    content: string;
  }) {
    if (!activeScratchpadId) {
      errorMsg = 'No active scratchpad — pick or create one first.';
      return;
    }
    try {
      const item = await api.createItem(activeScratchpadId, {
        content: payload.content,
        content_type: 'code_snippet',
      });
      await api.createCodeAnchor('scratchpad_item', item.id, {
        kind: 'file',
        path: payload.path,
        line_start: payload.lineStart,
        line_end: payload.lineEnd,
        revision: payload.revision === '' ? undefined : payload.revision,
      });
      await refresh();
      // Flash the new card. Reset null then set so the flash latch fires
      // even if the user creates two from the same selection in a row.
      flashItemId = null;
      await Promise.resolve();
      flashItemId = item.id;
    } catch (e) {
      errorMsg = `Failed to create scratchpad item: ${e}`;
      throw e; // let CodeViewer surface its own error UI too
    }
  }

  // loadProjects fetches the project list; called on mount and after any
  // create / delete. Not on the polling tick — projects don't change on
  // their own.
  async function loadProjects() {
    try {
      projects = await api.listProjects();
      // If the persisted activeProjectId is missing (first run, or the
      // project was deleted out from under us), fall back to the first
      // available, or "default" if none — that's what cmdServe seeds.
      if (!projects.find((p) => p.id === activeProjectId)) {
        activeProjectId = projects[0]?.id ?? 'default';
      }
    } catch (e) {
      errorMsg = String(e);
    }
  }

  async function selectProject(id: string) {
    if (id === activeProjectId) return;
    activeProjectId = id;
    activeScratchpadId = '';
    api.setUserSetting(LOCAL_USER, ACTIVE_PROJECT_KEY, id).catch(() => {});
    await Promise.all([refresh(), loadCodeCanvasState(id)]);
  }

  async function projectCreated(p: Project) {
    projects = [...projects, p];
    await selectProject(p.id);
  }

  function projectRenamed(p: Project) {
    projects = projects.map((x) => (x.id === p.id ? p : x));
  }

  async function projectDeleted(deletedId: string) {
    const remaining = projects.filter((p) => p.id !== deletedId);
    projects = remaining;
    if (deletedId === activeProjectId && remaining.length > 0) {
      await selectProject(remaining[0].id);
    } else if (remaining.length === 0) {
      // Shouldn't happen — delete button is disabled at length===1 — but
      // guard anyway so the UI doesn't strand on a missing project.
      activeProjectId = '';
      codeTabs = [];
      codeActiveIndex = 0;
      codeVisible = false;
      await refresh();
    }
  }

  async function refresh() {
    try {
      const sps = await api.listScratchpads(activeProjectId);
      scratchpads = sps;
      if (sps.length === 0) {
        activeScratchpadId = '';
        items = [];
        padTodos = [];
        padBugs = [];
        padKb = [];
        padUseCases = [];
      } else if (!sps.find((s) => s.id === activeScratchpadId)) {
        activeScratchpadId = sps[0].id;
      }
      if (activeScratchpadId) {
        const scoped = await Promise.all([
          api.listItems(activeScratchpadId),
          // include_done=true so the scratchpad canvas can fade cards
          // whose every derived item is in a terminal state. Counts at
          // the bottom reflect the same total set the cards visualise.
          api.listTodosByScratchpad(activeScratchpadId, true),
          api.listBugsByScratchpad(activeScratchpadId, true),
          api.listKBByScratchpad(activeScratchpadId, true),
          api.listUseCasesByScratchpad(activeScratchpadId, true),
          api.listScratchpadAnchors(activeScratchpadId),
          api.getLatestNotes(),
        ]);
        items = scoped[0];
        padTodos = scoped[1];
        padBugs = scoped[2];
        padKb = scoped[3];
        padUseCases = scoped[4];
        itemAnchors = groupAnchorsByOwner(scoped[5]);
        latestNotes = scoped[6].notes;
      } else {
        itemAnchors = {};
        latestNotes = [];
      }
      if (listsOpen) await loadProjectLists();
      void refreshReadiness();
      void refreshNeedsReviewCount();
      reviewTick += 1;
      void refreshJobsCount();
      void refreshDrafting();
      if (licensed('mcp')) {
        api.getFixableItems(activeProjectId).then((f) => (fixable = f ?? {})).catch(() => (fixable = {}));
        api.getSecurityKnowledge().then((k) => (securityStale = k.stale ?? [])).catch(() => {});
      }
      errorMsg = '';
    } catch (e) {
      errorMsg = String(e);
    }
  }

  function groupAnchorsByOwner(list: CodeAnchor[]): Record<string, CodeAnchor[]> {
    const out: Record<string, CodeAnchor[]> = {};
    for (const a of list) {
      (out[a.owner_id] ||= []).push(a);
    }
    return out;
  }

  async function selectScratchpad(id: string) {
    if (id === activeScratchpadId) return;
    activeScratchpadId = id;
    try {
      items = await api.listItems(id);
      const scoped = await Promise.all([
        api.listTodosByScratchpad(id, true),
        api.listBugsByScratchpad(id, true),
        api.listKBByScratchpad(id, true),
        api.listUseCasesByScratchpad(id, true),
        api.listScratchpadAnchors(id),
      ]);
      padTodos = scoped[0];
      padBugs = scoped[1];
      padKb = scoped[2];
      padUseCases = scoped[3];
      itemAnchors = groupAnchorsByOwner(scoped[4]);
      errorMsg = '';
    } catch (e) {
      errorMsg = String(e);
    }
  }

  async function newScratchpad(name: string) {
    try {
      const sp = await api.createScratchpad(activeProjectId, { name });
      scratchpads = [...scratchpads, sp];
      await selectScratchpad(sp.id);
    } catch (e) {
      errorMsg = String(e);
    }
  }

  async function renameScratchpad(id: string, name: string) {
    try {
      const sp = await api.updateScratchpad(id, { name });
      scratchpads = scratchpads.map((s) => (s.id === id ? sp : s));
    } catch (e) {
      errorMsg = String(e);
    }
  }

  async function removeScratchpad(id: string) {
    try {
      await api.deleteScratchpad(id);
      scratchpads = scratchpads.filter((s) => s.id !== id);
      if (activeScratchpadId === id && scratchpads.length > 0) {
        await selectScratchpad(scratchpads[0].id);
      } else if (scratchpads.length === 0) {
        items = [];
      }
    } catch (e) {
      errorMsg = String(e);
    }
  }

  function toggleDrawer() {
    // Through the layout now: the drawer is the 'tree' pane. The old boolean is
    // still written so anything else reading it stays consistent until it goes.
    togglePane('tree');
    saveUserBool(DRAWER_KEY, isPaneOpen('tree'));
  }

  // SSE channel — the primary update path. Mutation handlers on
  // the server publish coarse events; we listen for any of them
  // and debounce-refresh. Polling becomes a fallback that only
  // ticks when SSE is disconnected.
  let sse: SSEChannel | null = null;
  let filesRefreshTick = $state(0);

  // PWA update flow (UC-6): registerType 'autoUpdate' (vite.config) — a new
  // build installs, skip-waits, and claims clients on its own; we force a
  // reload the instant it takes control (the controllerchange listener below).
  // No opt-in toast: a rebuild can never strand the window on a stale/broken
  // bundle, which ends the recurring clear-cache dance after a server upgrade.
  useRegisterSW({
    // The browser only re-fetches sw.js on navigations — and an
    // installed PWA window is a long-lived SPA with none, so without
    // active checks a server upgrade is never noticed (live-test bug,
    // v0.10.14). Poll every minute (a cheap conditional fetch) and
    // check immediately when the window regains focus.
    onRegisteredSW(_url: string, r: ServiceWorkerRegistration | undefined) {
      if (!r) return;
      setInterval(() => void r.update(), 60_000);
      window.addEventListener('focus', () => void r.update());
    },
    onRegisterError(e: unknown) {
      console.warn('service worker registration failed:', e);
    },
  });

  // Force a reload the moment a NEW service worker takes control — i.e. an
  // update, not the first install. `controller` is null on a first-ever visit,
  // so capturing it now distinguishes "fresh install" (no reload) from
  // "upgrade" (reload onto the new bundle). The `reloading` guard avoids a
  // double reload if controllerchange fires more than once.
  if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator) {
    const hadController = !!navigator.serviceWorker.controller;
    let reloading = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (!hadController || reloading) return;
      reloading = true;
      window.location.reload();
    });
  }
  let sseConnected = $state(false);

  // Overflow-driven header compression — see lib/overflowCollapse.ts (shared
  // with the canvas toolbar; measures the bar itself instead of the viewport).
  let headerTight = $state(false);
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  function scheduleRefresh() {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = undefined;
      void refresh();
    }, SSE_DEBOUNCE_MS);
  }

  onMount(() => {
    // onMount must return a synchronous cleanup function. Initial fetch +
    // poll-timer setup live inside an async IIFE so we can await persisted
    // preferences (active project in particular) before kicking off
    // refresh() — otherwise we'd flash a "default project" frame.
    let timer: ReturnType<typeof setInterval> | undefined;
    (async () => {
      // Fire-and-forget: version is cosmetic; don't block first paint on it.
      void initTheme();
      void initFonts();
      void initUiPrefs();
      api.getVersion().then((v) => (buildInfo = v)).catch(() => {});
      // License is fire-and-forget too — see the `licensed` helper for
      // why null is treated as permissive.
      api.getLicense().then((l) => {
        license = l;
        // The window title carries the LICENSE holder, not the editable display
        // name — a fixed watermark of who this copy is licensed to (anti-theft).
        document.title = l.customer ? `CodeDistill — ${l.customer}` : 'CodeDistill';
      }).catch(() => {});
      // Current user (attribution) — drives the header profile menu.
      api.getMe().then((m) => { currentUser = m; }).catch(() => {});
      // Submit-shortcut load is fire-and-forget too — keystrokes before
      // it resolves use the default ('ctrl-enter'), matching pre-setting
      // behavior, so nothing breaks during the few ms it's outstanding.
      loadSubmitShortcut().catch(() => {});
      drawerWidth = Math.max(
        DRAWER_WIDTH_MIN,
        Math.min(DRAWER_WIDTH_MAX, await loadUserNum(DRAWER_WIDTH_KEY, DRAWER_WIDTH_DEFAULT)),
      );
      codeCanvasWidth = Math.max(
        CODE_CANVAS_WIDTH_MIN,
        Math.min(CODE_CANVAS_WIDTH_MAX, await loadUserNum(CODE_CANVAS_WIDTH_KEY, CODE_CANVAS_WIDTH_DEFAULT)),
      );
      try {
        const r = await api.getUserSetting(LOCAL_USER, ACTIVE_PROJECT_KEY);
        // undefined = 204, no active project remembered (every first run).
        // Must NOT return here: loadProjects() below is what falls back to the
        // first project, and skipping it leaves the app with no project, no
        // data and no SSE channel.
        if (r && typeof r.value === 'string' && r.value) {
          activeProjectId = r.value;
        }
      } catch {
        // Transport failure; loadProjects below still falls back to the first
        // project in the list.
      }
      // Ask what the server is missing before anything else paints: a fresh
      // machine has no Ollama and no models, and the rest of the UI would only
      // show empty lists and failed classifications.
      try {
        const st = await api.getSetupStatus();
        const have = (m: string) => st.models.installed.some((i) => i === m || i === m + ':latest');
        if (!st.completed && (!st.ollama.reachable || !have(st.models.classifier) || !have(st.models.embed))) {
          setupStatus = st;
        }
      } catch {
        // Older server without the endpoint, or a transport failure: no setup screen.
      }
      await loadProjects();
      await Promise.all([refresh(), loadCodeCanvasState(activeProjectId)]);

      // Open the SSE channel. Any event = debounced full refresh.
      // The polling fallback only fires when SSE is disconnected,
      // so the steady-state cost on a healthy connection is zero
      // background traffic except for occasional heartbeats and
      // user-action-triggered refreshes.
      sse = new SSEChannel();
      sse.onState((s) => {
        const was = sseConnected;
        sseConnected = s === 'connected';
        // Reconnect after a gap = we may have missed events while dead —
        // catch up immediately instead of waiting for the next change.
        if (!was && sseConnected) {
          void loadProjects();
          scheduleRefresh();
        }
      });
      const unsubs = [
        sse.on(EventNames.ItemsChanged, scheduleRefresh),
        sse.on(EventNames.InboxChanged, scheduleRefresh),
        sse.on(EventNames.TodosChanged, scheduleRefresh),
        sse.on(EventNames.BugsChanged, scheduleRefresh),
        sse.on(EventNames.KBChanged, scheduleRefresh),
        sse.on(EventNames.UseCasesChanged, scheduleRefresh),
        sse.on(EventNames.AnchorsChanged, scheduleRefresh),
        sse.on(EventNames.ScratchpadsChanged, scheduleRefresh),
        // Files panel tree refresh — fired by the code-index watcher
        // when a scan wrote/deleted chunks (the repo moved).
        sse.on(EventNames.FilesChanged, () => (filesRefreshTick += 1)),
        // Projects need their own refetch — refresh() only covers the active
        // project's contents, and another user's rename/create/delete must
        // reach this client's project switcher too.
        sse.on(EventNames.ProjectsChanged, () => {
          void loadProjects();
          scheduleRefresh();
        }),
      ];
      sse.open();

      // Fallback poll. Only fires when SSE is disconnected — the
      // happy path is a no-op tick. Cheap enough at 30s to leave
      // running unconditionally.
      timer = setInterval(() => {
        if (!sseConnected) void refresh();
      }, POLL_MS);

      // Cleanup binding for onDestroy. We can't return from inside
      // the async IIFE, so collect cleanups onto a module-scope
      // var that the outer return reads.
      cleanupSSE = () => {
        for (const u of unsubs) u();
        sse?.close();
        sse = null;
      };
    })();
    // The badge needs its own clock. A job started from the CLI produces no app
    // activity for forty minutes, so a count that refreshes only when something
    // else happens would sit at zero the whole time — which is the failure this
    // badge exists to prevent. Fifteen seconds: a job changes state every few
    // minutes, so anything faster is wasted requests.
    const jobsBadgeTimer = setInterval(() => void refreshJobsCount(), 15000);
    const uninstallShortcuts = installShortcuts();

    return () => {
      uninstallShortcuts();
      if (timer) clearInterval(timer);
      if (debounceTimer) clearTimeout(debounceTimer);
      clearInterval(jobsBadgeTimer);
      cleanupSSE?.();
    };
  });
  let cleanupSSE: (() => void) | undefined;
</script>

<main class={easterTheme}>
  {#if activity.busy && uiPrefs.activityWave}
    <div class="activity-bar" role="status" aria-label="Working…"></div>
  {/if}
  {#if expiringSoon && !expiryNoticeDismissed}
    <div class="license-banner notice" role="status">
      {#if currentUser?.multi_user && !currentUser?.is_admin}
        <span>
          This workspace's CodeDistill license expires in
          <strong>{daysToExpiry} {daysToExpiry === 1 ? 'day' : 'days'}</strong>
          ({new Date(license!.expires_at ?? '').toLocaleDateString()}) — ask your workspace admin to renew it.
        </span>
      {:else}
        <span>
          Your CodeDistill license expires in
          <strong>{daysToExpiry} {daysToExpiry === 1 ? 'day' : 'days'}</strong>
          ({new Date(license!.expires_at ?? '').toLocaleDateString()}).
          {#if currentUser?.multi_user}Renew it to keep the team's paid features active.{:else}Renew to avoid losing paid features.{/if}
        </span>
      {/if}
      <button class="dismiss" onclick={() => (expiryNoticeDismissed = true)} aria-label="Dismiss expiry notice">×</button>
    </div>
  {/if}
  {#if license && !licenseBannerDismissed && (license.state === 'grace' || license.state === 'expired' || license.state === 'invalid')}
    <div class="license-banner" class:urgent={license.state !== 'grace'} role="status">
      {#if license.state === 'grace'}
        <span>
          Your CodeDistill license has expired — paid features stay active until
          <strong>{new Date(license.grace_until ?? '').toLocaleDateString()}</strong>. Renew to avoid interruption.
        </span>
      {:else if license.state === 'expired'}
        <span>
          License expired — running the free tier. Paid features (MCP, code
          anchors, dedup) are off (your data is untouched). Renew to restore them.
        </span>
      {:else}
        <span>Installed license is invalid: {license.reason}</span>
      {/if}
      <button class="dismiss" onclick={() => (licenseBannerDismissed = true)} aria-label="Dismiss license notice">×</button>
    </div>
  {/if}
  <header
    class="strip"
    use:collapseOnOverflow={(t) => (headerTight = t)}
    use:measureWidth
    class:tight={headerTight || stripW < 1150}
    class:t2={stripW < 950}
    class:t3={stripW < 720}
  >
    <span class="brand">
      <!-- Double-click the mark opens About. The single-click handler is the
           matrix-theme dismissal and no-ops otherwise, so the two coexist. -->
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <svg
        class="logo"
        class:dismissable={matrixTriggered && !matrixDismissed}
        viewBox="0 0 64 64"
        aria-hidden="true"
        onclick={onLogoClick}
        ondblclick={() => (systemOpen = true)}
      >
        <!-- Distillation flask refining to a crystal — the CodeDistill mark.
             A single accent token so the mark adapts to light/dark themes;
             the favicon + PWA PNGs carry the fuller 3-tone version
             (web/public/icons). -->
        <path
          d="M27 13 L37 13 L37 22 L49.5 46 Q51 49 47.7 49 L16.3 49 Q13 49 14.5 46 L27 22 Z"
          fill="var(--p-66ccff)"
          opacity="0.22"
        />
        <path
          d="M27 13 L37 13 L37 22 L49.5 46 Q51 49 47.7 49 L16.3 49 Q13 49 14.5 46 L27 22 Z"
          fill="none"
          stroke="var(--p-66ccff)"
          stroke-width="2.4"
          stroke-linejoin="round"
        />
        <line x1="24.5" y1="13" x2="39.5" y2="13" stroke="var(--p-66ccff)" stroke-width="2.4" stroke-linecap="round" />
        <path d="M32 31 L36.5 36 L32 45 L27.5 36 Z" fill="var(--p-66ccff)" />
      </svg>
      <h1>CodeDistill</h1>
    </span>
    <!-- Menus, from the command registry (docs/design/menubar.md). -->
    <MenuBar menus={MENUS} compact={stripW < 950} />
    <div class="where">
      <Picker
        items={orderProjects(projects)}
        activeId={activeProjectId}
        label="project"
        emptyLabel="No project"
        onSelect={selectProject}
        onCreate={pickerCreateProject}
        onRename={pickerRenameProject}
        onDelete={pickerDeleteProject}
        canDelete={() => projects.length > 1}
      />
      <span class="header-divider" aria-hidden="true">/</span>
      <Picker
        items={scratchpads}
        activeId={activeScratchpadId}
        label="scratchpad"
        emptyLabel="No scratchpad"
        onSelect={selectScratchpad}
        onCreate={pickerCreateScratchpad}
        onRename={pickerRenameScratchpad}
        onDelete={pickerDeleteScratchpad}
        canDelete={() => scratchpads.length > 1}
      />
      <button class="palette-btn" onclick={() => runCommand('view.palette')} title="Command palette — find any command ({shortcutLabel(commandsById(['view.palette'])[0])})">
        <Icon name="search" size={12} /><span>{shortcutLabel(commandsById(['view.palette'])[0])}</span>
      </button>
    </div>
    <div class="header-actions">
      {#each headerCommands as cmd (cmd.id)}
        {#if isVisible(cmd)}
          <button
            class="lists-btn"
            class:on={cmd.active?.() ?? false}
            onclick={() => runCommand(cmd.id)}
            disabled={!isEnabled(cmd)}
            title={commandTitle(cmd)}
            aria-label={cmd.label}
            aria-pressed={cmd.active?.() ?? false}
          >
            {#if cmd.icon}
              <span class="btn-ico" style="--sig:{cmd.color ?? 'currentColor'}"><Icon name={cmd.icon as IconName} size={15} /></span>
            {/if}
            <span class="btn-lbl">{cmd.label}</span>
            {#if (cmd.badge?.() ?? 0) > 0}<span class="badge">{cmd.badge!()}</span>{/if}
          </button>
        {/if}
      {/each}
      <span class="layout-toggles" role="group" aria-label="Panels">
        {#each [['left', 'view.leftPanel'], ['bottom', 'view.bottomPanel'], ['right', 'view.rightPanel']] as [edge, id] (id)}
          {@const cmd = commandsById([id])[0]}
          {#if cmd}
            <button
              class="layout-toggle"
              class:on={cmd.active?.() ?? false}
              onclick={() => runCommand(id)}
              title={`${cmd.label}: ${cmd.description}`}
              aria-label={cmd.label}
              aria-pressed={cmd.active?.() ?? false}
            >
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.2" />
                {#if edge === 'left'}<rect x="1.5" y="2.5" width="4.5" height="11" fill="currentColor" opacity={cmd.active?.() ? 0.9 : 0.3} />{/if}
                {#if edge === 'right'}<rect x="10" y="2.5" width="4.5" height="11" fill="currentColor" opacity={cmd.active?.() ? 0.9 : 0.3} />{/if}
                {#if edge === 'bottom'}<rect x="1.5" y="9.5" width="13" height="4" fill="currentColor" opacity={cmd.active?.() ? 0.9 : 0.3} />{/if}
              </svg>
            </button>
          {/if}
        {/each}
      </span>
      <ProfileMenu
        me={currentUser}
        {license}
        onUpdated={() => api.getMe().then((m) => (currentUser = m)).catch(() => {})}
      />
    </div>
  </header>
  <CommandPalette open={paletteOpen} onClose={() => (paletteOpen = false)} {menuOf} />

  <StatsModal open={statsOpen} onClose={() => (statsOpen = false)} />
  {#if codeContextMenu}
    <ContextMenu x={codeContextMenu.x} y={codeContextMenu.y} ids={CODE_CONTEXT_COMMANDS} onClose={() => (codeContextMenu = null)} />
  {/if}
  <SystemInfoModal open={systemOpen} onClose={() => (systemOpen = false)} />
  {#if setupStatus}
    <SetupScreen status={setupStatus} onDone={() => { setupStatus = null; void refresh(); }} />
  {/if}

  <Modal
    open={settingsOpen}
    title="Settings"
    onClose={() => { settingsOpen = false; settingsTick += 1; settingsSection = 'appearance'; }}
    width="900px"
  >
    {#snippet children()}
      <Settings userId={LOCAL_USER} initial={settingsSection} />
    {/snippet}
  </Modal>

  <Modal
    open={adminOpen}
    title="Workspace Admin"
    onClose={() => (adminOpen = false)}
    width="720px"
  >
    {#snippet children()}
      {#if adminOpen}<AdminConsole />{/if}
    {/snippet}
  </Modal>

  <CreditsModal
    open={creditsOpen}
    build={buildInfo}
    {license}
    onClose={() => (creditsOpen = false)}
  />

<!-- One snippet for every pane, so PaneFrame never learns what a pane
     contains and each component keeps the props it always had. -->
{#snippet canvasControls()}
  <!-- Contextual controls for the canvas, rendered from the scratchpad's own
       commands (docs/design/menubar.md): the view switch always, the rest
       only when there is something to act on. -->
  {#each ['scratchpad.viewCanvas', 'scratchpad.viewList', 'scratchpad.viewCalendar', 'scratchpad.viewKanban'] as id (id)}
    {@const c = commandsById([id])[0]}
    {#if c && isVisible(c) && isEnabled(c)}
      <button class="cc-btn" class:on={c.active?.() ?? false} onclick={() => runCommand(id)} title={commandTitle(c)} aria-label={c.label} aria-pressed={c.active?.() ?? false}>
        {#if c.icon}<Icon name={c.icon as IconName} size={13} />{/if}
      </button>
    {/if}
  {/each}
  <span class="cc-sep" aria-hidden="true"></span>
  {#each ['scratchpad.filters', 'scratchpad.showHidden', 'scratchpad.group', 'agents.fixPad'] as id (id)}
    {@const c = commandsById([id])[0]}
    {@const n = c?.badge?.() ?? 0}
    {@const on = c?.active?.() ?? false}
    {#if c && isVisible(c) && isEnabled(c) && (id === 'scratchpad.filters' || n > 0 || on)}
      <button class="cc-btn text" class:on onclick={() => runCommand(id)} title={commandTitle(c)} aria-pressed={on}>
        {#if id === 'scratchpad.filters'}⛉ Filter{:else if id === 'scratchpad.showHidden'}{n} hidden{:else if id === 'scratchpad.group'}§ Group {n}{:else}Fix {padFindingCount} finding{padFindingCount === 1 ? '' : 's'}{/if}
      </button>
    {/if}
  {/each}
{/snippet}

{#snippet paneContent(id: string)}
  {#if id === 'tree'}
    <Drawer
      activeProject={activeProject}
      onProjectImported={projectCreated}
      onOpenFile={openFileInCanvas}
    />
  {:else if id === 'review'}
    <ChangesetPane
      projectId={activeProjectId}
      changeset={activeChangeset}
      onPick={(cs) => (activeChangeset = cs)}
      onOpenFile={(p) => { if (activeChangeset) openDiffTab(p, activeChangeset); }}
      onOpenItem={(t, id) => void openDockedFull(t, id)}
      tick={reviewTick}
      onReviewChanged={() => { void refreshNeedsReviewCount(); scheduleRefresh(); }}
    />
  {:else if id === 'dock'}
    <DockedItems onOpenFile={openFileInCanvas} onOpenFull={openDockedFull} />
  {:else if id === 'inbox'}
    <NeedsReviewPanel
      projectId={activeProjectId}
      projectName={activeProject?.name ?? ''}
      tick={reviewTick}
      onOpenItem={(ownerType: string, ownerId: string) =>
        void openDockedFull(ownerType as DockedItem['ownerType'], ownerId)}
      onChanged={() => { void refreshNeedsReviewCount(); scheduleRefresh(); }}
      onOpenChangeset={(cs) => { activeChangeset = cs; openPaneById('review'); }}
    />
  {:else if id === 'jobs'}
    <JobMonitor
      projectId={activeProjectId}
      onNewJob={() => (submitOpen = true)}
      onReview={(rid) => (reviewJobId = rid)}
      reloadKey={jobsReload}
      onOpenItem={(ot, oid) => {
        if (ot === 'todo_item' || ot === 'bug_item' || ot === 'use_case_item') void openDockedFull(ot, oid);
      }}
    />
  {/if}
{/snippet}

    <!-- The shell: a panes row, and the bottom slot beneath it.
         Two levels because the bottom slot is a ROW — it spans the full width
         under everything, which is what makes a terminal or command surface
         usable. A column could never do that. -->
    <div class="shell" class:dragging={!!draggingPane()}>
      {#each emptySlots as s (s)}
        <!-- Positioned over the shell rather than in the flow: a strip that took
             layout space would reflow everything the instant a drag started,
             moving the tab out from under the pointer. -->
        <div
          class="empty-slot {s}"
          role="presentation"
          ondragover={allowDrop}
          ondrop={(e) => dropInto(e, s)}
        ><span>Drop here</span></div>
      {/each}
      <div
        class="panes"
        class:resizing={!!resizingPane && resizingAxis === 'x'}
        style:grid-template-columns={paneColumns}
      >
        {#if visibleSlots().includes('left')}
          <PaneFrame slot="left" content={paneContent} />
          <div
            class="resize-handle"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize left pane"
            onpointerdown={(e) => { const id = activePane('left'); if (id) startPaneResize(e, id, 'x'); }}
            onpointermove={(e) => paneResizeMove(e, 'x', false)}
            onpointerup={endPaneResize}
            onpointercancel={endPaneResize}
          ></div>
        {/if}

        <div class="center">
          {#if activeProjectId}
            <!-- The centre's tabs: the canvas first, then every open file
                 (docs/design/center-tabs.md). The row is always there: its
                 right side carries the canvas's own controls
                 (docs/design/menubar.md). -->
          <CodeCanvas
            projectId={activeProjectId}
            tabs={codeTabs}
            activeIndex={codeActiveIndex}
            onActivate={activateCodeTab}
            onClose={closeCodeTab}
            onHide={hideCodeCanvas}
            canvasTab={{ label: activePad?.name ?? 'Canvas', active: !codeVisible || codeTabs.length === 0, onSelect: hideCodeCanvas }}
      trailing={(!codeVisible || codeTabs.length === 0) ? canvasControls : undefined}
            onAnchorClick={handleAnchorClick}
            onCreateItem={createItemFromCodeSelection}
            hasActiveScratchpad={!!activeScratchpadId}
            {pendingHighlight}
            onHighlightConsumed={() => (pendingHighlight = null)}
            externalHover={hoveredCardAnchor}
            pickerOpen={changesetPickerOpen}
            onPickerOpen={openChangesetPicker}
            onPickerClose={() => (changesetPickerOpen = false)}
            onPicked={onChangesetPicked}
            onContextMenu={(e) => (codeContextMenu = { x: e.clientX, y: e.clientY })}
            onOpenDiffFile={(p, d) => openDiffTab(p, d)}
            onOpenChangeset={showChangeset}
            onOpenItem={(t, id) => void openDockedFull(t, id)}
          />
          {/if}
        <section class="scratchpad" class:hidden={codeVisible && codeTabs.length > 0}>
      <ScratchpadPane
        {items}
        {scratchpads}
        {activeScratchpadId}
        projectId={activeProjectId}
        dedupEnabled={licensed('dedup')}
    canvasViewsEnabled={licensed('canvas_views')}
    {blockedBySource}
    {dueDateBySource}
    {tagsBySource}
    onOpenDerived={openDerivedFromItem}
        {hiddenCount}
        {doneSourceIds}
        {settingsTick}
        derivedStatus={derivedStatusBySource}
        {latestNoteBySource}
        onChange={refresh}
        {flashItemId}
        {itemAnchors}
        onCardAnchorClick={openCardAnchor}
        onCardAnchorHover={(a) => (hoveredCardAnchor = a)}
        onOpenSimilar={openSimilarItem}
        onDecompose={(item) => {
          submitPrefill = { source_item: item.id };
          submitOpen = true;
        }}
        {fixable}
        onFixWithAgent={(item) => fixWithAgent(item.scratchpad_id, [item.id])}
        onFixPad={() => fixWithAgent(activeScratchpadId, [])}
      />
        </section>
        </div>

        {#if visibleSlots().includes('right')}
          <div
            class="resize-handle"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize right pane"
            onpointerdown={(e) => { const id = activePane('right'); if (id) startPaneResize(e, id, 'x'); }}
            onpointermove={(e) => paneResizeMove(e, 'x', true)}
            onpointerup={endPaneResize}
            onpointercancel={endPaneResize}
          ></div>
          <PaneFrame slot="right" content={paneContent} />
        {/if}
      </div>

      {#if visibleSlots().includes('bottom')}
        <div
          class="resize-handle horizontal"
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize bottom pane"
          onpointerdown={(e) => { const id = activePane('bottom'); if (id) startPaneResize(e, id, 'y'); }}
          onpointermove={(e) => paneResizeMove(e, 'y', true)}
          onpointerup={endPaneResize}
          onpointercancel={endPaneResize}
        ></div>
        <div class="bottom-slot" style:height={`${paneWidth(activePane('bottom') ?? '') || 200}px`}>
          <PaneFrame slot="bottom" content={paneContent} />
        </div>
      {/if}
    </div>

    <!-- Status, not actions (docs/design/menubar.md): what the header used to
         carry beside its buttons. -->
    <footer class="statusbar">
      <span class="meta">
        <span class="stat"><b>{bannerCounts.total}</b> items</span>
        {#if bannerCounts.inbox}<span class="stat"><b>{bannerCounts.inbox}</b> inbox</span>{/if}
        {#if bannerCounts.todos}<span class="stat"><b>{bannerCounts.todos}</b> todos</span>{/if}
        {#if bannerCounts.bugs}<span class="stat"><b>{bannerCounts.bugs}</b> bugs</span>{/if}
        {#if bannerCounts.kb}<span class="stat"><b>{bannerCounts.kb}</b> kb</span>{/if}
        {#if bannerCounts.useCases}<span class="stat"><b>{bannerCounts.useCases}</b> use-cases</span>{/if}
        {#if bannerCounts.other}<span class="stat"><b>{bannerCounts.other}</b> other</span>{/if}
      </span>
      {#if errorMsg}<span class="err" title={errorMsg}>{errorMsg}</span>{/if}
      <span class="sb-spacer"></span>
      {#if jobsRunningCount > 0}
        <button class="sb-item" onclick={() => runCommand('view.jobs')} title="Open the job monitor">
          ⏳ {jobsRunningCount} job{jobsRunningCount === 1 ? '' : 's'} running
        </button>
      {/if}
      {#if securityStale.length}
        <button
          class="sb-item warn"
          onclick={() => runCommand('agents.securityKnowledge')}
          title={`Security knowledge out of date: ${securityStale.join(', ')}. The security agent still rules, and says its data is old.`}
        >⚠ Security knowledge out of date</button>
      {/if}
      <PowerBadge onClick={() => (settingsOpen = true)} />
      {#if buildInfo}
        <button
          class="sb-item version"
          onclick={() => runCommand('help.credits')}
          title="About CodeDistill — {buildInfo.version} ({buildInfo.git_sha}, {buildInfo.build_date})"
        >v{buildInfo.version}</button>
      {/if}
    </footer>

  <!-- Detail modals — owned by App so the code canvas can open them too. -->
  <TodoDetail
    todo={openTodo}
    onClose={() => (openTodo = null)}
    onSaved={refresh}
    onOpenFile={(path, revision) => { const id = openTodo?.id; openTodo = null; if (id) followItemCode('todo_item', id, path, revision); }}
  />
  <BugDetail
    bug={openBug}
    onClose={() => (openBug = null)}
    onSaved={refresh}
    onOpenFile={(path, revision) => { const id = openBug?.id; openBug = null; if (id) followItemCode('bug_item', id, path, revision); }}
  />
  <UseCaseDetail
    useCase={openUseCase}
    onClose={() => (openUseCase = null)}
    onSaved={refresh}
    onOpenFile={(path, revision) => { const id = openUseCase?.id; openUseCase = null; if (id) followItemCode('use_case_item', id, path, revision); }}
  />
  <KbDetail
    kb={openKb}
    onClose={() => (openKb = null)}
    onSaved={() => { void refresh(); void loadProjectLists(); }}
    onOpenFile={(path, revision) => { openKb = null; openFileInCanvas(path, revision); }}
  />

  <ListsPanel
    open={listsOpen}
    activeTab={listsActiveTab}
    projectId={activeProjectId}
    todos={projectTodos}
    bugs={projectBugs}
    kb={projectKb}
    useCases={projectUseCases}
    onTabChange={(t) => (listsActiveTab = t)}
    onClose={closeLists}
    onChange={() => { void refresh(); void loadProjectLists(); }}
    onOpenTodo={(t) => (openTodo = t)}
    onOpenBug={(b) => (openBug = b)}
    onOpenUseCase={(u) => (openUseCase = u)}
    onOpenKB={(k) => (openKb = k)}
  />

  <SearchPanel
    open={searchOpen}
    projectId={activeProjectId}
    onClose={closeSearch}
    onOpenScratchpadItem={searchOpenScratchpadItem}
    onOpenTodo={searchOpenTodo}
    onOpenBug={searchOpenBug}
    onOpenUseCase={searchOpenUseCase}
    onOpenKB={searchOpenKB}
  />

  <QAPanel
    open={qaOpen}
    projectId={activeProjectId}
    onClose={closeQA}
    onOpenScratchpadItem={searchOpenScratchpadItem}
    onOpenTodo={searchOpenTodo}
    onOpenBug={searchOpenBug}
    onOpenUseCase={searchOpenUseCase}
    onOpenKB={searchOpenKB}
    onOpenCodeChunk={qaOpenCodeChunk}
  />


  <DashboardPanel
    open={dashboardOpen}
    projectId={activeProjectId}
    projectName={activeProject?.name ?? ''}
    onClose={closeDashboard}
    onOpenNeedsReview={() => { dashboardOpen = false; openPaneById('inbox'); }}
  />



  <JobSubmit
    projectId={activeProjectId}
    open={submitOpen}
    initialWorkflow={submitWorkflow || (Object.keys(submitPrefill).length ? 'decompose' : undefined)}
    prefill={submitPrefill}
    prefillLabels={submitLabels}
    onClose={() => { submitOpen = false; submitPrefill = {}; submitWorkflow = ''; submitLabels = {}; }}
    onSubmitted={() => {
      // Open the panel on submit: the run has to be visible somewhere, and
      // starting one only to have nothing change is how a button looks broken.
      openPaneById('jobs');
      jobsReload += 1;
    }}
  />

  <ProposalReview
    jobId={reviewJobId}
    projectId={activeProjectId}
    onClose={() => (reviewJobId = null)}
    onApplied={() => { void refresh(); void loadProjectLists(); }}
  />

  <ArchitecturePanel
    open={architectureOpen}
    projectId={activeProjectId}
    projectName={activeProject?.name ?? ''}
    onClose={closeArchitecture}
    onPublished={scheduleRefresh}
  />

  <DataModelPanel
    open={dataModelOpen}
    projectId={activeProjectId}
    projectName={activeProject?.name ?? ''}
    onClose={closeDataModel}
    onPublished={scheduleRefresh}
  />

  <CodeAnalysisPanel
    open={analysisOpen}
    projectId={activeProjectId}
    projectName={activeProject?.name ?? ''}
    scratchpadId={activeScratchpadId}
    scratchpadName={activePad?.name ?? ''}
    onClose={closeAnalysis}
    onPushed={scheduleRefresh}
    onFixWithAgent={licensed('mcp') ? (itemId, padId, label) => { closeAnalysis(); fixWithAgent(padId, [itemId], { [itemId]: label }); } : undefined}
  />

  <HelpPanel open={helpOpen} onClose={closeHelp} {license} />

  <!-- Themed alert/confirm/prompt host (lib/dialog.ts) — replaces the
       cheap-looking native browser popups app-wide. -->
  <DialogHost />
</main>

<style>
  /* Global activity bar (item #20): an indeterminate sliding bar at the very
     top whenever any API request is in flight — instant "something's happening"
     feedback for every async action, app-wide. */
  .activity-bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 2px;
    z-index: 1000;
    background: linear-gradient(90deg, transparent, var(--p-99ccff), transparent);
    background-size: 40% 100%;
    background-repeat: no-repeat;
    animation: activity-slide 0.9s linear infinite;
    pointer-events: none;
  }
  @keyframes activity-slide {
    0% { background-position: -40% 0; }
    100% { background-position: 140% 0; }
  }
  /* License renewal / degradation banner. Amber for grace (still
     working), red-tinted for expired/invalid (features off). */
  .license-banner {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 14px;
    font-size: 13px;
    background: var(--p-3a2f10);
    color: var(--p-f0d080);
    border-bottom: 1px solid var(--p-5a4a18);
  }
  .license-banner.urgent {
    background: var(--p-3a1a14);
    color: var(--p-f0a090);
    border-bottom-color: var(--p-5a2a20);
  }
  /* Pre-expiry heads-up: calmer blue — informational, nothing's broken yet. */
  .license-banner.notice {
    background: var(--p-10202a);
    color: var(--p-99ccff);
    border-bottom-color: var(--p-1e3a52);
  }
  .license-banner span { flex: 1; }
  .license-banner .dismiss {
    background: transparent;
    border: none;
    color: inherit;
    font-size: 16px;
    cursor: pointer;
    padding: 0 4px;
  }

  main {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  /* One low strip (docs/design/menubar.md): brand, menus, where you are,
     the two queues, the panel toggles, you. */
  header.strip {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 34px;
    padding: 0 8px 0 10px;
    background: var(--p-0d0d0d);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .where {
    flex: 1 1 auto; min-width: 0;
    display: flex; align-items: center; justify-content: center; gap: 6px;
  }
  .palette-btn {
    display: inline-flex; align-items: center; gap: 4px; margin-left: 6px;
    background: var(--p-161616); border: 1px solid var(--p-2a2a2a); border-radius: 4px;
    color: var(--text-dim); font: inherit; font-size: 0.72rem; padding: 2px 8px; cursor: pointer;
  }
  .palette-btn:hover { color: var(--text); border-color: var(--p-3a3a3a); }
  header.tight .palette-btn span { display: none; }
  header.t2 h1 { display: none; }
  header.t3 .layout-toggles { display: none; }
  .where > :global(*) { min-width: 0; }
  .header-actions { flex-shrink: 0; }
  .brand, .palette-btn { flex-shrink: 0; }
  .layout-toggles { display: inline-flex; gap: 1px; margin: 0 4px; }
  .layout-toggle {
    display: inline-flex; align-items: center; justify-content: center;
    width: 24px; height: 22px; background: none; border: 0; border-radius: 4px;
    color: var(--text-dim); cursor: pointer; padding: 0;
  }
  .layout-toggle svg { width: 15px; height: 15px; }
  .layout-toggle:hover, .layout-toggle.on { color: var(--text); background: var(--p-1f1f1f); }
  .statusbar {
    display: flex; align-items: center; gap: 12px; height: 22px; padding: 0 10px;
    background: var(--p-0d0d0d); border-top: 1px solid var(--p-262626);
    font-size: 11px; color: var(--p-888888); flex-shrink: 0; overflow: hidden; white-space: nowrap;
  }
  .statusbar .err { overflow: hidden; text-overflow: ellipsis; }
  .sb-spacer { flex: 1; }
  .sb-item {
    background: none; border: 0; color: inherit; font: inherit; cursor: pointer;
    padding: 0 4px; border-radius: 3px; height: 18px;
  }
  .sb-item:hover { color: var(--text); background: var(--p-1f1f1f); }
  .sb-item.warn { color: var(--warn, #e0a030); }
  .sb-item.version { color: var(--p-555555); font-variant-numeric: tabular-nums; }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    flex-shrink: 0;
  }
  .logo { width: 18px; height: 18px; flex-shrink: 0; }
  /* Only the matrix easter-egg activates this hint — outside of that
     the logo is a plain decoration with no interactivity. */
  .logo.dismissable { cursor: pointer; }
  .logo.dismissable:hover { opacity: 0.7; }
  h1 {
    font-size: 12.5px;
    margin: 0;
    font-weight: 600;
    letter-spacing: 1px;
    color: var(--p-66ccff);
  }
  .version {
    color: var(--p-555555);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    margin-left: 2px;
    cursor: help;
  }
  .header-divider {
    color: var(--p-444444);
    font-size: 18px;
    user-select: none;
  }
  .meta {
    display: inline-flex;
    align-items: center;
    font-size: 11px;
    color: var(--p-888888);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .meta .stat { display: inline-flex; align-items: baseline; gap: 3px; }
  .meta .stat b { color: var(--p-cccccc); font-weight: 600; }
  .meta .stat + .stat::before { content: "·"; margin: 0 6px; opacity: 0.45; }
  .err { color: var(--p-ff8888); font-size: 11px; }
  /* Signature-hue icons: colour the glyph, not the label. */
  .lists-btn .btn-ico { display: inline-flex; align-items: center; color: var(--sig, currentColor); }
  /* Overflow-driven (not a viewport breakpoint — those rot as buttons are
     added): when the header measures itself as overflowing, labels collapse
     to icons (words become tooltips). See the headerTight observer. */
  header.tight .lists-btn .btn-lbl { display: none; }
  header.tight .lists-btn { padding: 5px 8px; }
  .header-actions {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .lists-btn {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-aaaaaa);
    font-size: 12px;
    line-height: 1;
    padding: 5px 10px;
    cursor: pointer;
    border-radius: 3px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .lists-btn:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .lists-btn:disabled { opacity: 0.45; cursor: default; }
  .lists-btn:disabled:hover { color: var(--p-aaaaaa); background: transparent; }
  .lists-btn.on,
  .lists-btn[aria-pressed="true"] {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .lists-btn .badge {
    background: var(--p-2d6840);
    color: var(--p-d8fdd0);
    font-size: 10px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 8px;
    min-width: 14px;
    text-align: center;
    line-height: 1.3;
  }
  /* Two levels: a panes row, and the bottom slot under it.
     The bottom slot is a ROW because it must span the full width — a terminal
     or command surface squeezed into a column is unusable. */
  .shell {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    position: relative;
  }

  /* Only present during a drag, so it can sit over the content without ever
     being in the way the rest of the time. */
  .empty-slot {
    position: absolute;
    z-index: 30;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(90, 140, 200, 0.14);
    border: 2px dashed var(--accent);
    color: var(--p-cccccc);
    font-size: 0.75rem;
    pointer-events: auto;
  }
  .empty-slot span { opacity: 0.85; }
  .empty-slot.left { top: 0; bottom: 0; left: 0; width: 170px; }
  .empty-slot.right { top: 0; bottom: 0; right: 0; width: 170px; }
  .empty-slot.bottom { left: 0; right: 0; bottom: 0; height: 110px; }
  .panes {
    flex: 1;
    display: grid;
    min-height: 0;
  }
  .panes.resizing {
    cursor: col-resize;
    user-select: none;
  }
  .bottom-slot {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-top: 1px solid var(--border);
  }
  .resize-handle.horizontal {
    height: 5px;
    width: auto;
    cursor: row-resize;
  }
  .drawer-aside,
  .code-aside {
    border-right: 1px solid var(--p-333333);
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .jobs-aside {
    border-left: 1px solid var(--p-333333);
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .dock-aside {
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .resize-handle {
    background: transparent;
    cursor: col-resize;
    position: relative;
    z-index: 1;
    transition: background-color 120ms ease;
  }
  /* Non-interactive 5px separator that fills the dock's grid gap. */
  .resize-handle-static { background: var(--p-1a1a1a); }
  .resize-handle:hover,
  .panes.resizing .resize-handle {
    background: var(--p-2d5578);
  }
  .scratchpad {
    min-height: 0;
    overflow: hidden;
  }
  .stale-chip { color: var(--warn, #e0a030); border-color: var(--warn, #e0a030); }
  /* The centre: the file tabs (when any are open) above the canvas or the
     code. The canvas stays mounted while a file is showing. */
  .center { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .center > :global(.canvas) { flex: 1 1 auto; min-height: 0; }
  .center > :global(.canvas.strip-only) { flex: 0 0 auto; }
  .center > .scratchpad { flex: 1 1 auto; min-height: 0; }
  .scratchpad.hidden { display: none; }
  .cc-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 4px;
    height: 22px; min-width: 24px; padding: 0 5px; background: none; border: 0; border-radius: 4px;
    color: var(--text-dim); font: inherit; font-size: 11px; cursor: pointer;
  }
  .cc-btn:hover { color: var(--text); background: var(--p-1f1f1f); }
  .cc-btn.on { color: var(--accent); background: var(--p-1f1f1f); }
  .cc-sep { width: 1px; height: 14px; background: var(--p-2a2a2a); margin: 0 4px; }
</style>
