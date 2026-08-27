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
  // Needs Review — the unified attention surface (the resurrected Inbox,
  // renamed to what it is). Two review moments in the loop, one place:
  //   1. Awaiting classification — captures the model has sorted, waiting for
  //      your accept/override (also actionable via the in-card banners).
  //   2. Awaiting sign-off — implemented items the trust dial escalated to a
  //      human; your approve/reject is evidence that feeds project trust.
  // The Dashboard shows the STATS; the work happens here or on the items.
  import * as api from '../lib/api';
  import type { ScratchpadItem } from '../lib/types';
  import { alertDialog, promptDialog } from '../lib/dialog';
  import Modal from './Modal.svelte';

  type Props = {
    open: boolean;
    projectId: string;
    projectName?: string;
    onClose: () => void;
    // Fired after any decision so App refreshes counts + canvas.
    onChanged: () => void;
  };
  let { open, projectId, projectName = '', onClose, onChanged }: Props = $props();

  let inbox = $state<ScratchpadItem[]>([]);
  let queue = $state<api.ReviewQueueResponse | null>(null);
  let loading = $state(false);
  let err = $state('');
  let loadedFor = $state('');

  $effect(() => {
    if (open && projectId && loadedFor !== projectId + ':open') {
      loadedFor = projectId + ':open';
      void load();
    }
    if (!open) loadedFor = '';
  });

  // scratchpad_id → name for the current project, so each pending item shows
  // WHERE it lives (you can't confidently accept what you can't locate). The
  // panel is project-scoped: inbox items outside this project are filtered out.
  let padNames = $state<Record<string, string>>({});
  async function load() {
    loading = true;
    err = '';
    try {
      const [ib, q, pads] = await Promise.all([
        api.listInbox(),
        api.getReviewQueue(projectId),
        api.listScratchpads(projectId),
      ]);
      const names: Record<string, string> = {};
      for (const p of pads) names[p.id] = p.name;
      padNames = names;
      // Only this project's pending captures.
      inbox = ib.filter((it) => names[it.scratchpad_id] !== undefined);
      queue = q;
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  // Per-item busy state — a Set so one item processing doesn't disable the
  // others (Rich's find: accepting item A blocked B and C). Concurrent
  // accepts are fine.
  let busyItems = $state<Set<string>>(new Set());
  let busyLabels = $state<Record<string, string>>({});
  const isBusy = (id: string, label?: string) =>
    busyItems.has(id) && (label === undefined || busyLabels[id] === label);
  async function act(itemID: string, label: string, fn: () => Promise<unknown>) {
    if (busyItems.has(itemID)) return;
    busyItems = new Set(busyItems).add(itemID);
    busyLabels = { ...busyLabels, [itemID]: label };
    try {
      await fn();
      await load();
      onChanged();
    } catch (e) {
      void alertDialog(`Action failed: ${e}`);
    } finally {
      const next = new Set(busyItems);
      next.delete(itemID);
      busyItems = next;
      const { [itemID]: _drop, ...rest } = busyLabels;
      busyLabels = rest;
    }
  }

  // Per-item, keyed by owner_type+id: reviewing one item mustn't disable the
  // rest (same fix as the classification section above).
  let reviewingItems = $state<Set<string>>(new Set());
  const rkey = (it: api.ReviewQueueEntry) => it.owner_type + ':' + it.id;
  const isReviewing = (it: api.ReviewQueueEntry) => reviewingItems.has(rkey(it));
  async function review(it: api.ReviewQueueEntry, decision: 'approved' | 'rejected') {
    const k = rkey(it);
    if (reviewingItems.has(k)) return;
    let note = '';
    if (decision === 'rejected') {
      const n = await promptDialog('Why does this need rework? (optional — lands on the item and in trust evidence)', {
        title: 'Needs rework',
        confirmLabel: 'Reject',
        danger: true,
      });
      if (n === null) return;
      note = n;
    }
    reviewingItems = new Set(reviewingItems).add(k);
    try {
      await api.recordReview(it.owner_type, it.id, decision, note);
      await load();
      onChanged();
    } catch (e) {
      void alertDialog(`Review failed: ${e}`);
    } finally {
      const next = new Set(reviewingItems);
      next.delete(k);
      reviewingItems = next;
    }
  }

  const signoff = $derived((queue?.items ?? []).filter((i) => i.needs_review && !i.decision));
  const decided = $derived((queue?.items ?? []).filter((i) => i.needs_review && i.decision));
</script>

<Modal {open} title={`Needs review — ${projectName}`} {onClose} width="860px">
  {#if loading && !queue}
    <p class="muted">Loading…</p>
  {:else}
    {#if err}<p class="err">{err}</p>{/if}

    <section class="blk">
      <div class="blk-head">
        <strong>Awaiting classification</strong>
        <span class="count">{inbox.length}</span>
        <span class="hint">the model sorted these — accept or override</span>
      </div>
      {#if inbox.length === 0}
        <p class="empty">Nothing pending — new captures land here (and on their cards) until you accept them.</p>
      {:else}
        {#each inbox as item (item.id)}
          <article class="entry">
            <div class="meta">
              <span class="cat">{item.proposed_category ?? '?'}</span>
              {#if padNames[item.scratchpad_id]}<span class="pad" title="Scratchpad">🗂 {padNames[item.scratchpad_id]}</span>{/if}
              <span class="excerpt">{item.content.slice(0, 140)}{item.content.length > 140 ? '…' : ''}</span>
            </div>
            {#if item.classification_reasoning}
              <div class="reasoning">↳ {item.classification_reasoning}</div>
            {/if}
            <div class="actions">
              <button class="ok" disabled={isBusy(item.id)}
                onclick={() => act(item.id, 'accept', () => api.acceptItem(item.id))}>
                {isBusy(item.id, 'accept') ? 'Accepting…' : `Accept ${item.proposed_category}`}
              </button>
              {#each ['todo', 'bug', 'kb', 'use_case'] as cat (cat)}
                {#if item.proposed_category !== cat}
                  <button disabled={isBusy(item.id)}
                    onclick={() => act(item.id, cat, () => api.reclassifyItem(item.id, cat as 'todo' | 'bug' | 'kb' | 'use_case'))}>
                    {isBusy(item.id, cat) ? '…' : `→ ${cat}`}
                  </button>
                {/if}
              {/each}
              <button class="danger" disabled={isBusy(item.id)}
                onclick={() => act(item.id, 'reject', () => api.rejectItem(item.id))}>
                {isBusy(item.id, 'reject') ? 'Rejecting…' : 'Reject'}
              </button>
            </div>
          </article>
        {/each}
      {/if}
    </section>

    <section class="blk">
      <div class="blk-head">
        <strong>Awaiting your sign-off</strong>
        <span class="count">{signoff.length}</span>
        {#if queue}
          <span class="hint" title={queue.trust.explanation}>
            trust: {queue.trust.tier} · escalating {queue.escalate_at_or_above}+ — your decisions feed the dial
          </span>
        {/if}
      </div>
      {#if signoff.length === 0}
        <p class="empty">
          Nothing escalated. Implemented work below the escalation threshold auto-clears on
          earned trust; anything risky, failing, or in an always-review zone appears here.
        </p>
      {:else}
        {#each signoff as it (it.owner_type + it.id)}
          <article class="entry rq">
            <span class="band {it.band}" title="Risk: {it.band} (score {it.score})">{it.band}</span>
            <span class="subj" title={it.reasons.join(' · ')}>
              <span class="num">#{it.number}</span>{it.subject}
              {#if it.domains?.length}<span class="domain">{it.domains.join(', ')}</span>{/if}
            </span>
            <span class="flags">
              {#if it.unanchored}<span class="flag warn" title="No code location recorded — can't be traced or verified">⚠ no location</span>{/if}
              {#if it.reverted}<span class="flag bad">reverted</span>{/if}
              {#if it.in_zone}<span class="flag zone">zone</span>{/if}
              {#if it.failing}<span class="flag bad">failing</span>
              {:else if !it.has_verification}<span class="flag warn">unverified</span>{/if}
              {#if it.refuted}<span class="flag bad" title="AI review refuted a criterion">AI✗</span>{/if}
            </span>
            <span class="actions">
              <button class="ok" disabled={isReviewing(it)} onclick={() => review(it, 'approved')}>✓ Approve</button>
              <button class="danger" disabled={isReviewing(it)} onclick={() => review(it, 'rejected')}>✗ Rework</button>
            </span>
          </article>
        {/each}
      {/if}
      {#if decided.length}
        <p class="muted small">{decided.length} already decided this cycle — see the item's Log for each decision.</p>
      {/if}
    </section>
  {/if}
</Modal>

<style>
  .muted { color: var(--p-777777); font-size: 12px; }
  .small { font-size: 11px; margin: 6px 2px 0; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .blk { margin-bottom: 18px; }
  .blk-head {
    display: flex; align-items: baseline; gap: 8px; margin-bottom: 8px;
    color: var(--p-e8e8e8); font-size: 13px;
  }
  .count {
    background: var(--p-1e3a52); color: var(--p-99ccff); border-radius: 9px;
    font-size: 11px; padding: 0 7px; line-height: 17px;
  }
  .hint { color: var(--p-777777); font-size: 11px; }
  .empty {
    color: var(--p-777777); font-size: 12px; font-style: italic;
    background: var(--p-121212); border: 1px dashed var(--p-2a2a2a);
    border-radius: 6px; padding: 10px 12px; margin: 0;
  }
  .entry {
    background: var(--p-141414); border: 1px solid var(--p-262626); border-radius: 6px;
    padding: 8px 10px; margin-bottom: 6px;
  }
  .meta { display: flex; gap: 8px; align-items: baseline; }
  .cat {
    flex: none; font-size: 10px; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--p-ccaaff); border: 1px solid var(--p-3a2a55); border-radius: 3px; padding: 1px 6px;
  }
  .pad { flex: none; font-size: 10px; color: var(--p-7d97b0); font-family: ui-monospace, monospace; }
  .excerpt { font-size: 12.5px; color: var(--p-dddddd); }
  .reasoning { font-size: 11px; color: var(--p-888888); margin-top: 4px; }
  .actions { display: flex; gap: 6px; margin-top: 8px; flex-wrap: wrap; }
  .actions button { font-size: 11px; padding: 3px 9px; }
  .actions .ok { background: var(--p-13210f); color: var(--p-a9d5a9); border-color: var(--p-2c5a2c); }
  .actions .danger { background: var(--p-2a1414); color: var(--p-e0a0a0); border-color: var(--p-553030); }
  .entry.rq { display: flex; align-items: center; gap: 10px; }
  .band {
    flex: none; font-size: 10px; font-weight: 600; border-radius: 3px; padding: 1px 7px;
    text-transform: uppercase; letter-spacing: 0.4px;
  }
  .band.Low { color: var(--p-99cc99); background: var(--p-13210f); }
  .band.Medium { color: var(--p-e0c07a); background: var(--p-241d0f); }
  .band.High { color: var(--p-e0a070); background: var(--p-2a1a0f); }
  .band.Critical { color: var(--p-ff8888); background: var(--p-2a1414); }
  .band.Unknown { color: var(--p-999999); background: var(--p-1a1a1a); }
  .subj { flex: 1; min-width: 0; font-size: 12.5px; color: var(--p-dddddd); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .num { color: var(--p-888888); margin-right: 6px; font-family: ui-monospace, monospace; font-size: 11px; }
  .domain { margin-left: 8px; font-size: 10px; color: var(--p-7d97b0); font-family: ui-monospace, monospace; }
  .flags { display: inline-flex; gap: 4px; flex: none; }
  .flag { font-size: 10px; border-radius: 3px; padding: 1px 5px; }
  .flag.warn { color: var(--p-e0c07a); background: var(--p-241d0f); }
  .flag.bad { color: var(--p-ff8888); background: var(--p-2a1414); }
  .flag.zone { color: var(--p-99ccff); background: var(--p-16222e); }
  .entry.rq .actions { margin-top: 0; flex: none; }
</style>
