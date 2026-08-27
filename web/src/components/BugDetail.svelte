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
  import type { BugItem, Severity, BugStatus } from '../lib/types';
  import { isBugDone, isBugInProgress } from '../lib/lifecycle';

  type Props = {
    bug: BugItem | null;
    onClose: () => void;
    onSaved: () => void;
    // UC-9: jump-to-code for file anchors (host closes; App opens canvas).
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { bug, onClose, onSaved, onOpenFile }: Props = $props();

  // Bumped after a review sign-off so the Log tab reloads to show it.
  let logTick = $state(0);

  // Snapshot on id-change — same pattern as TodoDetail — so polling can't clobber edits.
  let draft = $state<BugItem | null>(null);
  let snapId = $state<string | null>(null);
  $effect(() => {
    if (bug?.id !== snapId) {
      draft = bug ? { ...bug, tags: bug.tags ?? [] } : null;
      snapId = bug?.id ?? null;
      saveErr = '';
    }
  });

  let saving = $state(false);
  let saveErr = $state('');
  let claiming = $state(false);
  let claimOpen = $state(false);
  let reopening = $state(false);

  async function doClaim(claimer: string) {
    if (!bug || claiming) return;
    claiming = true;
    try {
      const updated = await api.claimBug(bug.id, claimer);
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
    if (!bug || reopening) return;
    reopening = true;
    try {
      const updated = await api.reopenBug(bug.id);
      draft = { ...updated };
      onSaved();
    } catch (e) {
      void alertDialog(`Reopen failed: ${e}`);
    } finally {
      reopening = false;
    }
  }

  // Two-tab modal: "Bug" is the editable form (default), "Source" is
  // the raw classifier lineage. Resets to 'bug' on every id change so
  // the user lands on the editing surface, not whatever tab was last
  // open for a different bug.
  let activeTab = $state<'bug' | 'acceptance' | 'throughline' | 'verification' | 'log'>('bug');
  $effect(() => {
    void bug?.id;
    activeTab = 'bug';
  });


  const SEVERITIES: Severity[] = ['critical', 'major', 'minor', 'trivial'];
  // All statuses available always — the workflow is unrestricted (any
  // status to any status). Ordered: active states, then fix-type
  // terminals, then non-fix terminals.
  const ALL_STATUSES: BugStatus[] = [
    'open', 'investigating', 'in-progress',
    'fixed', 'verified', 'closed',
    'not_a_bug', 'wont_fix', 'duplicate',
  ];
  // If the bug carries a legacy status not in the canonical list,
  // surface it so the user can see + change it; otherwise the dropdown
  // would silently drop the option.
  let allowedStatuses = $derived.by<BugStatus[]>(() => {
    const cur = (bug?.status ?? 'open') as BugStatus;
    if (ALL_STATUSES.includes(cur)) return ALL_STATUSES;
    return [cur, ...ALL_STATUSES];
  });

  async function save() {
    if (!draft || !bug || saving) return;
    saving = true;
    saveErr = '';
    try {
      // Only include status in the PATCH if the user changed it — avoids
      // server-side transition rejection when status is already at its final value.
      const body: Parameters<typeof api.updateBug>[1] = {
        subject: draft.subject,
        severity: draft.severity,
        due_date: draft.due_date ?? '',
        steps_to_reproduce: draft.steps_to_reproduce ?? '',
        expected_behavior: draft.expected_behavior ?? '',
        actual_behavior: draft.actual_behavior ?? '',
        environment: draft.environment ?? '',
        affected_component: draft.affected_component ?? '',
        commit_sha: draft.commit_sha ?? '',
        commit_tag: draft.commit_tag ?? '',
        tags: draft.tags,
      };
      if (draft.status !== bug.status) body.status = draft.status;
      const updated = await api.updateBug(bug.id, body);
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

  function cancel() { onClose(); }

  function fmtDate(s?: string): string {
    if (!s) return '—';
    try { return new Date(s).toLocaleString(); } catch { return s; }
  }

</script>

<Modal open={bug !== null} title={bug ? `BUG-${bug.number} · ${bug.subject}` : 'Bug'} onClose={cancel}>
  {#if draft}
    <div class="modal-tabs">
      <button class="modal-tab" class:active={activeTab === 'bug'} onclick={() => (activeTab = 'bug')}>Bug</button>
      <button class="modal-tab" class:active={activeTab === 'acceptance'} onclick={() => (activeTab = 'acceptance')}>Acceptance</button>
      <button class="modal-tab" class:active={activeTab === 'throughline'} onclick={() => (activeTab = 'throughline')}>Provenance</button>
      <button class="modal-tab" class:active={activeTab === 'verification'} onclick={() => (activeTab = 'verification')}>Verification</button>
      <button class="modal-tab" class:active={activeTab === 'log'} onclick={() => (activeTab = 'log')}>Log</button>
    </div>

    <ReviewBar ownerType="bug_item" ownerId={draft.id} status={draft.status} onDecided={() => { logTick++; onSaved(); }} />

    {#if activeTab === 'bug'}
    <section class="form">
      <label class="field">
        <span>Subject</span>
        <input type="text" bind:value={draft.subject} />
      </label>

      <div class="field">
        <span>Tags</span>
        <TagEditor bind:tags={draft.tags} onChange={() => {}} />
      </div>

      <div class="row2">
        <DueDateField value={draft.due_date} onChange={(v) => { if (draft) draft.due_date = v; }} />
      </div>

      <div class="row2">
        <label class="field">
          <span>Severity</span>
          <select bind:value={draft.severity}>
            {#each SEVERITIES as s}
              <option value={s}>{s}</option>
            {/each}
          </select>
        </label>

        <label class="field">
          <span>Status</span>
          <select bind:value={draft.status}>
            {#each allowedStatuses as s}
              <option value={s}>{s}</option>
            {/each}
          </select>
        </label>
      </div>

      <label class="field">
        <span>Steps to reproduce</span>
        <textarea bind:value={draft.steps_to_reproduce} rows="3"></textarea>
      </label>

      <label class="field">
        <span>Expected behavior</span>
        <textarea bind:value={draft.expected_behavior} rows="2"></textarea>
      </label>

      <label class="field">
        <span>Actual behavior</span>
        <textarea bind:value={draft.actual_behavior} rows="2"></textarea>
      </label>

      <div class="row2">
        <label class="field">
          <span>Environment</span>
          <input type="text" bind:value={draft.environment} />
        </label>
        <label class="field">
          <span>Affected component</span>
          <input type="text" bind:value={draft.affected_component} />
        </label>
      </div>

      <div class="row2">
        <label class="field">
          <span>Commit SHA</span>
          <input type="text" bind:value={draft.commit_sha} placeholder="auto-filled from HEAD on fixed/verified/closed" class="mono-input" />
        </label>
        <label class="field">
          <span>Commit tag</span>
          <input type="text" bind:value={draft.commit_tag} placeholder="optional" />
        </label>
      </div>

      <div class="meta-grid">
        <div><span class="k">Origin</span><span class="v">{draft.origin}</span></div>
        <div><span class="k">Created</span><span class="v">{fmtDate(draft.created_at)}</span></div>
        {#if draft.completed_at}
          <div><span class="k">Completed</span><span class="v">{fmtDate(draft.completed_at)}</span></div>
        {/if}
        <div><span class="k">ID</span><span class="v mono">{draft.id}</span></div>
      </div>

      <CodeAnchorChips ownerType="bug_item" ownerId={draft.id} {onOpenFile} />
      <CustomFields owner="bug_item" id={draft.id} />

      <div class="lifecycle">
        <SyncIndicator syncStatus={draft.sync_status} lastSyncAt={draft.last_sync_at} lastSyncError={draft.last_sync_error} />
        {#if isBugInProgress(draft.status)}
          <InProgressBadge claimedBy={draft.claimed_by} claimedAt={draft.claimed_at} />
        {/if}
        {#if !isBugDone(draft.status) && !isBugInProgress(draft.status)}
          <button type="button" class="action" onclick={() => (claimOpen = true)} disabled={claiming}>
            {claiming ? 'Claiming…' : '◐ Claim'}
          </button>
        {/if}
        {#if isBugDone(draft.status)}
          <button type="button" class="action" onclick={reopen} disabled={reopening}>
            {reopening ? 'Reopening…' : '↺ Reopen'}
          </button>
        {/if}
        {#if draft.claimed_by && isBugDone(draft.status)}
          <span class="claimer-meta">Last claimed by {draft.claimed_by}</span>
        {/if}
      </div>

      {#if saveErr}
        <div class="err">{saveErr}</div>
      {/if}
    </section>
    {:else if activeTab === 'acceptance'}
    <section class="lineage">
      <AcceptanceCriteria ownerType="bug_item" ownerId={draft.id} />
    </section>
    {:else if activeTab === 'throughline'}
    <section class="lineage">
      <Throughline ownerType="bug_item" ownerId={draft.id} intentTitle={draft.subject} {onOpenFile} />
      <hr class="prov-div" />
      <SourceItemFields itemId={draft.source_item_id} {onSaved} {onOpenFile} />
    </section>
    {:else if activeTab === 'verification'}
    <section class="lineage">
      <Verification ownerType="bug_item" ownerId={draft.id} />
    </section>
    {:else}
    <section class="lineage">
      <ActivityLog ownerType="bug_item" ownerId={draft.id} sourceItemId={draft.source_item_id} refreshTick={logTick} />
    </section>
    {/if}
  {/if}

  {#snippet footer()}
    <span style="margin-right:auto"><ChangeTypeMenu sourceItemId={draft?.source_item_id} currentType="bug" onDone={() => { onSaved(); onClose(); }} /></span>
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
  .field textarea,
  .field select {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: inherit;
  }
  .field textarea { resize: vertical; min-height: 50px; font-family: ui-monospace, monospace; }
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
</style>
