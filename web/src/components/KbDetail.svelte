<!-- =============================================================================
  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.

  CodeDistill

  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
  Public License v3.0 (see the LICENSE file) and, separately, a commercial
  license available from Nyx Software, Inc. Use outside the terms of one of those
  licenses is prohibited.

  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
============================================================================= -->

<!--
  KB detail modal — brings knowledge entries to parity with todo/bug/use-case
  (they used to be an inline-expand list with no Provenance, Log, or Tags).
  Tabs: KB (editable form incl. Tags) | Provenance (Throughline + read-only
  source) | Log (activity timeline). Same three-tab spine as the others, minus
  the work-verification tabs (Acceptance/Verification) that don't apply to KB.
-->
<script lang="ts">
  import Modal from './Modal.svelte';
  import * as api from '../lib/api';
  import CodeAnchorChips from './CodeAnchorChips.svelte';
  import SourceItemFields from './SourceItemFields.svelte';
  import Throughline from './Throughline.svelte';
  import ActivityLog from './ActivityLog.svelte';
  import ChangeTypeMenu from './ChangeTypeMenu.svelte';
  import TagEditor from './TagEditor.svelte';
  import SyncIndicator from './SyncIndicator.svelte';
  import type { KnowledgeEntry, KbKind, KnowledgeStatus } from '../lib/types';

  type Props = {
    kb: KnowledgeEntry | null;
    onClose: () => void;
    onSaved: () => void;
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { kb, onClose, onSaved, onOpenFile }: Props = $props();

  // Snapshot on id-change so a background refresh can't clobber edits.
  let draft = $state<KnowledgeEntry | null>(null);
  let snapId = $state<string | null>(null);
  $effect(() => {
    if (kb?.id !== snapId) {
      draft = kb ? { ...kb, tags: kb.tags ?? [] } : null;
      snapId = kb?.id ?? null;
      saveErr = '';
    }
  });

  let saving = $state(false);
  let saveErr = $state('');

  let activeTab = $state<'kb' | 'throughline' | 'log'>('kb');
  $effect(() => {
    void kb?.id;
    activeTab = 'kb';
  });

  const KINDS: KbKind[] = ['reference', 'architecture', 'convention', 'decision'];

  async function save() {
    if (!draft || !kb || saving) return;
    saving = true;
    saveErr = '';
    try {
      const updated = await api.updateKB(kb.id, {
        title: draft.title,
        content: draft.content,
        kind: draft.kind,
        status: draft.status,
        tags: draft.tags,
      });
      draft = { ...updated, tags: updated.tags ?? [] };
      onSaved();
      onClose();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  async function toggleDeprecated() {
    if (!draft || !kb || saving) return;
    saving = true;
    saveErr = '';
    try {
      if (draft.status === 'deprecated') {
        const updated = await api.reopenKB(kb.id);
        draft = { ...updated, tags: updated.tags ?? [] };
      } else {
        const updated = await api.updateKB(kb.id, { status: 'deprecated' as KnowledgeStatus });
        draft = { ...updated, tags: updated.tags ?? [] };
      }
      onSaved();
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

<Modal open={kb !== null} title={kb ? `KB-${kb.number} · ${kb.title}` : 'Knowledge'} onClose={cancel}>
  {#if draft}
    <div class="modal-tabs">
      <button class="modal-tab" class:active={activeTab === 'kb'} onclick={() => (activeTab = 'kb')}>KB</button>
      <button class="modal-tab" class:active={activeTab === 'throughline'} onclick={() => (activeTab = 'throughline')}>Provenance</button>
      <button class="modal-tab" class:active={activeTab === 'log'} onclick={() => (activeTab = 'log')}>Log</button>
    </div>

    {#if activeTab === 'kb'}
    <section class="form">
      <label class="field">
        <span>Title</span>
        <input type="text" bind:value={draft.title} />
      </label>

      <div class="field">
        <span>Tags</span>
        <TagEditor bind:tags={draft.tags} onChange={() => {}} />
      </div>

      <label class="field">
        <span>Content</span>
        <textarea bind:value={draft.content} rows="12"></textarea>
      </label>

      <div class="row2">
        <label class="field">
          <span>Kind (project brain)</span>
          <select bind:value={draft.kind}>
            {#each KINDS as k}
              <option value={k}>{k}</option>
            {/each}
          </select>
        </label>
        <div class="field">
          <span>Status</span>
          <div class="status-row">
            <span class="status-val">{draft.status}</span>
            <button type="button" class="action" onclick={toggleDeprecated} disabled={saving}>
              {draft.status === 'deprecated' ? '↺ Reactivate' : 'Deprecate'}
            </button>
          </div>
        </div>
      </div>

      <div class="meta-grid">
        <div><span class="k">Created</span><span class="v">{fmtDate(draft.created_at)}</span></div>
        <div><span class="k">ID</span><span class="v mono">{draft.id}</span></div>
      </div>

      <CodeAnchorChips ownerType="knowledge_entry" ownerId={draft.id} {onOpenFile} />

      <div class="lifecycle">
        <SyncIndicator syncStatus={draft.sync_status} lastSyncAt={draft.last_sync_at} lastSyncError={draft.last_sync_error} />
      </div>

      {#if saveErr}
        <div class="err">{saveErr}</div>
      {/if}
    </section>
    {:else if activeTab === 'throughline'}
    <section class="lineage">
      <Throughline ownerType="knowledge_entry" ownerId={draft.id} intentTitle={draft.title} {onOpenFile} />
      <hr class="prov-div" />
      <SourceItemFields itemId={draft.source_item_id} {onSaved} {onOpenFile} />
    </section>
    {:else if activeTab === 'log'}
    <section class="lineage">
      <ActivityLog ownerType="knowledge_entry" ownerId={draft.id} sourceItemId={draft.source_item_id} />
    </section>
    {/if}
  {/if}

  {#snippet footer()}
    <span style="margin-right:auto"><ChangeTypeMenu sourceItemId={draft?.source_item_id} currentType="kb" onDone={() => { onSaved(); onClose(); }} /></span>
    <button onclick={cancel} disabled={saving}>Cancel</button>
    <button class="primary" onclick={save} disabled={saving || !draft?.title?.trim()}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .lifecycle {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    flex-wrap: wrap;
  }
  .action {
    background: var(--p-2a2a2a);
    border: 1px solid var(--p-444444);
    color: var(--p-dddddd);
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .action:hover { background: var(--p-3a3a3a); }
  .action:disabled { opacity: 0.5; cursor: default; }
  .status-row { display: flex; align-items: center; gap: 10px; }
  .status-val { font-size: 13px; color: var(--p-eeeeee); text-transform: capitalize; }

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
  .field textarea,
  .field select {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: inherit;
  }
  .field textarea { resize: vertical; min-height: 120px; font-family: ui-monospace, monospace; font-size: 12px; }
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
    grid-template-columns: 100px 1fr;
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
  .lineage { min-height: 120px; }
  .err { color: var(--p-ff8888); font-size: 12px; margin-top: 6px; }
  .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
</style>
