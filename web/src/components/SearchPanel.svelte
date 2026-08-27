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
  import { onMount, onDestroy, tick } from 'svelte';
  import * as api from '../lib/api';
  import type { SearchHit, SearchHitKind } from '../lib/api';

  // Right-side slide-in for semantic search results. Mirror of Lists in
  // shape (same slide animation, same dismissal pattern). Live-debounced
  // input fires search on each keystroke after a short delay; click on a
  // hit routes through the same callbacks the code-canvas anchor click
  // already uses (lifted modal state for derived items, scratchpad
  // navigation + flash for raw items).

  type Props = {
    open: boolean;
    projectId: string;
    onClose: () => void;
    onOpenScratchpadItem: (id: string, scratchpadId: string) => void;
    onOpenTodo: (id: string) => void;
    onOpenBug: (id: string) => void;
    onOpenUseCase: (id: string) => void;
    onOpenKB: (id: string) => void;
  };
  let {
    open,
    projectId,
    onClose,
    onOpenScratchpadItem,
    onOpenTodo,
    onOpenBug,
    onOpenUseCase,
    onOpenKB,
  }: Props = $props();

  let q = $state('');
  let hits = $state<SearchHit[]>([]);
  let scanned = $state(0);
  let loading = $state(false);
  let err = $state('');
  let inputEl = $state<HTMLInputElement | undefined>(undefined);

  // Debounce timer id; cancelled on each new keystroke.
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  const DEBOUNCE_MS = 300;
  const MIN_CHARS = 2;

  function onInput() {
    if (debounceTimer) clearTimeout(debounceTimer);
    if (q.trim().length < MIN_CHARS) {
      hits = [];
      err = '';
      loading = false;
      return;
    }
    debounceTimer = setTimeout(runSearch, DEBOUNCE_MS);
  }

  async function runSearch() {
    if (!projectId || q.trim().length < MIN_CHARS) return;
    loading = true;
    err = '';
    const myQ = q;
    try {
      const res = await api.search(projectId, q);
      // If the input changed while we were waiting, drop these results.
      if (myQ !== q) return;
      hits = res.hits;
      scanned = res.scanned;
    } catch (e) {
      err = String(e);
    } finally {
      if (myQ === q) loading = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      onClose();
    }
  }
  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));

  // Auto-focus the input each time the panel opens.
  let lastOpen = false;
  $effect(() => {
    if (open && !lastOpen) {
      lastOpen = true;
      void tick().then(() => inputEl?.focus());
    } else if (!open) {
      lastOpen = false;
    }
  });

  function kindLabel(k: SearchHitKind): string {
    switch (k) {
      case 'scratchpad_item': return 'Item';
      case 'todo_item': return 'Todo';
      case 'bug_item': return 'Bug';
      case 'knowledge_entry': return 'KB';
      case 'use_case_item': return 'Use Case';
    }
  }
  function kindColor(k: SearchHitKind): string {
    switch (k) {
      case 'todo_item': return 'var(--p-99ccff)';
      case 'bug_item': return 'var(--p-ff8888)';
      case 'knowledge_entry': return 'var(--p-cceeff)';
      case 'use_case_item': return 'var(--p-ffcc99)';
      case 'scratchpad_item': return 'var(--p-dddddd)';
    }
  }

  function openHit(h: SearchHit) {
    switch (h.kind) {
      case 'scratchpad_item':
        onOpenScratchpadItem(h.id, h.scratchpad_id ?? '');
        break;
      case 'todo_item': onOpenTodo(h.id); break;
      case 'bug_item': onOpenBug(h.id); break;
      case 'use_case_item': onOpenUseCase(h.id); break;
      case 'knowledge_entry': onOpenKB(h.id); break;
    }
  }

  function pct(score: number): string {
    return Math.round(score * 100) + '%';
  }
</script>

