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
  import { alertDialog } from '../lib/dialog';
  import * as api from '../lib/api';
  import type { TodoItem } from '../lib/types';
  import { isTodoDone, isTodoInProgress, todoTerminalLabel } from '../lib/lifecycle';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import InProgressBadge from './InProgressBadge.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import { deleteSyncWarning } from '../lib/sync';

  // The parent fetches `todos` (open items only — the backend filters
  // done by default since v0.8.3). When the user toggles "Show done"
  // we ask the parent for done items via the loadDone callback (so
  // the parent decides whether to call /scratchpads/{id}/todos or
  // /projects/{pid}/todos with include_done=true). We don't push the
  // toggle up to the parent's normal refresh cycle.
  type Props = {
    todos: TodoItem[];
    onChange: () => void;
    onOpenDetail: (todo: TodoItem) => void;
    loadDone?: () => Promise<TodoItem[]>;
    // flags: item id → unanchored, for items the review queue flagged.
    flags?: Map<string, boolean>;
  };
  let { todos, onChange, onOpenDetail, loadDone, flags }: Props = $props();

  let pendingDelete = $state<TodoItem | null>(null);
  let showDone = $state(false);
  let doneItems = $state<TodoItem[]>([]);
  let doneLoading = $state(false);

  async function refreshDone() {
    if (!loadDone) return;
    doneLoading = true;
    try {
      const all = await loadDone();
      doneItems = all.filter((t) => isTodoDone(t.status));
    } catch (e) {
      console.warn('load done todos:', e);
      doneItems = [];
    } finally {
      doneLoading = false;
    }
  }

  async function toggleShowDone() {
    showDone = !showDone;
    if (showDone) {
      await refreshDone();
    } else {
      doneItems = [];
    }
  }

  async function toggle(t: TodoItem) {
    try {
      await api.updateTodo(t.id, {
        status: t.status === 'complete' ? 'incomplete' : 'complete',
      });
      onChange();
      if (showDone) await refreshDone();
    } catch (e) {
      void alertDialog(`Toggle failed: ${e}`);
    }
  }

  async function doDelete() {
    const t = pendingDelete;
    pendingDelete = null;
    if (!t) return;
    try {
      await api.deleteTodo(t.id);
      onChange();
      if (showDone) await refreshDone();
    } catch (e) {
      void alertDialog(`Delete failed: ${e}`);
    }
  }
</script>

<div class="header">
  <button
    type="button"
    class="show-done"
    onclick={toggleShowDone}
    title={showDone ? 'Hide done todos' : 'Show done todos'}
  >
    {showDone ? '− Hide done' : '+ Show done'}
    {#if showDone && !doneLoading} ({doneItems.length}){/if}
  </button>
</div>

<ul>
  {#each todos as t (t.id)}
    <li class:in-progress={isTodoInProgress(t.status)}>
      <input
        type="checkbox"
        checked={t.status === 'complete'}
        onchange={() => toggle(t)}
      />
      <button
        class="subject"
        title="Open details"
        onclick={() => onOpenDetail(t)}
      >{t.source_name || t.subject}</button>
      {#if flags?.has(t.id)}
        <span class="rq-mark" class:unanchored={flags.get(t.id)}
          title={flags.get(t.id) ? 'Flagged for review — no code location was recorded' : 'Flagged for review'}>⚠</span>
      {/if}
      {#if isTodoInProgress(t.status)}
        <InProgressBadge claimedBy={t.claimed_by} claimedAt={t.claimed_at} />
      {/if}
      <SyncIndicator syncStatus={t.sync_status} lastSyncAt={t.last_sync_at} lastSyncError={t.last_sync_error} />
      {#if t.priority !== 'none'}
        <span class="pri pri-{t.priority}">{t.priority}</span>
      {/if}
      {#if t.origin === 'agent-derived'}
        <span class="origin" title="created by agent">★</span>
      {/if}
      <button class="del" onclick={() => (pendingDelete = t)} title="Delete">×</button>
    </li>
  {/each}
  {#if todos.length === 0}
    <li class="empty">No todos yet.</li>
  {/if}

  {#if showDone && doneItems.length > 0}
    <li class="divider" aria-hidden="true">
      <span>── Done ──</span>
    </li>
    {#each doneItems as t (t.id)}
      <li class="done">
        <input
          type="checkbox"
          checked
          onchange={() => toggle(t)}
        />
        <button
          class="subject"
          title="Open details"
          onclick={() => onOpenDetail(t)}
        >{t.source_name || t.subject}</button>
        {#if flags?.has(t.id)}
          <span class="rq-mark" class:unanchored={flags.get(t.id)}
            title={flags.get(t.id) ? 'Flagged for review — no code location was recorded' : 'Flagged for review'}>⚠</span>
        {/if}
        <span class="terminal">{todoTerminalLabel(t.status)}</span>
        <SyncIndicator syncStatus={t.sync_status} lastSyncAt={t.last_sync_at} lastSyncError={t.last_sync_error} />
        {#if t.claimed_by}
          <span class="claimer" title={`Last claimed by ${t.claimed_by}`}>by {t.claimed_by}</span>
        {/if}
        <button class="del" onclick={() => (pendingDelete = t)} title="Delete">×</button>
      </li>
    {/each}
  {/if}
</ul>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete todo"
  message={pendingDelete
    ? `Delete “${pendingDelete.subject}”? This cannot be undone.${deleteSyncWarning(pendingDelete)}`
    : ''}
  onConfirm={doDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .header {
    display: flex;
    justify-content: flex-end;
    padding: 4px 6px;
  }
  .show-done {
    background: transparent;
    border: 0;
    color: var(--p-888888);
    font-size: 11px;
    cursor: pointer;
    padding: 2px 4px;
  }
  .show-done:hover { color: var(--p-99ccff); }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  li {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 6px;
    border-bottom: 1px solid var(--p-2a2a2a);
    font-size: 12px;
  }
  li.done {
    opacity: 0.6;
  }
  li.done .subject {
    text-decoration: line-through;
    color: var(--p-888888);
  }
  li.divider {
    justify-content: center;
    color: var(--p-666666);
    border-bottom: none;
    padding-top: 12px;
    padding-bottom: 4px;
    font-size: 11px;
  }
  .rq-mark { color: var(--p-d4a54d); font-size: 12px; flex: none; cursor: default; }
  .rq-mark.unanchored { color: var(--p-e07a7a); }
  .subject {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: left;
    background: transparent;
    border: none;
    padding: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .subject:hover { color: var(--p-99ccff); text-decoration: underline; }
  .pri {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 2px;
    text-transform: uppercase;
    font-weight: 600;
    letter-spacing: 0.5px;
  }
  .pri-high { background: var(--p-5a1515); color: var(--p-ffdddd); }
  .pri-medium { background: var(--p-4a3a10); color: var(--p-ffeedd); }
  .pri-low { background: var(--p-153a4a); color: var(--p-ddeeff); }
  .origin {
    color: var(--p-99ccff);
    font-size: 11px;
  }
  .terminal {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 2px;
    text-transform: uppercase;
    font-weight: 500;
    letter-spacing: 0.5px;
    background: var(--p-2a2a2a);
    color: var(--p-aaaaaa);
  }
  .claimer {
    font-size: 10px;
    color: var(--p-777777);
    font-style: italic;
  }
  .del {
    border: none;
    background: transparent;
    color: var(--p-666666);
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
  }
  .del:hover { color: var(--p-ffffff); }
  .empty {
    justify-content: center;
    color: var(--p-666666);
    padding: 20px;
    border-bottom: none;
  }
</style>
