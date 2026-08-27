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
  // Glass-box Phase 2: an item's measurable "definition of done." Soft —
  // criteria are a ratified contract, not a gate. AI-proposed criteria (slice
  // 2) arrive as 'proposed' for invited review (Accept / Dismiss); hand-typed
  // ones are their own ratification (created 'accepted').
  import * as api from '../lib/api';
  import type { AcceptanceCriterion, CodeAnchorOwnerType, CriterionState } from '../lib/types';
  import { draftingCriteria, markDrafting } from '../lib/draftingStore';

  type Props = {
    ownerType: CodeAnchorOwnerType;
    ownerId: string;
  };
  let { ownerType, ownerId }: Props = $props();

  let criteria = $state<AcceptanceCriterion[]>([]);
  let loading = $state(false);
  let loadErr = $state('');
  let loadedKey = $state('');
  let newText = $state('');
  let busy = $state(false);

  // Drafting runs as a background job now — this item is "drafting" while its id
  // is in the shared set (which the modal AND the card read). Covers reopening
  // the modal mid-draft, since the flag lives in the store, not this component.
  const drafting = $derived($draftingCriteria.has(ownerId));

  $effect(() => {
    const key = `${ownerType}:${ownerId}`;
    if (!ownerId || key === loadedKey) return;
    loadedKey = key;
    void reload();
  });

  // When the background draft finishes (the item leaves the set), pull in the
  // freshly-persisted criteria — this is what makes them appear without a manual
  // refresh, whether the modal was open the whole time or just reopened.
  let wasDrafting = $state(false);
  $effect(() => {
    const now = $draftingCriteria.has(ownerId);
    if (wasDrafting && !now) void reload();
    wasDrafting = now;
  });

  async function reload() {
    loading = true;
    loadErr = '';
    try {
      criteria = await api.listAcceptanceCriteria(ownerType, ownerId);
    } catch (e) {
      loadErr = String(e);
    } finally {
      loading = false;
    }
  }

  // Dismissed criteria stay in the model (so slice-2 drafting won't re-propose
  // them) but drop out of the working list.
  const visible = $derived(criteria.filter((c) => c.state !== 'rejected'));
  const accepted = $derived(visible.filter((c) => c.state === 'accepted' || c.state === 'satisfied').length);

  async function add() {
    const text = newText.trim();
    if (!text || busy) return;
    busy = true;
    try {
      await api.createAcceptanceCriterion(ownerType, ownerId, { text });
      newText = '';
      await reload();
    } catch (e) {
      loadErr = String(e);
    } finally {
      busy = false;
    }
  }

  async function patch(c: AcceptanceCriterion, body: { text?: string; state?: CriterionState }) {
    try {
      await api.updateAcceptanceCriterion(c.id, body);
      await reload();
    } catch (e) {
      loadErr = String(e);
    }
  }

  async function remove(c: AcceptanceCriterion) {
    try {
      await api.deleteAcceptanceCriterion(c.id);
      await reload();
    } catch (e) {
      loadErr = String(e);
    }
  }

  function saveEdit(c: AcceptanceCriterion, value: string) {
    const t = value.trim();
    if (t && t !== c.text) void patch(c, { text: t });
  }

  function addKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      void add();
    }
  }

  async function draftWithAI() {
    if (drafting) return;
    loadErr = '';
    try {
      // Starts a background job and returns immediately (202). It runs
      // server-side to completion regardless of this modal — mark the item so
      // the card + this bar show "drafting…" at once; the completion effect
      // above pulls the criteria in when it's done.
      await api.draftAcceptanceCriteria(ownerType, ownerId);
      markDrafting(ownerId);
    } catch (e) {
      loadErr = `AI draft failed: ${e}`;
    }
  }
</script>

