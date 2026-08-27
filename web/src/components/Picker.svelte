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
  import ConfirmDialog from './ConfirmDialog.svelte';

  // Generic picker dropdown for header use. Trigger button shows the
  // currently-selected item's name + caret; clicking opens a panel with
  // every item, inline rename + delete actions on hover, and a "+ New"
  // affordance at the bottom. Used for both the project and scratchpad
  // pickers in App's header.

  type Item = { id: string; name: string };

  type Props = {
    items: Item[];
    activeId: string;
    // Singular noun: "project" / "scratchpad". Used in placeholders, dialog
    // copy, and tooltips.
    label: string;
    onSelect: (id: string) => void | Promise<void>;
    onCreate: (name: string) => Promise<void>;
    onRename: (id: string, name: string) => Promise<void>;
    onDelete: (id: string) => Promise<void>;
    // Returns false for items that should not show the trash icon (e.g.,
    // the last surviving scratchpad in a project). Defaults to all-allowed.
    canDelete?: (id: string) => boolean;
    // Shown in the trigger when no item is active.
    emptyLabel?: string;
  };
  let {
    items,
    activeId,
    label,
    onSelect,
    onCreate,
    onRename,
    onDelete,
    canDelete = () => true,
    emptyLabel = '—',
  }: Props = $props();

  let open = $state(false);
  let panel = $state<HTMLDivElement | undefined>(undefined);
  let trigger = $state<HTMLButtonElement | undefined>(undefined);

  let renamingId = $state<string | null>(null);
  let renameDraft = $state('');
  let creating = $state(false);
  let createDraft = $state('');
  let pendingDelete = $state<Item | null>(null);
  let busy = $state(false);
  let err = $state('');

  let activeItem = $derived(items.find((i) => i.id === activeId) ?? null);

  function toggle() {
    open = !open;
    if (open) {
      err = '';
      renamingId = null;
      creating = false;
      createDraft = '';
    }
  }

  function close() {
    open = false;
    renamingId = null;
    creating = false;
  }

  function onDocClick(e: MouseEvent) {
    if (!open) return;
    // A delete-confirm modal is up — it renders outside the panel, so its own
    // backdrop/cancel owns this click. Closing the panel here would hide a
    // failed delete's error, which only renders inside the panel (audit M21).
    if (pendingDelete) return;
    const t = e.target as Node;
    if (panel?.contains(t) || trigger?.contains(t)) return;
    close();
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) close();
  }
  onMount(() => {
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
  });
  onDestroy(() => {
    document.removeEventListener('mousedown', onDocClick);
    document.removeEventListener('keydown', onKey);
  });

  async function selectItem(id: string) {
    if (id !== activeId) await onSelect(id);
    close();
  }

  function startRename(it: Item) {
    renamingId = it.id;
    renameDraft = it.name;
    err = '';
  }
  async function commitRename() {
    if (!renamingId || busy) return;
    const next = renameDraft.trim();
    const cur = items.find((i) => i.id === renamingId);
    if (!next || !cur || next === cur.name) {
      renamingId = null;
      return;
    }
    busy = true;
    err = '';
    try {
      await onRename(renamingId, next);
      renamingId = null;
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }
  function cancelRename() {
    renamingId = null;
    err = '';
  }

  function startCreate() {
    creating = true;
    createDraft = '';
    err = '';
  }
  async function commitCreate() {
    if (busy) return;
    const next = createDraft.trim();
    if (!next) {
      creating = false;
      return;
    }
    busy = true;
    err = '';
    try {
      await onCreate(next);
      creating = false;
      createDraft = '';
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }
  function cancelCreate() {
    creating = false;
    err = '';
  }

  async function confirmDelete() {
    const it = pendingDelete;
    pendingDelete = null;
    if (!it) return;
    busy = true;
    err = '';
    try {
      await onDelete(it.id);
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="picker">
  <button
    type="button"
    class="trigger"
    bind:this={trigger}
    onclick={toggle}
    aria-haspopup="listbox"
    aria-expanded={open}
    title="Switch {label}"
  >
    <span class="name">{activeItem?.name ?? emptyLabel}</span>
    <span class="caret" aria-hidden="true">▾</span>
  </button>

  {#if open}
    <div class="panel" bind:this={panel} role="listbox">
      {#if err}
        <div class="err">{err}</div>
      {/if}
      <ul>
        {#each items as it (it.id)}
          <li class:active={it.id === activeId}>
            {#if renamingId === it.id}
              <!-- svelte-ignore a11y_autofocus -->
              <input
                type="text"
                class="rename-input"
                bind:value={renameDraft}
                onkeydown={(e) => {
                  if (e.key === 'Enter') commitRename();
                  else if (e.key === 'Escape') cancelRename();
                }}
                onblur={commitRename}
                disabled={busy}
                autofocus
              />
            {:else}
              <button
                type="button"
                class="row"
                onclick={() => selectItem(it.id)}
                title={it.name}
              >
                <span class="row-name">{it.name}</span>
              </button>
              <span class="row-actions">
                <button
                  type="button"
                  class="action"
                  title="Rename"
                  aria-label="Rename {it.name}"
                  onclick={(e) => { e.stopPropagation(); startRename(it); }}
                >✎</button>
                {#if canDelete(it.id)}
                  <button
                    type="button"
                    class="action danger"
                    title="Delete"
                    aria-label="Delete {it.name}"
                    onclick={(e) => { e.stopPropagation(); pendingDelete = it; }}
                  >×</button>
                {/if}
              </span>
            {/if}
          </li>
        {/each}
        {#if items.length === 0 && !creating}
          <li class="empty">No {label}s yet.</li>
        {/if}
      </ul>

      <div class="footer">
        {#if creating}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            type="text"
            class="create-input"
            placeholder="New {label} name"
            bind:value={createDraft}
            onkeydown={(e) => {
              if (e.key === 'Enter') commitCreate();
              else if (e.key === 'Escape') cancelCreate();
            }}
            disabled={busy}
            autofocus
          />
          <button type="button" class="ok" onclick={commitCreate} disabled={busy || !createDraft.trim()}>
            {busy ? '…' : 'Add'}
          </button>
          <button type="button" class="ghost" onclick={cancelCreate} disabled={busy}>Cancel</button>
        {:else}
          <button type="button" class="add" onclick={startCreate}>+ New {label}</button>
        {/if}
      </div>
    </div>
  {/if}
</div>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete {label}"
  message={pendingDelete ? `Delete “${pendingDelete.name}”? This cannot be undone.` : ''}
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .picker {
    position: relative;
    display: inline-flex;
  }
  .trigger {
    background: transparent;
    border: 1px solid var(--p-2a2a2a);
    color: var(--p-dddddd);
    font-size: 13px;
    padding: 5px 10px;
    cursor: pointer;
    border-radius: 3px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 240px;
  }
  .trigger:hover { background: var(--p-1a1a1a); border-color: var(--p-444444); }
  .trigger[aria-expanded="true"] {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .trigger .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .trigger .caret {
    color: var(--p-888888);
    font-size: 10px;
  }
  .panel {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    min-width: 240px;
    max-width: 360px;
    background: var(--p-161616);
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.6);
    z-index: 100;
    padding: 4px 0;
  }
  .err {
    color: var(--p-ff8888);
    font-size: 11px;
    padding: 4px 10px 6px;
    background: var(--p-2a1414);
    border-bottom: 1px solid var(--p-4a2020);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 300px;
    overflow-y: auto;
  }
  li {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid var(--p-1e1e1e);
  }
  li.empty {
    color: var(--p-666666);
    font-size: 12px;
    font-style: italic;
    padding: 8px 10px;
    justify-content: center;
  }
  li.active .row {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
  }
  .row {
    flex: 1;
    background: transparent;
    border: none;
    color: var(--p-dddddd);
    text-align: left;
    padding: 6px 10px;
    cursor: pointer;
    font-size: 12px;
    overflow: hidden;
    border-radius: 0;
    min-width: 0;
  }
  .row:hover { background: var(--p-1a2a38); color: var(--p-99ccff); }
  .row-name {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-actions {
    display: none;
    align-items: center;
    padding-right: 4px;
  }
  li:hover .row-actions { display: inline-flex; }
  .action {
    background: transparent;
    border: none;
    color: var(--p-888888);
    cursor: pointer;
    font-size: 13px;
    padding: 2px 6px;
    border-radius: 2px;
    line-height: 1;
  }
  .action:hover { color: var(--p-ffffff); background: var(--p-2d5578); }
  .action.danger:hover { color: var(--p-ffffff); background: var(--p-5a1515); }
  .rename-input,
  .create-input {
    flex: 1;
    background: var(--p-0a0a0a);
    color: var(--p-ffffff);
    border: 1px solid var(--p-2d5578);
    padding: 5px 10px;
    font-size: 12px;
    font-family: inherit;
    border-radius: 0;
    min-width: 0;
  }
  .rename-input:focus,
  .create-input:focus {
    outline: none;
    border-color: var(--p-66ccff);
  }
  .footer {
    border-top: 1px solid var(--p-262626);
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px;
  }
  .add {
    flex: 1;
    background: transparent;
    border: 1px dashed var(--p-2d5578);
    color: var(--p-99ccff);
    font-size: 11px;
    padding: 5px 10px;
    cursor: pointer;
    border-radius: 3px;
    text-align: left;
  }
  .add:hover { background: var(--p-1e3a52); color: var(--p-ffffff); border-style: solid; }
  .ok, .ghost {
    border: 1px solid var(--p-2d5578);
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    font-size: 11px;
    padding: 5px 10px;
    cursor: pointer;
    border-radius: 3px;
  }
  .ok:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-ffffff); }
  .ghost {
    background: transparent;
    border-color: var(--p-333333);
    color: var(--p-aaaaaa);
  }
  .ghost:hover:not(:disabled) { background: var(--p-1a1a1a); color: var(--p-dddddd); }
  .ok:disabled, .ghost:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
