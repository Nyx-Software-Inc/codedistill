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
  // The review on the changeset showing in the Changes pane (not the item
  // sign-off bar, which is ReviewBar.svelte): none yet
  // ("Request review"), or its status, approvals so far against the number
  // required, each decision with its note, and the actions the acting user
  // may take. Merged is recorded, not performed.
  import * as api from '../lib/api';
  import type { ReviewView } from '../lib/api';
  import { promptDialog } from '../lib/dialog';

  type Props = {
    projectId: string;
    changesetId: string | null;
    /** Bumped by the host when something elsewhere may have changed the review. */
    tick?: number;
    /** After any change here, so the host (and Needs review) refresh. */
    onChanged?: () => void;
  };
  let { projectId, changesetId, tick = 0, onChanged }: Props = $props();

  let view = $state<ReviewView | null>(null);
  let loading = $state(false);
  let busy = $state(false);
  let err = $state('');
  let loadedKey = '';
  let seq = 0;

  $effect(() => {
    const key = `${projectId}|${changesetId}|${tick}`;
    if (key === loadedKey) return;
    loadedKey = key;
    void load();
  });

  async function load() {
    const my = ++seq;
    err = '';
    if (!projectId || !changesetId) { view = null; return; }
    loading = true;
    try {
      const v = await api.getChangesetReview(projectId, changesetId);
      if (my !== seq) return;
      view = v;
    } catch (e) {
      if (my !== seq) return;
      // 404 is the normal "no review requested" state.
      view = null;
      if (!/: 404\)$/.test(String(e))) err = String(e);
    } finally {
      if (my === seq) loading = false;
    }
  }

  async function run(f: () => Promise<ReviewView>) {
    if (busy) return;
    busy = true;
    err = '';
    try {
      view = await f();
      onChanged?.();
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }
  function request() {
    if (!changesetId) return;
    void run(() => api.openReview(projectId, changesetId));
  }
  function approve() {
    if (!view) return;
    void run(() => api.decideReview(projectId, view!.review.id, 'approved'));
  }
  async function requestChanges() {
    if (!view) return;
    const note = await promptDialog('What needs to change? (optional — lands on the review)', {
      title: 'Request changes', confirmLabel: 'Request changes', danger: true,
    });
    if (note === null) return;
    void run(() => api.decideReview(projectId, view!.review.id, 'rejected', note));
  }
  function setStatus(st: 'merged' | 'abandoned' | 'open') {
    if (!view) return;
    void run(() => api.setReviewStatus(projectId, view!.review.id, st));
  }
  function when(iso: string): string {
    const d = new Date(iso);
    const m = Math.round((Date.now() - d.getTime()) / 60000);
    if (m < 1) return 'just now';
    if (m < 60) return `${m}m ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h}h ago`;
    return d.toLocaleDateString();
  }
  const STATUS_LABEL: Record<string, string> = {
    open: 'Open', changes_requested: 'Changes requested', approved: 'Approved', merged: 'Merged', abandoned: 'Abandoned',
  };
  const live = $derived(!!view && (view.review.status === 'open' || view.review.status === 'changes_requested' || view.review.status === 'approved'));
</script>

{#if changesetId}
  <div class="rb" class:has={!!view}>
    {#if err}<span class="err">{err}</span>{/if}
    {#if !view}
      <span class="muted">{loading ? 'Loading review…' : 'No review requested.'}</span>
      <button type="button" class="btn primary" disabled={busy || loading} onclick={request} title="Open a review on this changeset: it lands in Needs review and, under the review policy, gates completion of the items it covers">Request review</button>
    {:else}
      <span class="chip {view.review.status}">{STATUS_LABEL[view.review.status] ?? view.review.status}</span>
      <span class="appr" title="Approvals counted toward the required number">{view.approvals}/{view.review.required_approvals} approvals</span>
      {#if view.open_comments > 0}<span class="muted">💬 {view.open_comments} open</span>{/if}
      {#if view.is_author}<span class="muted" title="Your approval does not count in a multi-user workspace">you are the author</span>{/if}
      <span class="actions">
        {#if live}
          <button type="button" class="btn ok" disabled={busy || view.mine === 'approved'} onclick={approve}>✓ Approve</button>
          <button type="button" class="btn danger" disabled={busy || view.mine === 'rejected'} onclick={() => void requestChanges()}>✗ Request changes</button>
          {#if view.review.status === 'approved'}
            <button type="button" class="btn" disabled={busy} onclick={() => setStatus('merged')} title="Record that this changeset was merged (CodeDistill does not run git merge)">Mark merged</button>
          {/if}
          <button type="button" class="btn quiet" disabled={busy} onclick={() => setStatus('abandoned')}>Abandon</button>
        {:else if view.review.status === 'abandoned'}
          <button type="button" class="btn" disabled={busy} onclick={() => setStatus('open')}>Reopen</button>
        {/if}
      </span>
    {/if}
  </div>
  {#if view && view.decisions.length > 0}
    <div class="decisions">
      {#each view.decisions as d (d.id)}
        <div class="dec" class:uncounted={!d.counts}>
          <span class="who">{d.reviewer_name ?? d.reviewer}</span>
          <span class="verdict {d.decision}">{d.decision === 'approved' ? 'approved' : 'requested changes'}</span>
          {#if d.superseded}<span class="muted">(superseded)</span>{:else if !d.counts}<span class="muted">(author, not counted)</span>{/if}
          <span class="muted">{when(d.created_at)}</span>
          {#if d.note}<span class="note">— {d.note}</span>{/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}

<style>
  .rb {
    flex-shrink: 0;
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 4px 8px;
    background: var(--p-131313);
    border-bottom: 1px solid var(--p-262626);
    font-size: 11px;
    color: var(--p-aaaaaa);
  }
  .muted { color: var(--p-777777); }
  .err { color: var(--p-ff8888); }
  .chip { font-size: 10px; padding: 1px 8px; border-radius: 9px; background: var(--p-262626); color: var(--p-dddddd); text-transform: uppercase; letter-spacing: 0.4px; }
  .chip.approved, .chip.merged { color: var(--diff-add-fg); background: var(--diff-add-gutter); }
  .chip.changes_requested { color: var(--diff-del-fg); background: var(--diff-del-gutter); }
  .chip.open { color: var(--p-99ccff); background: var(--p-1e3a52); }
  .chip.abandoned { color: var(--p-888888); }
  .appr { color: var(--p-dddddd); }
  .actions { margin-left: auto; display: inline-flex; gap: 6px; }
  .btn {
    font-size: 11px; padding: 3px 10px; border-radius: 4px; cursor: pointer;
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-aaaaaa);
  }
  .btn:hover { color: var(--p-ffffff); border-color: var(--p-555555); }
  .btn.primary { background: var(--p-2d5578); border-color: var(--p-2d5578); color: var(--p-cceeff); }
  .btn.ok { color: var(--diff-add-fg); border-color: var(--diff-add-fg); }
  .btn.danger { color: var(--diff-del-fg); border-color: var(--diff-del-fg); }
  .btn.quiet { border-color: transparent; }
  .btn:disabled { opacity: 0.45; cursor: default; }
  .decisions {
    flex-shrink: 0;
    display: flex; flex-direction: column; gap: 2px;
    padding: 3px 10px 5px;
    background: var(--p-131313);
    border-bottom: 1px solid var(--p-262626);
    font-size: 11px;
  }
  .dec { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; }
  .dec.uncounted { opacity: 0.6; }
  .who { font-weight: 600; color: var(--p-dddddd); }
  .verdict.approved { color: var(--diff-add-fg); }
  .verdict.rejected { color: var(--diff-del-fg); }
  .note { color: var(--p-aaaaaa); }
</style>
