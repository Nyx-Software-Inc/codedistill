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
  import type { KnowledgeEntry } from '../lib/types';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import { deleteSyncWarning } from '../lib/sync';

  // A KB row now opens the full detail modal (Provenance/Log/Tags parity with
  // todo/bug/use-case) via onOpenDetail — App owns the modal, same as the
  // other panes.
  type Props = {
    kb: KnowledgeEntry[];
    onChange: () => void;
    onOpenDetail: (k: KnowledgeEntry) => void;
    loadDone?: () => Promise<KnowledgeEntry[]>;
  };
  let { kb, onChange, onOpenDetail, loadDone }: Props = $props();

  let pendingDelete = $state<KnowledgeEntry | null>(null);
  let showDone = $state(false);
  let doneItems = $state<KnowledgeEntry[]>([]);
  let doneLoading = $state(false);

  async function refreshDone() {
    if (!loadDone) return;
    doneLoading = true;
    try {
      const all = await loadDone();
      doneItems = all.filter((k) => k.status === 'deprecated');
    } catch (e) {
      console.warn('load deprecated KB:', e);
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

  function requestDelete(k: KnowledgeEntry, e: MouseEvent | KeyboardEvent) {
    e.stopPropagation();
    pendingDelete = k;
  }

  async function doDelete() {
    const k = pendingDelete;
    pendingDelete = null;
    if (!k) return;
    try {
      await api.deleteKB(k.id);
      onChange();
      if (showDone) await refreshDone();
    } catch (err) {
      void alertDialog(`Delete failed: ${err}`);
    }
  }
</script>

<div class="header">
  <button
    type="button"
    class="show-done"
    onclick={toggleShowDone}
    title={showDone ? 'Hide deprecated KB' : 'Show deprecated KB'}
  >
    {showDone ? '− Hide deprecated' : '+ Show deprecated'}
    {#if showDone && !doneLoading} ({doneItems.length}){/if}
  </button>
</div>

<ul>
  {#each kb as k (k.id)}
    <li id="kb-row-{k.id}">
      <button class="row" onclick={() => onOpenDetail(k)}>
        <span class="title">{k.source_name || k.title}</span>
        {#if k.kind && k.kind !== 'reference'}
          <span class="kind-badge {k.kind}" title="Project brain: {k.kind}">{k.kind}</span>
        {/if}
        {#if k.source_item_id}
          <span class="origin" title="from scratchpad">★</span>
        {/if}
        <SyncIndicator syncStatus={k.sync_status} lastSyncAt={k.last_sync_at} lastSyncError={k.last_sync_error} />
        <span
          class="del"
          role="button"
          tabindex="0"
          onclick={(e) => requestDelete(k, e)}
          onkeydown={(e) => { if (e.key === 'Enter') requestDelete(k, e); }}
          title="Delete"
        >×</span>
      </button>
    </li>
  {/each}
  {#if kb.length === 0}
    <li class="empty">No KB entries yet.</li>
  {/if}

  {#if showDone && doneItems.length > 0}
    <li class="divider" aria-hidden="true">
      <span>── Deprecated ──</span>
    </li>
    {#each doneItems as k (k.id)}
      <li id="kb-row-{k.id}" class="done">
        <button class="row" onclick={() => onOpenDetail(k)}>
          <span class="title">{k.source_name || k.title}</span>
          <span class="terminal">Deprecated</span>
          <SyncIndicator syncStatus={k.sync_status} lastSyncAt={k.last_sync_at} lastSyncError={k.last_sync_error} />
          <span
            class="del"
            role="button"
            tabindex="0"
            onclick={(e) => requestDelete(k, e)}
            onkeydown={(e) => { if (e.key === 'Enter') requestDelete(k, e); }}
            title="Delete"
          >×</span>
        </button>
      </li>
    {/each}
  {/if}
</ul>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete KB entry"
  message={pendingDelete
    ? `Delete “${pendingDelete.title}”? This cannot be undone.${deleteSyncWarning(pendingDelete)}`
    : ''}
  onConfirm={doDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  li {
    border-bottom: 1px solid var(--p-2a2a2a);
    font-size: 12px;
  }
  .row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 6px;
    background: transparent;
    border: none;
    color: inherit;
    font-size: 12px;
    text-align: left;
    cursor: pointer;
    border-radius: 0;
  }
  .row:hover { background: var(--p-252525); }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .origin { color: var(--p-99ccff); font-size: 11px; }
  .kind-badge {
    flex: none;
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 0 4px;
    color: var(--p-aaaaaa);
  }
  .kind-badge.architecture { color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .kind-badge.convention { color: var(--p-66cc66); }
  .kind-badge.decision { color: var(--p-d4a54d); }
  .del {
    color: var(--p-666666);
    font-size: 14px;
    padding: 0 4px;
    cursor: pointer;
    line-height: 1;
  }
  .del:hover { color: var(--p-ffffff); }
  .empty {
    text-align: center;
    color: var(--p-666666);
    padding: 20px;
    border-bottom: none;
    display: flex;
    justify-content: center;
  }

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
  li.done {
    opacity: 0.6;
  }
  li.divider {
    text-align: center;
    color: var(--p-666666);
    border-bottom: none;
    padding: 12px 0 4px;
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
</style>
