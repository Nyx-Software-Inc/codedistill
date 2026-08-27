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
  import type { CanvasFilter } from '../lib/types';

  // Right-side slide-out hosting every canvas view filter (Backlog
  // bug #1): the category filter that used to be a chip row above the
  // entry box — where it read as a twin of the classify-as buttons —
  // plus the Open-only toggle. New filters (tags, provenance, …)
  // belong here too.

  type Props = {
    open: boolean;
    filter: CanvasFilter;
    counts: (f: CanvasFilter) => number;
    hideDone: boolean;
    hiddenByDoneCount: number;
    onFilterChange: (f: CanvasFilter) => void;
    onToggleHideDone: () => void;
    onClose: () => void;
  };
  let {
    open,
    filter,
    counts,
    hideDone,
    hiddenByDoneCount,
    onFilterChange,
    onToggleHideDone,
    onClose,
  }: Props = $props();

  const FILTERS: { f: CanvasFilter; label: string; hint?: string }[] = [
    { f: 'all', label: 'All' },
    { f: 'todo', label: 'Todo' },
    { f: 'bug', label: 'Bug' },
    { f: 'kb', label: 'KB' },
    { f: 'use_case', label: 'Use Case' },
    { f: 'unclassified', label: 'Unclassified' },
    { f: 'pending_review', label: 'Pending review', hint: 'Items the agent flagged for confirmation' },
  ];

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) onClose();
  }
  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));
</script>

<aside class="fpanel" class:open aria-hidden={!open} aria-label="Canvas filters">
  <header class="head">
    <span class="title">Filters</span>
    <button type="button" class="x" onclick={onClose} aria-label="Close filters" title="Close (Esc)">×</button>
  </header>
  <div class="content">
    <div class="section-label">Show type</div>
    <div role="radiogroup" aria-label="Filter by category" class="radio-list">
      {#each FILTERS as { f, label, hint } (f)}
        <button
          type="button"
          class="row"
          class:on={filter === f}
          onclick={() => onFilterChange(f)}
          aria-pressed={filter === f}
          title={hint}
        >
          <span class="dot">{filter === f ? '◉' : '○'}</span>
          <span class="lbl">{label}</span>
          <span class="ct">{counts(f)}</span>
        </button>
      {/each}
    </div>

    <div class="divider"></div>

    <button
      type="button"
      class="row"
      class:on={hideDone}
      onclick={onToggleHideDone}
      aria-pressed={hideDone}
      title={hideDone
        ? 'Showing only open items. Click to show all.'
        : 'Hide items whose every derived todo/bug/kb/use case is done.'}
    >
      <span class="dot">{hideDone ? '☑' : '☐'}</span>
      <span class="lbl">Open only</span>
      {#if hideDone && hiddenByDoneCount > 0}
        <span class="ct">{hiddenByDoneCount} hidden</span>
      {/if}
    </button>
  </div>
</aside>

<style>
  .fpanel {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 230px;
    background: var(--p-0d0d0d);
    border-left: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    transform: translateX(100%);
    transition: transform 180ms ease, box-shadow 180ms ease;
    z-index: 200;
    /* Shadow only while open — see QAPanel: a closed off-screen panel with a
       permanent shadow paints a dark strip over the viewport's right edge. */
    box-shadow: none;
    pointer-events: none;
  }
  .fpanel.open {
    transform: translateX(0);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.45);
    pointer-events: auto;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
  }
  .title {
    flex: 1;
    font-size: 12px;
    color: var(--p-dddddd);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .x {
    background: transparent;
    border: none;
    color: var(--p-888888);
    font-size: 18px;
    line-height: 1;
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .x:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .content {
    flex: 1;
    overflow-y: auto;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .section-label {
    color: var(--p-888888);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 2px;
  }
  .radio-list { display: flex; flex-direction: column; }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: none;
    color: var(--p-bbbbbb);
    font-size: 12px;
    padding: 5px 6px;
    border-radius: 3px;
    cursor: pointer;
    text-align: left;
  }
  .row:hover { background: var(--p-1a1a1a); color: var(--p-ffffff); }
  .row.on { color: var(--p-99ccff); }
  .dot { width: 14px; text-align: center; }
  .lbl { flex: 1; }
  .ct {
    color: var(--p-777777);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .row.on .ct { color: var(--p-99ccff); }
  .divider {
    border-top: 1px solid var(--p-262626);
    margin: 8px 0;
  }
</style>
