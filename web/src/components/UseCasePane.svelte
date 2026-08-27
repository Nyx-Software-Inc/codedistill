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
  import type { UseCaseItem, UseCaseStatus } from '../lib/types';
  import { isUseCaseDone, isUseCaseInProgress, useCaseTerminalLabel } from '../lib/lifecycle';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import InProgressBadge from './InProgressBadge.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import { deleteSyncWarning } from '../lib/sync';

  type Props = {
    useCases: UseCaseItem[];
    onChange: () => void;
    onOpenDetail: (uc: UseCaseItem) => void;
    loadDone?: () => Promise<UseCaseItem[]>;
    // flags: item id → unanchored, for items the review queue flagged.
    flags?: Map<string, boolean>;
  };
  let { useCases, onChange, onOpenDetail, loadDone, flags }: Props = $props();

  // Distinct per-status glyphs — the old first-letter scheme rendered
  // both in_progress and implemented as "I" (Backlog bug #3).
  const STATUS_GLYPH: Record<string, string> = {
    open: '○',
    approved: '◇',
    in_progress: '◐',
    completed: '✓',
    rejected: '✕',
  };

  // In-row workflow (new-bug fix, v0.10.13): the detail modal always
  // had the status dropdown + Mark implemented, but the rows offered
  // no control at all — bugs-row parity. proposed -> in_progress ->
  // implemented; abandoning stays a detail-modal decision.
  const FLOW: UseCaseStatus[] = ['open', 'approved', 'in_progress', 'completed'];
  function nextOf(s: UseCaseStatus): UseCaseStatus | null {
    const i = FLOW.indexOf(s);
    return i < 0 || i === FLOW.length - 1 ? null : FLOW[i + 1];
  }
  async function advance(u: UseCaseItem) {
    const next = nextOf(u.status);
    if (!next) return;
    try {
      await api.updateUseCase(u.id, { status: next });
      onChange();
      if (showDone) await refreshDone();
    } catch (e) {
      void alertDialog(`Status update failed: ${e}`);
    }
  }

  let pendingDelete = $state<UseCaseItem | null>(null);
  let showDone = $state(false);
  let doneItems = $state<UseCaseItem[]>([]);
  let doneLoading = $state(false);

  async function refreshDone() {
    if (!loadDone) return;
    doneLoading = true;
    try {
      const all = await loadDone();
      doneItems = all.filter((u) => isUseCaseDone(u.status));
    } catch (e) {
      console.warn('load done use cases:', e);
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

  async function doDelete() {
    const u = pendingDelete;
    pendingDelete = null;
    if (!u) return;
    try {
      await api.deleteUseCase(u.id);
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
    title={showDone ? 'Hide done use cases' : 'Show done use cases'}
  >
    {showDone ? '− Hide done' : '+ Show done'}
    {#if showDone && !doneLoading} ({doneItems.length}){/if}
  </button>
</div>

<ul>
  {#each useCases as u (u.id)}
    <li>
      <span class="status status-{u.status}" title={u.status}>{STATUS_GLYPH[u.status] ?? u.status[0].toUpperCase()}</span>
      <span class="num" title="Use case ID">UC-{u.number}</span>
      <button
        class="subject"
        title="Open details"
        onclick={() => onOpenDetail(u)}
      >{u.source_name || u.subject}</button>
      {#if flags?.has(u.id)}
        <span class="rq-mark" class:unanchored={flags.get(u.id)}
          title={flags.get(u.id) ? 'Flagged for review — no code location was recorded' : 'Flagged for review'}>⚠</span>
      {/if}
      {#if isUseCaseInProgress(u.status)}
        <InProgressBadge claimedBy={u.claimed_by} claimedAt={u.claimed_at} />
      {/if}
      <SyncIndicator syncStatus={u.sync_status} lastSyncAt={u.last_sync_at} lastSyncError={u.last_sync_error} />
      {#if u.target_release}
        <span class="release" title="Target release">{u.target_release}</span>
      {/if}
      {#if u.origin === 'agent-derived'}
        <span class="origin" title="created by agent">★</span>
      {/if}
      <button
        class="status-btn"
        onclick={() => advance(u)}
        disabled={!nextOf(u.status)}
        title={nextOf(u.status)
          ? `Advance to ${nextOf(u.status)?.replace('_', ' ')}`
          : u.status.replace('_', ' ')}
      >{u.status.replace('_', ' ')}</button>
      <button class="del" onclick={() => (pendingDelete = u)} title="Delete">×</button>
    </li>
  {/each}
  {#if useCases.length === 0}
    <li class="empty">No use cases yet.</li>
  {/if}

  {#if showDone && doneItems.length > 0}
    <li class="divider" aria-hidden="true">
      <span>── Done ──</span>
    </li>
    {#each doneItems as u (u.id)}
      <li class:done={u.status === 'completed'} class:abandoned={u.status === 'rejected'}>
        <span class="status status-{u.status}" title={u.status}>{STATUS_GLYPH[u.status] ?? u.status[0].toUpperCase()}</span>
        <span class="num" title="Use case ID">UC-{u.number}</span>
        <button
          class="subject"
          title="Open details"
          onclick={() => onOpenDetail(u)}
        >{u.source_name || u.subject}</button>
        {#if flags?.has(u.id)}
          <span class="rq-mark" class:unanchored={flags.get(u.id)}
            title={flags.get(u.id) ? 'Flagged for review — no code location was recorded' : 'Flagged for review'}>⚠</span>
        {/if}
        <span class="terminal">{useCaseTerminalLabel(u.status)}</span>
        <SyncIndicator syncStatus={u.sync_status} lastSyncAt={u.last_sync_at} lastSyncError={u.last_sync_error} />
        {#if u.claimed_by}
          <span class="claimer" title={`Last claimed by ${u.claimed_by}`}>by {u.claimed_by}</span>
        {/if}
        <button class="del" onclick={() => (pendingDelete = u)} title="Delete">×</button>
      </li>
    {/each}
  {/if}
</ul>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete use case"
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
  .rq-mark { color: var(--p-d4a54d); font-size: 12px; flex: none; cursor: default; }
  .rq-mark.unanchored { color: var(--p-e07a7a); }
  li.done .subject { color: var(--p-99cc99); }
  li.abandoned .subject {
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
  .status {
    display: inline-block;
    width: 16px;
    height: 16px;
    line-height: 16px;
    text-align: center;
    border-radius: 3px;
    font-size: 9px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .status-open { background: var(--p-2d3a52); color: var(--p-99ccff); }
  .status-approved { background: var(--p-10202a); color: var(--p-66ccff); }
  .status-in_progress { background: var(--p-4a3a10); color: var(--p-ffeedd); }
  .status-completed { background: var(--p-2a4a2a); color: var(--p-99cc99); }
  .status-rejected { background: var(--p-3a2a2a); color: var(--p-cc8888); }
  .num {
    font-size: 10px;
    padding: 1px 5px;
    background: var(--p-1e3040);
    color: var(--p-99ccff);
    border-radius: 2px;
    font-family: ui-monospace, monospace;
    flex-shrink: 0;
  }
  .release {
    font-size: 10px;
    padding: 1px 5px;
    background: var(--p-2a2a2a);
    color: var(--p-aaaaaa);
    border-radius: 2px;
    font-family: ui-monospace, monospace;
  }
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
  .status-btn {
    font-size: 10px;
    padding: 2px 8px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
</style>
