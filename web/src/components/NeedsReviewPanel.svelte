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
  import Icon from './Icon.svelte';
  import ScratchpadItemEdit from './ScratchpadItemEdit.svelte';
  import ClassifyBar from './ClassifyBar.svelte';
  import type { ScratchpadItem } from '../lib/types';
  import { alertDialog, promptDialog } from '../lib/dialog';

  // A PANE, not a modal — which is the whole point of the panes work. This was
  // a <Modal> you opened, acted in and dismissed, and that framing is what made
  // the classification queue invisible: with List as the default scratchpad
  // view, 39 findings needed accept/reject and nothing on screen said so. A
  // surface you need continuously must not be something you keep opening.
  //
  // Dropping the modal changed three things. There is no `open` prop, because a
  // pane renders only when it is the active pane in its slot — mounted IS
  // visible. There is no `onClose`, because PaneFrame owns closing. And the
  // width is no longer 860px: the right slot is ~460 and the bottom slot is the
  // full window, so the layout below is container-query driven rather than
  // assuming room for a single row.
  type Props = {
    projectId: string;
    projectName?: string;
    /** Bumped by App whenever its data cycle ran, so a pane left open does not
     *  go stale. A modal got a fresh load every time it opened; a pane has to be
     *  told. */
    tick?: number;
    // Fired after any decision so App refreshes counts + canvas.
    onChanged: () => void;
    /** Open an implemented item's detail. App owns those modals, so unlike the
     *  pending section — whose source editor this panel mounts itself — this one
     *  has to travel out. Worth the prop: approving work you cannot inspect is a
     *  worse version of the problem this whole change is about. */
    onOpenItem?: (ownerType: string, ownerId: string) => void;
  };
  let { projectId, projectName = '', tick = 0, onChanged, onOpenItem }: Props = $props();

  let inbox = $state<ScratchpadItem[]>([]);
  let queue = $state<api.ReviewQueueResponse | null>(null);
  let loading = $state(false);
  let err = $state('');
  let loadedFor = $state('');
  /** The capture being read, or null. */
  let opened = $state<ScratchpadItem | null>(null);

  // Reload on mount, on a project switch, and on App's tick. Reading both
  // values inside the effect is what subscribes to them — the previous version
  // keyed off `open`, which no longer exists.
  $effect(() => {
    const key = projectId + ':' + tick;
    if (projectId && loadedFor !== key) {
      loadedFor = key;
      void load();
    }
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

  // Per-item busy state used to live here as a shared Set, because accepting
  // item A would otherwise disable B and C (Rich's find). ClassifyBar makes that
  // structural instead: each row renders its own instance with its own busy
  // flag, so one decision cannot block another by construction.

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

<!-- Owned here rather than reached through App: the panel is a pane, so it
     cannot call ScratchpadPane's openItem router. A pending capture has no
     derived item by definition, so the source editor is the only correct
     target anyway. -->
<ScratchpadItemEdit
  item={opened}
  onClose={() => (opened = null)}
  onSaved={() => { void load(); onChanged(); }}
/>

<div class="nr">
  <!-- The project name lived in the modal's title bar. The pane's header is a
       tab strip reading "Needs review", so the scope has to be stated here or
       you cannot tell whose queue you are looking at. -->
  <div class="scope">
    <span class="proj">{projectName || 'No project'}</span>
    <button class="reload" onclick={() => void load()} disabled={loading} title="Reload the queue">
      {loading ? '…' : '↻'}
    </button>
  </div>

  <div class="nr-body">
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
              <!-- Deciding without being able to inspect was the real gap: the
                   panel could accept, reclassify and reject a capture but had no
                   way to READ it. Sits in the metadata line that already exists,
                   so it costs no extra row. -->
              <button
                class="open"
                title="Open this capture"
                aria-label="Open this capture"
                onclick={() => (opened = item)}
              ><Icon name="edit" size={11} /></button>
              <span class="cat">{item.proposed_category ?? '?'}</span>
              {#if padNames[item.scratchpad_id]}<span class="pad" title="Scratchpad">🗂 {padNames[item.scratchpad_id]}</span>{/if}
              <!-- Full content, clamped to three lines, rather than a 140-char
                   slice. The old cut landed mid-word at a fixed count and
                   ignored how wide the pane actually is — and the pane is
                   resizable now. Most captures are short enough to show whole,
                   so only long ones clamp. -->
              <span class="excerpt">{item.content}</span>
            </div>
            {#if item.classification_reasoning}
              <div class="reasoning">↳ {item.classification_reasoning}</div>
            {/if}
            <!-- The same accept/reclassify/reject as every other view.
                 expanded: this is a queue you work straight through, so all
                 four categories stay inline — a dropdown would cost a click
                 per item on the one surface built for a run of decisions. -->
            <ClassifyBar item={item} expanded onChanged={() => { void load(); onChanged(); }} />
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
            {#if onOpenItem}
              <button
                class="open"
                title="Open {it.subject}"
                aria-label="Open {it.subject}"
                onclick={() => onOpenItem(it.owner_type, it.id)}
              ><Icon name="edit" size={11} /></button>
            {/if}
          </article>
        {/each}
      {/if}
      {#if decided.length}
        <p class="muted small">{decided.length} already decided this cycle — see the item's Log for each decision.</p>
      {/if}
    </section>
  {/if}
  </div>
</div>

<style>
  .nr {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
    /* The unit the queries below measure. A pane is 460px at the right edge and
       the full window along the bottom, so entries cannot assume either. */
    container-type: inline-size;
  }
  .nr-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 10px 12px 14px;
  }

  .scope {
    display: flex; align-items: center; gap: 8px;
    padding: 5px 10px;
    border-bottom: 1px solid var(--p-222222);
    flex: none;
  }
  .proj {
    flex: 1; min-width: 0; font-size: 11px; color: var(--p-888888);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .reload {
    flex: none; border: 0; background: none; color: var(--p-777777);
    cursor: pointer; font-size: 12px; padding: 0 3px;
  }
  .reload:hover:not(:disabled) { color: var(--text); }

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
  /* flex-start, not baseline: the excerpt is up to three lines now, and
     baseline alignment would drag the badges down to its last line. */
  .meta { display: flex; gap: 8px; align-items: flex-start; }

  .open {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    padding: 0;
    margin-top: 1px;
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    background: none;
    color: var(--p-8a8a8a);
    cursor: pointer;
  }
  .open:hover { color: var(--text); border-color: var(--p-555555); background: var(--p-262626); }
  /* The sign-off row is one centred line, so it does not want the pending
     row's top nudge (that exists to line the button up with the first line of
     a three-line excerpt). */
  .entry.rq .open { margin-top: 0; }
  .cat {
    flex: none; font-size: 10px; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--p-ccaaff); border: 1px solid var(--p-3a2a55); border-radius: 3px; padding: 1px 6px;
  }
  .pad { flex: none; font-size: 10px; color: var(--p-7d97b0); font-family: ui-monospace, monospace; }
  .excerpt {
    flex: 1;
    min-width: 0;
    font-size: 12.5px;
    color: var(--p-dddddd);
    /* Three lines, then ellipsis — and it keeps the badges on line one because
       the box only grows downward. */
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
    white-space: pre-wrap;
    word-break: break-word;
  }
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

  /* Narrow pane: the one-line row becomes three stacked lines. At 460px the
     band, a subject, four flags and two buttons cannot share a row — the
     subject would ellipsis down to nothing and the buttons would be the only
     thing you could read. */
  @container (max-width: 560px) {
    .entry.rq {
      display: grid;
      grid-template-columns: auto 1fr auto;
      grid-template-areas:
        'band    subj    open'
        'flags   flags   flags'
        'actions actions actions';
      gap: 6px 8px;
      align-items: start;
    }
    .entry.rq .band { grid-area: band; }
    /* Without this the button has no area and lands in the first free cell —
       the row BELOW actions, which is where Rich found it. */
    .entry.rq .open { grid-area: open; }
    .entry.rq .subj {
      grid-area: subj;
      /* Wraps instead of truncating: two lines of subject beats an ellipsis. */
      white-space: normal;
      overflow: visible;
      text-overflow: clip;
    }
    .entry.rq .flags { grid-area: flags; flex-wrap: wrap; }
    .entry.rq .actions { grid-area: actions; }
    .entry.rq .domain { margin-left: 0; display: block; }
    /* The classification entry's own buttons already wrap; the hint does not. */
    .blk-head { flex-wrap: wrap; }
  }
</style>
