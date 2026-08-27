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
  import { onMount, onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import type {
    TodoItem, BugItem, KnowledgeEntry, UseCaseItem,
  } from '../lib/types';
  import { loadUserBool, saveUserBool } from '../lib/userSettings';
  import TodoPane from './TodoPane.svelte';
  import BugPane from './BugPane.svelte';
  import KbPane from './KbPane.svelte';
  import UseCasePane from './UseCasePane.svelte';

  // Persistence key for the "open only" toggle. Default true so the
  // panel feels uncluttered on fresh installs; user can flip to see
  // history.
  const OPEN_ONLY_KEY = 'ui.lists.open_only';

  // A todo/bug/use_case is "done" — the dimensions we hide when
  // openOnly is on. KB has no status field; the toggle is a no-op
  // on the KB tab (we still show the toggle for consistency).
  function isTodoDone(t: TodoItem): boolean {
    return t.status === 'complete';
  }
  function isBugDone(b: BugItem): boolean {
    // Any terminal status counts as "done" for the open-only filter —
    // fix-type (fixed/verified/closed) and non-fix-type
    // (not_a_bug/wont_fix/duplicate) alike.
    return b.status === 'fixed' || b.status === 'verified' || b.status === 'closed'
        || b.status === 'not_a_bug' || b.status === 'wont_fix' || b.status === 'duplicate';
  }
  function isUseCaseDone(u: UseCaseItem): boolean {
    return u.status === 'completed' || u.status === 'rejected';
  }

  // Right-side slide-in surface for project-wide derived items. Replaces
  // the drawer's old per-pad Todos / Bugs / KB / Use Cases sub-tabs from
  // the pre-redesign drawer. Reuses the existing Pane components so the
  // detail-modal-on-click flow (lifted to App.svelte in stage 3 of the
  // code-canvas experiment) keeps working unchanged.
  //
  // App owns the data + the active tab; this component is purely
  // presentational. KB still uses externalExpandId for the code-canvas
  // anchor-click flow.

  type Tab = 'todos' | 'bugs' | 'kb' | 'use-cases';

  type Props = {
    open: boolean;
    activeTab: Tab;
    projectId: string;
    todos: TodoItem[];
    bugs: BugItem[];
    kb: KnowledgeEntry[];
    useCases: UseCaseItem[];
    onTabChange: (t: Tab) => void;
    onClose: () => void;
    onChange: () => void;
    onOpenTodo: (t: TodoItem) => void;
    onOpenBug: (b: BugItem) => void;
    onOpenUseCase: (u: UseCaseItem) => void;
    onOpenKB: (k: KnowledgeEntry) => void;
  };
  let {
    open,
    activeTab,
    projectId,
    todos,
    bugs,
    kb,
    useCases,
    onTabChange,
    onClose,
    onChange,
    onOpenTodo,
    onOpenBug,
    onOpenUseCase,
    onOpenKB,
  }: Props = $props();

  // Done-loading callbacks for the per-pane "Show done" toggles. Each
  // calls the project endpoint with include_done=true so the pane
  // gets the full list (it filters down to terminal status itself).
  const loadDoneTodos    = () => api.listTodos(projectId, true);
  const loadDoneBugs     = () => api.listBugs(projectId, true);
  const loadDoneKB       = () => api.listKB(projectId, true);
  const loadDoneUseCases = () => api.listUseCases(projectId, true);

  // Review-queue flags: which items the Dashboard queue flagged for attention
  // (needs_review), and whether that's because they're unanchored (no recorded
  // code location). Badged on each pane so attention-needed work shows in
  // context, not only on the Dashboard. Refetched when the lists change
  // (marking an item done adds/removes flags). Map value = unanchored.
  let reviewFlags = $state<Map<string, boolean>>(new Map());
  $effect(() => {
    // Reference deps so the fetch re-runs when items change.
    void [projectId, todos.length, bugs.length, useCases.length];
    if (!projectId) return;
    let cancelled = false;
    api
      .getReviewQueue(projectId)
      .then((q) => {
        if (cancelled) return;
        const m = new Map<string, boolean>();
        for (const it of q.items) if (it.needs_review) m.set(it.id, !!it.unanchored);
        reviewFlags = m;
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  });

  // Show-open-only toggle state. Loaded on mount from user_settings;
  // every toggle saves fire-and-forget.
  let openOnly = $state(true);
  function toggleOpenOnly() {
    openOnly = !openOnly;
    saveUserBool(OPEN_ONLY_KEY, openOnly);
  }

  // Filtered lists derived from props + toggle. KB is always full (no
  // status). Total counts shown in the tab strip use the filtered
  // lengths so the badge matches what's rendered below.
  let filteredTodos    = $derived(openOnly ? todos.filter((t) => !isTodoDone(t)) : todos);
  let filteredBugs     = $derived(openOnly ? bugs.filter((b) => !isBugDone(b)) : bugs);
  let filteredUseCases = $derived(openOnly ? useCases.filter((u) => !isUseCaseDone(u)) : useCases);
  // Show how many are hidden so the toggle's effect is visible.
  let hiddenTodos    = $derived(todos.length - filteredTodos.length);
  let hiddenBugs     = $derived(bugs.length - filteredBugs.length);
  let hiddenUseCases = $derived(useCases.length - filteredUseCases.length);

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) onClose();
  }
  onMount(async () => {
    document.addEventListener('keydown', onKey);
    openOnly = await loadUserBool(OPEN_ONLY_KEY, true);
  });
  onDestroy(() => document.removeEventListener('keydown', onKey));
</script>

<aside class="lists" class:open aria-hidden={!open} aria-label="Project lists">
  <header class="head">
    <span class="title">Project lists</span>
    <button
      type="button"
      class="x"
      onclick={onClose}
      aria-label="Close lists panel"
      title="Close (Esc)"
    >×</button>
  </header>

  <div class="tabs" role="tablist">
    <button
      type="button"
      role="tab"
      class:on={activeTab === 'todos'}
      aria-selected={activeTab === 'todos'}
      onclick={() => onTabChange('todos')}
    >Todos <span class="ct">({filteredTodos.length})</span></button>
    <button
      type="button"
      role="tab"
      class:on={activeTab === 'bugs'}
      aria-selected={activeTab === 'bugs'}
      onclick={() => onTabChange('bugs')}
    >Bugs <span class="ct">({filteredBugs.length})</span></button>
    <button
      type="button"
      role="tab"
      class:on={activeTab === 'kb'}
      aria-selected={activeTab === 'kb'}
      onclick={() => onTabChange('kb')}
    >KB <span class="ct">({kb.length})</span></button>
    <button
      type="button"
      role="tab"
      class:on={activeTab === 'use-cases'}
      aria-selected={activeTab === 'use-cases'}
      onclick={() => onTabChange('use-cases')}
    >Use Cases <span class="ct">({filteredUseCases.length})</span></button>
  </div>

  <div class="filter-row">
    <label class="filter-toggle">
      <input type="checkbox" checked={openOnly} onchange={toggleOpenOnly} />
      <span>Show open only</span>
    </label>
    {#if openOnly}
      {@const hiddenTotal = hiddenTodos + hiddenBugs + hiddenUseCases}
      {#if hiddenTotal > 0}
        <span class="hidden-count">({hiddenTotal} hidden)</span>
      {/if}
    {/if}
  </div>

  <div class="content">
    {#if activeTab === 'todos'}
      <TodoPane todos={filteredTodos} {onChange} onOpenDetail={onOpenTodo} loadDone={loadDoneTodos} flags={reviewFlags} />
    {:else if activeTab === 'bugs'}
      <BugPane bugs={filteredBugs} {onChange} onOpenDetail={onOpenBug} loadDone={loadDoneBugs} flags={reviewFlags} />
    {:else if activeTab === 'kb'}
      <KbPane
        {kb}
        {onChange}
        onOpenDetail={onOpenKB}
        loadDone={loadDoneKB}
      />
    {:else if activeTab === 'use-cases'}
      <UseCasePane useCases={filteredUseCases} {onChange} onOpenDetail={onOpenUseCase} loadDone={loadDoneUseCases} flags={reviewFlags} />
    {/if}
  </div>
</aside>

<style>
  .lists {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 380px;
    max-width: 90vw;
    background: var(--p-0d0d0d);
    border-left: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    transform: translateX(100%);
    transition: transform 220ms ease;
    z-index: 200;
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.45);
    pointer-events: none;
  }
  .lists.open {
    transform: translateX(0);
    pointer-events: auto;
  }
  .head {
    display: flex;
    align-items: center;
    padding: 8px 10px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .title {
    flex: 1;
    font-size: 12px;
    color: var(--p-dddddd);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .x {
    background: transparent;
    border: none;
    color: var(--p-888888);
    font-size: 18px;
    line-height: 1;
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .x:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .tabs {
    display: flex;
    background: var(--p-0a0a0a);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .tabs button {
    flex: 1;
    background: transparent;
    border: none;
    border-right: 1px solid var(--p-1e1e1e);
    color: var(--p-aaaaaa);
    padding: 8px 10px;
    font-size: 11px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    border-radius: 0;
  }
  .tabs button:last-child { border-right: none; }
  .tabs button:hover { background: var(--p-161616); color: var(--p-dddddd); }
  .tabs button.on {
    background: var(--p-1a1a1a);
    color: var(--p-ffffff);
    border-bottom: 2px solid var(--p-66ccff);
    margin-bottom: -1px;
  }
  .ct {
    font-size: 10px;
    color: var(--p-666666);
    font-variant-numeric: tabular-nums;
  }
  .filter-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    background: var(--p-0a0a0a);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .filter-toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: var(--p-aaaaaa);
    cursor: pointer;
    user-select: none;
  }
  .filter-toggle input { cursor: pointer; }
  .filter-toggle:hover { color: var(--p-dddddd); }
  .hidden-count {
    font-size: 10px;
    color: var(--p-666666);
    font-style: italic;
    font-variant-numeric: tabular-nums;
  }
  .content {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }
</style>
