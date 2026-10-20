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
  // Accept / reclassify / reject — one implementation, every view.
  //
  // Rich set List as his default scratchpad view and 39 analysis findings sat
  // there with nothing on screen saying they needed anything. Accept was
  // reachable on canvas cards and in Needs review, and NOWHERE else: List,
  // Kanban and Calendar did not even reference 'pending-review'.
  //
  // There were already TWO implementations before this — the canvas banner
  // routing through ScratchpadPane's onAccept/onReclassify props, and
  // NeedsReviewPanel calling the API itself. Adding three more copies would
  // have made five places for "accept" to drift apart. So this is the only one,
  // and the props it replaced are deleted rather than left live.
  //
  // Self-acting for the same reason DependencyEditor is: it mounts in five
  // places, and a component whose behaviour arrives through five parents has
  // five chances to be wired wrong.
  import * as api from '../lib/api';
  import type { ScratchpadItem } from '../lib/types';
  import { alertDialog, confirmDialog } from '../lib/dialog';

  type Cat = 'todo' | 'bug' | 'kb' | 'use_case';

  type Props = {
    item: ScratchpadItem;
    /** Narrow surfaces — list rows, kanban cards. Shortens the labels without
     *  going icon-only, which would mean hovering to learn what Accept does. */
    compact?: boolean;
    /** Every category inline instead of behind the menu. For Needs review,
     *  which is a queue you work straight through: hiding three of the four
     *  choices behind a dropdown costs a click per item, and that surface
     *  exists precisely to make a run of decisions fast. */
    expanded?: boolean;
    /** Fired after any decision so the caller refreshes. */
    onChanged: () => void;
  };
  let { item, compact = false, expanded = false, onChanged }: Props = $props();

  const CATS: Cat[] = ['todo', 'bug', 'kb', 'use_case'];
  const LABELS: Record<Cat, string> = {
    todo: 'Todo', bug: 'Bug', kb: 'Knowledge', use_case: 'Use case',
  };

  const proposed = $derived.by<Cat | ''>(() => {
    const c = item.proposed_category ?? '';
    return (CATS as string[]).includes(c) ? (c as Cat) : '';
  });
  const others = $derived.by(() => CATS.filter((c) => c !== proposed));

  // Shown only while the item is actually waiting on a person. Every other
  // state renders nothing at all, so callers can mount this unconditionally.
  const pending = $derived(item.classification_state === 'pending-review');

  let busy = $state('');
  let menuOpen = $state(false);

  async function run(label: string, fn: () => Promise<unknown>) {
    if (busy) return;
    busy = label;
    menuOpen = false;
    try {
      await fn();
      onChanged();
    } catch (e) {
      void alertDialog(`${label} failed: ${e}`);
    } finally {
      busy = '';
    }
  }

  const accept = () => run('Accept', () => api.acceptItem(item.id));
  const reclassify = (c: Cat) => run(`Reclassify to ${LABELS[c]}`,
    () => api.reclassifyItem(item.id, c));

  async function reject() {
    menuOpen = false;
    // Confirmed because it is the one irreversible-feeling choice here, and it
    // now appears on the canvas, where it never used to — a misclick on a
    // surface you use constantly should not quietly discard a capture.
    const ok = await confirmDialog(
      'Reject this capture? It leaves the review queue and will not become a work item.',
      { title: 'Reject capture', confirmLabel: 'Reject', danger: true },
    );
    if (!ok) return;
    await run('Reject', () => api.rejectItem(item.id));
  }
</script>

{#if pending}
  <!-- data-grid-nodrag: on the canvas these sit inside a draggable card, and a
       pointerdown on a button must not start a drag. Harmless elsewhere. -->
  <div class="cb" class:compact data-grid-nodrag>
    {#if proposed}
      <button
        type="button"
        class="accept"
        disabled={!!busy}
        onclick={(e) => { e.stopPropagation(); void accept(); }}
        title="Accept the agent's classification"
      >{busy === 'Accept' ? '…' : (compact ? `✓ ${LABELS[proposed]}` : `Accept ${LABELS[proposed]}`)}</button>
    {/if}
    {#if expanded}
      {#each others as c (c)}
        <button
          type="button"
          disabled={!!busy}
          onclick={(e) => { e.stopPropagation(); void reclassify(c); }}
          title="Re-classify as {LABELS[c]}"
        >{busy === `Reclassify to ${LABELS[c]}` ? '…' : `→ ${LABELS[c]}`}</button>
      {/each}
      <button
        type="button"
        class="rej-inline"
        disabled={!!busy}
        onclick={(e) => { e.stopPropagation(); void reject(); }}
      >{busy === 'Reject' ? '…' : 'Reject'}</button>
    {:else}
    <div class="wrap">
      <button
        type="button"
        class="other"
        disabled={!!busy}
        aria-expanded={menuOpen}
        aria-haspopup="menu"
        onclick={(e) => { e.stopPropagation(); menuOpen = !menuOpen; }}
        title={proposed ? 'Re-classify to another category, or reject' : 'Classify this capture'}
      >{proposed ? (compact ? '▾' : 'Other ▾') : (compact ? 'Classify ▾' : 'Classify ▾')}</button>
      {#if menuOpen}
        <div class="menu" role="menu">
          {#each others as c (c)}
            <button
              type="button"
              role="menuitem"
              onclick={(e) => { e.stopPropagation(); void reclassify(c); }}
            >{LABELS[c]}</button>
          {/each}
          <button
            type="button"
            role="menuitem"
            class="rej"
            onclick={(e) => { e.stopPropagation(); void reject(); }}
          >Reject</button>
        </div>
      {/if}
    </div>
    {/if}
  </div>
{/if}

<style>
  .cb {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
  }

  .cb button {
    font-family: inherit;
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 4px;
    border: 1px solid var(--p-3a3a3a);
    background: var(--p-222222);
    color: var(--p-dddddd);
    cursor: pointer;
    white-space: nowrap;
  }
  .cb button:disabled { opacity: 0.6; cursor: default; }
  .cb.compact button { font-size: 10px; padding: 1px 6px; }

  .accept {
    background: var(--p-13210f) !important;
    border-color: var(--p-2c5a2c) !important;
    color: var(--p-a9d5a9) !important;
  }
  .accept:hover:not(:disabled) { background: var(--p-1a3015) !important; }

  .wrap { position: relative; display: flex; }

  .cb { flex-wrap: wrap; }
  .rej-inline {
    background: var(--p-2a1414) !important;
    border-color: var(--p-553030) !important;
    color: var(--p-e0a0a0) !important;
  }

  .menu {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 60;
    margin-top: 2px;
    background: var(--p-222222);
    border: 1px solid var(--p-3a3a3a);
    border-radius: 4px;
    padding: 2px;
    display: flex;
    flex-direction: column;
    min-width: 7.5rem;
  }
  .menu button {
    border: 0;
    background: none;
    text-align: left;
    border-radius: 3px;
  }
  .menu button:hover { background: var(--p-333333); }
  .menu .rej {
    color: var(--p-e0a0a0);
    border-top: 1px solid var(--p-2f2f2f);
    margin-top: 2px;
    border-radius: 0 0 3px 3px;
  }
</style>