<aside class="search" class:open aria-hidden={!open} aria-label="Search panel">
  <header class="head">
    <span class="title">Search</span>
    <button
      type="button"
      class="x"
      onclick={onClose}
      aria-label="Close search panel"
      title="Close (Esc)"
    >×</button>
  </header>

  <div class="input-row">
    <input
      type="search"
      bind:value={q}
      bind:this={inputEl}
      oninput={onInput}
      placeholder="Search items by meaning…"
      aria-label="Search query"
    />
  </div>

  <div class="content">
    {#if err}
      <div class="err">{err}</div>
    {:else if loading}
      <div class="muted">Searching…</div>
    {:else if q.trim().length < MIN_CHARS}
      <div class="muted">
        Type at least {MIN_CHARS} characters. Search uses local embeddings —
        no network required, no model match counts.
      </div>
    {:else if hits.length === 0}
      <div class="muted">
        No matches above the relevance threshold. Try rephrasing,
        or run <code>codedistill embed-backfill</code> if many items
        are unembedded.
      </div>
    {:else}
      <div class="meta">
        {hits.length} hit{hits.length === 1 ? '' : 's'}
        {#if scanned > 0} · {scanned} scanned{/if}
      </div>
      <ul class="hits">
        {#each hits as h (h.kind + ':' + h.id)}
          <li>
            <button type="button" class="hit" onclick={() => openHit(h)}>
              <div class="hit-head">
                <span class="kind" style:color={kindColor(h.kind)}>{kindLabel(h.kind)}</span>
                <span class="title">{h.title || '(untitled)'}</span>
                {#if h.scratchpad_name}
                  <span class="pad" title="From scratchpad: {h.scratchpad_name}">in {h.scratchpad_name}</span>
                {/if}
                <span class="score" title="cosine similarity">{pct(h.score)}</span>
              </div>
              {#if h.snippet && h.snippet !== h.title}
                <div class="snippet">{h.snippet}</div>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</aside>

<style>
  .search {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 420px;
    max-width: 90vw;
    background: var(--p-0d0d0d);
    border-left: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    transform: translateX(100%);
    transition: transform 220ms ease;
    z-index: 200;
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.45);
    pointer-events: none;
  }
  .search.open {
    transform: translateX(0);
    pointer-events: auto;
  }
  .head {
    display: flex;
    align-items: center;
    padding: 8px 10px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
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
  .input-row {
    padding: 10px;
    background: var(--p-0a0a0a);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .input-row input {
    width: 100%;
    background: var(--p-0d1117);
    color: var(--p-ffffff);
    border: 1px solid var(--p-2d5578);
    padding: 8px 12px;
    font-size: 13px;
    border-radius: 3px;
  }
  .input-row input:focus {
    outline: none;
    border-color: var(--p-66ccff);
  }
  .content {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 8px;
  }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 8px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
  }
  .muted {
    color: var(--p-777777);
    font-size: 12px;
    font-style: italic;
    padding: 12px;
    line-height: 1.5;
  }
  .muted code {
    background: var(--p-1a1a1a);
    padding: 1px 6px;
    border-radius: 2px;
    font-size: 11px;
    color: var(--p-99ccff);
    font-style: normal;
  }
  .meta {
    font-size: 10px;
    color: var(--p-666666);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding: 0 4px 8px;
  }
  .hits {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .hit {
    width: 100%;
    background: var(--p-161616);
    border: 1px solid var(--p-262626);
    border-radius: 3px;
    padding: 8px 10px;
    text-align: left;
    color: inherit;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .hit:hover {
    background: var(--p-1a2530);
    border-color: var(--p-2d5578);
  }
  .hit-head {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }
  .hit-head .kind {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    flex-shrink: 0;
  }
  .hit-head .title {
    flex: 1;
    font-weight: 500;
    color: var(--p-dddddd);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-transform: none;
    letter-spacing: normal;
  }
  .hit-head .pad {
    font-size: 10px;
    color: var(--p-888888);
    font-style: italic;
    flex-shrink: 0;
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-transform: none;
    letter-spacing: normal;
  }
  .hit-head .score {
    font-size: 10px;
    color: var(--p-888888);
    font-variant-numeric: tabular-nums;
  }
  .snippet {
    font-size: 11px;
    color: var(--p-999999);
    line-height: 1.4;
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  }
</style>
