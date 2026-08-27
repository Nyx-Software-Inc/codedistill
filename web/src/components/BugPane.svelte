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
  import type { BugItem, BugStatus } from '../lib/types';
  import { isBugDone, isBugInProgress, bugTerminalLabel } from '../lib/lifecycle';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import InProgressBadge from './InProgressBadge.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import { deleteSyncWarning } from '../lib/sync';

  type Props = {
    bugs: BugItem[];
    onChange: () => void;
    onOpenDetail: (bug: BugItem) => void;
    loadDone?: () => Promise<BugItem[]>;
    // flags: item id → unanchored, for items the review queue flagged. Presence
    // means "needs review"; value true means "no code location recorded".
    flags?: Map<string, boolean>;
  };
  let { bugs, onChange, onOpenDetail, loadDone, flags }: Props = $props();

  let pendingDelete = $state<BugItem | null>(null);
  let showDone = $state(false);
  let doneItems = $state<BugItem[]>([]);
  let doneLoading = $state(false);

  const FLOW: BugStatus[] = [
    'open', 'investigating', 'in-progress',
    'fixed', 'verified', 'closed',
  ];

  function nextOf(s: BugStatus): BugStatus | null {
    const i = FLOW.indexOf(s);
    return i < 0 || i === FLOW.length - 1 ? null : FLOW[i + 1];
  }

  async function refreshDone() {
    if (!loadDone) return;
    doneLoading = true;
    try {
      const all = await loadDone();
      doneItems = all.filter((b) => isBugDone(b.status));
    } catch (e) {
      console.warn('load done bugs:', e);
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

  async function advance(b: BugItem) {
    const next = nextOf(b.status);
    if (!next) return;
    try {
      await api.updateBug(b.id, { status: next });
      onChange();
      if (showDone) await refreshDone();
    } catch (e) {
      void alertDialog(`Status update failed: ${e}`);
    }
  }

  async function doDelete() {
    const b = pendingDelete;
    pendingDelete = null;
    if (!b) return;
    try {
      await api.deleteBug(b.id);
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
    title={showDone ? 'Hide done bugs' : 'Show done bugs'}
  >
    {showDone ? '− Hide done' : '+ Show done'}
    {#if showDone && !doneLoading} ({doneItems.length}){/if}
  </button>
</div>

<ul>
  {#each bugs as b (b.id)}
    <li class="status-{b.status}">
      <span class="sev sev-{b.severity}">{b.severity}</span>
      <button
        class="subject"
        title="Open details"
        onclick={() => onOpenDetail(b)}
      >{b.source_name || b.subject}</button>
      {#if flags?.has(b.id)}
        <span class="rq-mark" class:unanchored={flags.get(b.id)}
          title={flags.get(b.id)
            ? 'Flagged for review — no code location was recorded'
            : 'Flagged for review'}>⚠</span>
      {/if}
      {#if isBugInProgress(b.status)}
        <InProgressBadge claimedBy={b.claimed_by} claimedAt={b.claimed_at} />
      {/if}
      <SyncIndicator syncStatus={b.sync_status} lastSyncAt={b.last_sync_at} lastSyncError={b.last_sync_error} />
      {#if b.origin === 'agent-derived'}
        <span class="origin" title="created by agent">★</span>
      {/if}
      <button class="status-btn" onclick={() => advance(b)} disabled={!nextOf(b.status)}>
        {b.status}
      </button>
      <button class="del" onclick={() => (pendingDelete = b)} title="Delete">×</button>
    </li>
  {/each}
  {#if bugs.length === 0}
    <li class="empty">No bugs yet.</li>
  {/if}

  {#if showDone && doneItems.length > 0}
    <li class="divider" aria-hidden="true">
      <span>── Done ──</span>
    </li>
    {#each doneItems as b (b.id)}
      <li class="done status-{b.status}">
        <span class="sev sev-{b.severity}">{b.severity}</span>
        <button
          class="subject"
          title="Open details"
          onclick={() => onOpenDetail(b)}
        >{b.source_name || b.subject}</button>
        {#if flags?.has(b.id)}
          <span class="rq-mark" class:unanchored={flags.get(b.id)}
            title={flags.get(b.id) ? 'Flagged for review — no code location was recorded' : 'Flagged for review'}>⚠</span>
        {/if}
        <span class="terminal">{bugTerminalLabel(b.status)}</span>
        <SyncIndicator syncStatus={b.sync_status} lastSyncAt={b.last_sync_at} lastSyncError={b.last_sync_error} />
        {#if b.claimed_by}
          <span class="claimer" title={`Last claimed by ${b.claimed_by}`}>by {b.claimed_by}</span>
        {/if}
        <button class="del" onclick={() => (pendingDelete = b)} title="Delete">×</button>
      </li>
    {/each}
  {/if}
</ul>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete bug"
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
  .sev {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 2px;
    text-transform: uppercase;
    font-weight: 600;
    letter-spacing: 0.5px;
  }
  .sev-critical { background: var(--p-5a1515); color: var(--p-ffdddd); }
  .sev-major { background: var(--p-5a3a10); color: var(--p-ffeedd); }
  .sev-minor { background: var(--p-333333); color: var(--p-bbbbbb); }
  .sev-trivial { background: var(--p-1e1e1e); color: var(--p-888888); }
  .origin { color: var(--p-99ccff); font-size: 11px; }
  .rq-mark { color: var(--p-d4a54d); font-size: 12px; flex: none; cursor: default; }
  .rq-mark.unanchored { color: var(--p-e07a7a); }
  .status-btn {
    font-size: 10px;
    padding: 2px 8px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
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
