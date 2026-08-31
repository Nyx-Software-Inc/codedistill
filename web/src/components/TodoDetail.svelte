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
  import Modal from './Modal.svelte';
  import ReviewBar from './ReviewBar.svelte';
  import * as api from '../lib/api';
  import CodeAnchorChips from './CodeAnchorChips.svelte';
  import CustomFields from './CustomFields.svelte';
  import SourceItemFields from './SourceItemFields.svelte';
  import Throughline from './Throughline.svelte';
  import AcceptanceCriteria from './AcceptanceCriteria.svelte';
  import Verification from './Verification.svelte';
  import ActivityLog from './ActivityLog.svelte';
  import ChangeTypeMenu from './ChangeTypeMenu.svelte';
  import TagEditor from './TagEditor.svelte';
  import DueDateField from './DueDateField.svelte';
  import InProgressBadge from './InProgressBadge.svelte';
  import ClaimDialog from './ClaimDialog.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import type { TodoItem, Priority, TodoStatus } from '../lib/types';
  import { PRIORITIES } from '../lib/types';
  import { isTodoDone, isTodoInProgress } from '../lib/lifecycle';

  type Props = {
    todo: TodoItem | null;
    onClose: () => void;
    onSaved: () => void;
    // UC-9: jump-to-code for file anchors (host closes; App opens canvas).
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { todo, onClose, onSaved, onOpenFile }: Props = $props();

  // Bumped after a review sign-off so the Log tab reloads to show it.
  let logTick = $state(0);

  // Snapshot the todo on id-change so the 3s poll cannot clobber in-progress edits.
  // We compare by id — not by prop identity — because App.svelte reassigns the whole
  // todos array every tick, producing new object references for unchanged rows.
  let draft = $state<TodoItem | null>(null);
  let snapId = $state<string | null>(null);
  $effect(() => {
    if (todo?.id !== snapId) {
      draft = todo ? { ...todo, tags: todo.tags ?? [] } : null;
      snapId = todo?.id ?? null;
      saveErr = '';
    }
  });

  let saving = $state(false);
  let saveErr = $state('');

  // Three-tab modal: "Todo" is the editable form (default), "Source" is
  // the raw classifier lineage, "History" is the event timeline (UC-5).
  // Resets to 'todo' on every id change.
  let activeTab = $state<'todo' | 'acceptance' | 'throughline' | 'verification' | 'log'>('todo');
  $effect(() => {
    void todo?.id;
    activeTab = 'todo';
  });

  const STATUSES: TodoStatus[] = ['incomplete', 'in_progress', 'complete', 'abandoned'];

  let claiming = $state(false);
  let claimOpen = $state(false);
  let reopening = $state(false);

  async function doClaim(claimer: string) {
    if (!todo || claiming) return;
    claiming = true;
    try {
      const updated = await api.claimTodo(todo.id, claimer);
      draft = { ...updated };
      claimOpen = false;
      onSaved();
    } catch (e) {
      void alertDialog(`Claim failed: ${e}`);
    } finally {
      claiming = false;
    }
  }

  async function reopen() {
    if (!todo || reopening) return;
    reopening = true;
    try {
      const updated = await api.reopenTodo(todo.id);
      draft = { ...updated };
      onSaved();
    } catch (e) {
      void alertDialog(`Reopen failed: ${e}`);
    } finally {
      reopening = false;
    }
  }

  async function save() {
    if (!draft || !todo || saving) return;
    saving = true;
    saveErr = '';
    try {
      const updated = await api.updateTodo(todo.id, {
        subject: draft.subject,
        priority: draft.priority,
        status: draft.status,
        commit_sha: draft.commit_sha ?? '',
        commit_tag: draft.commit_tag ?? '',
        due_date: draft.due_date ?? '',
        tags: draft.tags,
      });
      // Reflect server-set fields (auto-filled commit_sha / completed_at)
      // back into draft so the user sees them before close.
      draft = { ...updated };
      onSaved();
      onClose();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  function cancel() {
    onClose();
  }

  function fmtDate(s?: string): string {
    if (!s) return '—';
    try { return new Date(s).toLocaleString(); } catch { return s; }
  }

</script>

<Modal open={todo !== null} title={todo ? `TODO-${todo.number} · ${todo.subject}` : 'Todo'} onClose={cancel}>
  {#if draft}
    <div class="modal-tabs">
      <button class="modal-tab" class:active={activeTab === 'todo'} onclick={() => (activeTab = 'todo')}>Todo</button>
      <button class="modal-tab" class:active={activeTab === 'acceptance'} onclick={() => (activeTab = 'acceptance')}>Acceptance</button>
      <button class="modal-tab" class:active={activeTab === 'throughline'} onclick={() => (activeTab = 'throughline')}>Provenance</button>
      <button class="modal-tab" class:active={activeTab === 'verification'} onclick={() => (activeTab = 'verification')}>Verification</button>
      <button class="modal-tab" class:active={activeTab === 'log'} onclick={() => (activeTab = 'log')}>Log</button>
    </div>

    <ReviewBar ownerType="todo_item" ownerId={draft.id} status={draft.status} onDecided={() => { logTick++; onSaved(); }} />

    {#if activeTab === 'todo'}
    <section class="form">
      <label class="field">
        <span>Subject</span>
        <input type="text" bind:value={draft.subject} />
      </label>

      <div class="row2">
        <label class="field">
          <span>Priority</span>
          <select bind:value={draft.priority}>
            {#each PRIORITIES as p}
              <option value={p}>{p}</option>
            {/each}
          </select>
        </label>

        <label class="field">
          <span>Status</span>
          <select bind:value={draft.status}>
            {#each STATUSES as s}
              <option value={s}>{s}</option>
            {/each}
          </select>
        </label>
      </div>

      <div class="row2">
        <label class="field">
          <span>Commit SHA</span>
          <input type="text" bind:value={draft.commit_sha} placeholder="auto-filled from HEAD on complete" class="mono-input" />
        </label>
        <label class="field">
          <span>Commit tag</span>
          <input type="text" bind:value={draft.commit_tag} placeholder="optional" />
        </label>
      </div>

      <div class="row2">
        <DueDateField value={draft.due_date} onChange={(v) => { if (draft) draft.due_date = v; }} />
      </div>

      <div class="field">
        <span>Tags</span>
        <TagEditor bind:tags={draft.tags} onChange={() => {}} />
      </div>

      <div class="meta-grid">
        <div><span class="k">Origin</span><span class="v">{draft.origin}</span></div>
        <div><span class="k">Created</span><span class="v">{fmtDate(draft.created_at)}</span></div>
        {#if draft.completed_at}
          <div><span class="k">Completed</span><span class="v">{fmtDate(draft.completed_at)}</span></div>
        {/if}
        <div><span class="k">ID</span><span class="v mono">{draft.id}</span></div>
      </div>

      <CodeAnchorChips ownerType="todo_item" ownerId={draft.id} {onOpenFile} />
      <CustomFields owner="todo_item" id={draft.id} />

      <div class="lifecycle">
        <SyncIndicator syncStatus={draft.sync_status} lastSyncAt={draft.last_sync_at} lastSyncError={draft.last_sync_error} />
        {#if isTodoInProgress(draft.status)}
          <InProgressBadge claimedBy={draft.claimed_by} claimedAt={draft.claimed_at} />
        {/if}
        {#if !isTodoDone(draft.status) && !isTodoInProgress(draft.status)}
          <button type="button" class="action" onclick={() => (claimOpen = true)} disabled={claiming}>
            {claiming ? 'Claiming…' : '◐ Claim'}
          </button>
        {/if}
        {#if isTodoDone(draft.status)}
          <button type="button" class="action" onclick={reopen} disabled={reopening}>
            {reopening ? 'Reopening…' : '↺ Reopen'}
          </button>
        {/if}
        {#if draft.claimed_by && isTodoDone(draft.status)}
          <span class="claimer-meta">Last claimed by {draft.claimed_by}</span>
        {/if}
      </div>

      {#if saveErr}
        <div class="err">{saveErr}</div>
      {/if}
    </section>
    {:else if activeTab === 'acceptance'}
    <section class="lineage">
      <AcceptanceCriteria ownerType="todo_item" ownerId={draft.id} />
    </section>
    {:else if activeTab === 'throughline'}
    <section class="lineage">
      <Throughline ownerType="todo_item" ownerId={draft.id} intentTitle={draft.subject} {onOpenFile} />
      <hr class="prov-div" />
      <SourceItemFields itemId={draft.source_item_id} {onSaved} {onOpenFile} />
    </section>
    {:else if activeTab === 'verification'}
    <section class="lineage">
      <Verification ownerType="todo_item" ownerId={draft.id} />
    </section>
    {:else}
    <section class="lineage">
      <ActivityLog ownerType="todo_item" ownerId={draft.id} sourceItemId={draft.source_item_id} refreshTick={logTick} />
    </section>
    {/if}
  {/if}

  {#snippet footer()}
    <span style="margin-right:auto"><ChangeTypeMenu sourceItemId={draft?.source_item_id} currentType="todo" onDone={() => { onSaved(); onClose(); }} /></span>
    <button onclick={cancel} disabled={saving}>Cancel</button>
    <button class="primary" onclick={save} disabled={saving || !draft?.subject?.trim()}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<ClaimDialog open={claimOpen} busy={claiming} onClaim={doClaim} onClose={() => (claimOpen = false)} />

<style>
  .lifecycle {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    flex-wrap: wrap;
  }
  .lifecycle .action {
    background: var(--p-2a2a2a);
    border: 1px solid var(--p-444444);
    color: var(--p-dddddd);
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .lifecycle .action:hover { background: var(--p-3a3a3a); }
  .lifecycle .claimer-meta {
    font-size: 11px;
    color: var(--p-888888);
    font-style: italic;
  }

  .prov-div { border: none; border-top: 1px solid var(--p-262626); margin: 16px 0; }
  .modal-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--p-262626);
    margin: -4px -4px 12px;
  }
  .modal-tab {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--p-888888);
    padding: 6px 14px 5px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    cursor: pointer;
    border-radius: 0;
    margin-bottom: -1px;
  }
  .modal-tab:hover { color: var(--p-cccccc); }
  .modal-tab.active {
    color: var(--p-ffffff);
    border-bottom-color: var(--p-66ccff);
  }
  .form { margin-bottom: 16px; }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
  }
  .field span {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .field input,
  .field select {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: inherit;
  }
  .mono-input { font-family: ui-monospace, monospace; font-size: 12px; }
  .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .meta-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 4px 16px;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--p-262626);
  }
  .meta-grid > div {
    display: grid;
    grid-template-columns: 80px 1fr;
    font-size: 12px;
    padding: 3px 0;
  }
  .k {
    color: var(--p-888888);
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.5px;
    padding-top: 2px;
  }
  .v { color: var(--p-dddddd); font-size: 12px; }
  .mono { font-family: ui-monospace, monospace; font-size: 11px; color: var(--p-aaaaaa); }
  .err { color: var(--p-ff8888); font-size: 12px; margin-top: 6px; }
  .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .primary:hover:not(:disabled) { background: var(--p-2d5578); }
  /* UC-5 lineage timeline */
</style>