<section class="ac">
  {#if loading && criteria.length === 0}
    <p class="muted">Loading…</p>
  {:else}
    {#if loadErr}<p class="err">{loadErr}</p>{/if}

    {#if drafting}
      <p class="drafting-banner">
        ✨ <strong>Drafting criteria in the background.</strong> You don't have to
        wait — close this and keep working; they'll appear here (and clear the
        ✨ on the card) when the model finishes.
      </p>
    {/if}

    {#if visible.length === 0}
      <div class="empty">
        <p class="empty-head">No acceptance criteria yet.</p>
        <p class="sub">
          Spell out what "done" means as a few checkable statements — the
          contract the build is measured against. Let the AI draft a starting
          set, or add your own below.
        </p>
        <button class="draft-btn" disabled={drafting} onclick={draftWithAI}>
          {drafting ? 'Drafting…' : '✨ Draft with AI'}
        </button>
      </div>
    {:else}
      <div class="head">
        <span class="count">{accepted}/{visible.length} accepted</span>
        <button class="draft-link" disabled={drafting} onclick={draftWithAI}>
          {drafting ? 'Drafting…' : '✨ Draft more'}
        </button>
      </div>
      <ul class="list">
        {#each visible as c (c.id)}
          <li class="row" class:proposed={c.state === 'proposed'}>
            <span class="state {c.state}" title={c.state}></span>
            <input
              class="text"
              value={c.text}
              onblur={(e) => saveEdit(c, e.currentTarget.value)}
              onkeydown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); }}
            />
            {#if c.provenance === 'ai-proposed'}<span class="ai" title="Drafted by the AI">AI</span>{/if}
            <span class="actions">
              {#if c.state === 'proposed'}
                <button class="act ok" title="Accept" onclick={() => patch(c, { state: 'accepted' })}>✓</button>
                <button class="act no" title="Dismiss" onclick={() => patch(c, { state: 'rejected' })}>✕</button>
              {/if}
              <button class="act del" title="Delete" onclick={() => remove(c)}>🗑</button>
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    <div class="add">
      <input
        class="text"
        placeholder="Add a criterion — what must be true for this to be done?"
        bind:value={newText}
        onkeydown={addKey}
      />
      <button class="add-btn" disabled={busy || !newText.trim()} onclick={add}>Add</button>
    </div>
  {/if}
</section>

<style>
  .ac { padding: 4px 2px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .drafting-banner {
    font-size: 12px; line-height: 1.5; color: var(--p-cfe6ff);
    background: var(--p-16222e); border: 1px solid var(--p-2d5578);
    border-radius: 6px; padding: 8px 12px; margin: 0 0 10px;
  }
  .head { margin-bottom: 6px; display: flex; align-items: center; justify-content: space-between; }
  .count { font-size: 11px; color: var(--p-888888); text-transform: uppercase; letter-spacing: 0.5px; }
  .draft-link {
    background: none; border: none; cursor: pointer;
    font-size: 11px; color: var(--p-99ccff); padding: 0;
  }
  .draft-link:hover:not(:disabled) { color: var(--p-cceeff); }
  .draft-link:disabled { opacity: 0.5; cursor: default; }
  .draft-btn {
    margin-top: 10px;
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    border-radius: 4px;
    padding: 6px 14px;
    font-size: 12px;
    cursor: pointer;
  }
  .draft-btn:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .draft-btn:disabled { opacity: 0.6; cursor: default; }
  .list { list-style: none; margin: 0 0 8px; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-radius: 4px;
    padding: 4px 8px;
  }
  .row.proposed { border-left: 2px solid var(--p-d4a54d); }
  .state { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--p-555555); }
  .state.proposed { background: var(--p-d4a54d); }
  .state.accepted { background: var(--p-66cc66); }
  .state.satisfied { background: var(--p-66cc66); box-shadow: 0 0 0 2px rgba(102, 204, 102, 0.25); }
  .state.failed { background: var(--p-e07a7a); }
  .text {
    flex: 1;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 3px;
    color: var(--p-eeeeee);
    font-size: 13px;
    padding: 3px 5px;
  }
  .text:hover { border-color: var(--p-262626); }
  .text:focus { outline: none; border-color: var(--p-2d5578); background: var(--p-0a0a0a); }
  .ai {
    flex: none;
    font-size: 9px;
    font-weight: 600;
    color: var(--p-d4a54d);
    border: 1px solid var(--p-d4a54d);
    border-radius: 3px;
    padding: 0 3px;
  }
  .actions { display: inline-flex; gap: 2px; flex: none; }
  .act {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 12px;
    padding: 2px 4px;
    border-radius: 3px;
    color: var(--p-888888);
  }
  .act:hover { background: var(--p-262626); }
  .act.ok:hover { color: var(--p-66cc66); }
  .act.no:hover, .act.del:hover { color: var(--p-e07a7a); }
  .add { display: flex; gap: 6px; }
  .add .text { border-color: var(--p-262626); }
  .add-btn {
    flex: none;
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    border-radius: 3px;
    padding: 4px 12px;
    font-size: 12px;
    cursor: pointer;
  }
  .add-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .empty { text-align: center; padding: 14px 12px; }
  .empty-head { color: var(--p-bbbbbb); font-size: 13px; margin: 0 0 4px; }
  .sub { font-size: 12px; line-height: 1.5; color: var(--p-777777); margin: 0 auto; max-width: 360px; }
</style>
