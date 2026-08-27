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
  import type { QACitation, QACitationKind } from '../lib/api';
  import { shouldSubmit, submitHintText } from '../lib/submitShortcut';

  // Right-side slide-in for retrieval-augmented Q&A. Single-turn v1:
  // type a question, get an answer with [item:abc] / [code:def] citation
  // markers rendered as inline clickable spans. Click → routes through
  // the same handlers SearchPanel uses for items, plus a new "open file
  // at line range" path for code-chunk citations.

  type Props = {
    open: boolean;
    projectId: string;
    onClose: () => void;
    onOpenScratchpadItem: (id: string, scratchpadId: string) => void;
    onOpenTodo: (id: string) => void;
    onOpenBug: (id: string) => void;
    onOpenUseCase: (id: string) => void;
    onOpenKB: (id: string) => void;
    // Code-chunk citation click → open the file in the code canvas at
    // the chunk's line range.
    onOpenCodeChunk: (filePath: string, lineStart: number, lineEnd: number) => void;
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
    onOpenCodeChunk,
  }: Props = $props();

  let q = $state('');
  let answer = $state('');
  let citations = $state<QACitation[]>([]);
  let loading = $state(false);
  let err = $state('');
  let usedItems = $state(0);
  let usedChunks = $state(0);
  let inputEl = $state<HTMLTextAreaElement | undefined>(undefined);

  function submit() {
    if (!q.trim() || loading) return;
    void run();
  }

  async function run() {
    loading = true;
    err = '';
    answer = '';
    citations = [];
    usedItems = 0;
    usedChunks = 0;
    try {
      const res = await api.qa(projectId, q);
      answer = res.answer;
      citations = res.citations;
      usedItems = res.used_items;
      usedChunks = res.used_chunks;
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) onClose();
  }
  function onInputKey(e: KeyboardEvent) {
    if (shouldSubmit(e)) {
      e.preventDefault();
      submit();
    }
  }

  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));

  let lastOpen = false;
  $effect(() => {
    if (open && !lastOpen) {
      lastOpen = true;
      void tick().then(() => inputEl?.focus());
    } else if (!open) {
      lastOpen = false;
    }
  });

  // Citation routing — mirror of SearchPanel's openHit plus code chunks.
  function openCitation(c: QACitation) {
    switch (c.kind) {
      case 'scratchpad_item':
        onOpenScratchpadItem(c.id, c.scratchpad_id ?? '');
        break;
      case 'todo_item': onOpenTodo(c.id); break;
      case 'bug_item': onOpenBug(c.id); break;
      case 'use_case_item': onOpenUseCase(c.id); break;
      case 'knowledge_entry': onOpenKB(c.id); break;
      case 'code_chunk':
        if (c.file_path) {
          onOpenCodeChunk(c.file_path, c.line_start ?? 0, c.line_end ?? 0);
        }
        break;
    }
  }

  // Parse the answer text and render with [marker] segments replaced by
  // clickable spans. Unknown markers (model hallucinated) render as the
  // original literal text so the user sees what the model said.
  type Segment =
    | { kind: 'text'; text: string }
    | { kind: 'cite'; cit: QACitation };
  let segments = $derived.by<Segment[]>(() => {
    if (!answer) return [];
    const byMarker = new Map<string, QACitation>();
    for (const c of citations) byMarker.set(c.marker, c);
    const out: Segment[] = [];
    // Match [item:xxx] or [code:xxx]; xxx is alphanumeric + optional dashes.
    const re = /\[(item|code):([A-Za-z0-9_-]+)\]/g;
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(answer)) !== null) {
      if (m.index > last) out.push({ kind: 'text', text: answer.slice(last, m.index) });
      const marker = `${m[1]}:${m[2]}`;
      const cit = byMarker.get(marker);
      if (cit) {
        out.push({ kind: 'cite', cit });
      } else {
        out.push({ kind: 'text', text: m[0] });
      }
      last = m.index + m[0].length;
    }
    if (last < answer.length) out.push({ kind: 'text', text: answer.slice(last) });
    return out;
  });

  function citeLabel(c: QACitation): string {
    switch (c.kind) {
      case 'code_chunk':
        return c.file_path && c.line_start
          ? `${shortPath(c.file_path)}:${c.line_start}`
          : 'code';
      default:
        return c.title || c.kind;
    }
  }
  function shortPath(p: string): string {
    const slash = p.lastIndexOf('/');
    return slash >= 0 ? p.slice(slash + 1) : p;
  }
  function citeColor(k: QACitationKind): string {
    switch (k) {
      case 'todo_item': return 'var(--p-99ccff)';
      case 'bug_item': return 'var(--p-ff8888)';
      case 'knowledge_entry': return 'var(--p-cceeff)';
      case 'use_case_item': return 'var(--p-ffcc99)';
      case 'scratchpad_item': return 'var(--p-dddddd)';
      case 'code_chunk': return '#c084fc';
    }
  }
</script>

<aside class="qa" class:open aria-hidden={!open} aria-label="Q&A panel">
  <header class="head">
    <span class="title">Ask</span>
    <button
      type="button"
      class="x"
      onclick={onClose}
      aria-label="Close Q&A panel"
      title="Close (Esc)"
    >×</button>
  </header>

  <div class="input-row">
    <textarea
      bind:value={q}
      bind:this={inputEl}
      onkeydown={onInputKey}
      placeholder={`Ask anything about this project — items + indexed code.\n${submitHintText()}.`}
      rows="3"
      aria-label="Question"
    ></textarea>
    <button
      type="button"
      class="send"
      onclick={submit}
      disabled={loading || !q.trim()}
    >{loading ? 'Thinking…' : 'Ask'}</button>
  </div>

  <div class="content">
    {#if err}
      <div class="err">{err}</div>
    {:else if loading}
      <div class="muted">Retrieving + answering…</div>
    {:else if !answer && citations.length === 0}
      <div class="muted">
        Q&A retrieves the most relevant items + code chunks for your
        question, then asks the local model to answer using only that
        context. Citations are clickable.
        <br /><br />
        Make sure you've run <code>codedistill index-code</code> at least
        once for code chunks to be available.
      </div>
    {:else}
      <div class="meta">
        Drew on {usedItems} item{usedItems === 1 ? '' : 's'}
        + {usedChunks} code chunk{usedChunks === 1 ? '' : 's'}
      </div>
      <div class="answer">
        {#each segments as seg}
          {#if seg.kind === 'text'}{seg.text}{:else}
            <button
              type="button"
              class="cite"
              style:color={citeColor(seg.cit.kind)}
              title={seg.cit.title}
              onclick={() => openCitation(seg.cit)}
            >{citeLabel(seg.cit)}</button>
          {/if}
        {/each}
      </div>
      {#if citations.length > 0}
        <details class="sources">
          <summary>All sources ({citations.length})</summary>
          <ul>
            {#each citations as c (c.marker)}
              <li>
                <button
                  type="button"
                  class="src"
                  style:color={citeColor(c.kind)}
                  onclick={() => openCitation(c)}
                >
                  <span class="src-kind">{c.kind}</span>
                  <span class="src-title">{c.title}</span>
                </button>
              </li>
            {/each}
          </ul>
        </details>
      {/if}
    {/if}
  </div>
</aside>

<style>
  .qa {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 520px;
    max-width: 95vw;
    background: var(--p-0d0d0d);
    border-left: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    transform: translateX(100%);
    transition: transform 220ms ease, box-shadow 220ms ease;
    z-index: 200;
    /* Shadow only while open — a closed panel sits just off-screen, and a
       permanent shadow would project a dark strip back over the viewport's
       right edge (avatar, Send, canvas). */
    box-shadow: none;
    pointer-events: none;
  }
  .qa.open {
    transform: translateX(0);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.45);
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
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  textarea {
    width: 100%;
    background: var(--p-0d1117);
    color: var(--p-ffffff);
    border: 1px solid var(--p-2d5578);
    padding: 8px 12px;
    font-size: 13px;
    font-family: inherit;
    border-radius: 3px;
    resize: vertical;
    min-height: 64px;
  }
  textarea:focus {
    outline: none;
    border-color: var(--p-66ccff);
  }
  .send {
    align-self: flex-end;
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    padding: 5px 14px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
  }
  .send:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-ffffff); }
  .send:disabled { opacity: 0.5; cursor: not-allowed; }
  .content {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 12px;
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
    color: var(--p-888888);
    font-size: 12px;
    font-style: italic;
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
    margin-bottom: 10px;
  }
  .answer {
    font-size: 13px;
    line-height: 1.6;
    white-space: pre-wrap;
    color: var(--p-dddddd);
  }
  .cite {
    background: transparent;
    border: 1px solid currentColor;
    padding: 0 6px;
    border-radius: 10px;
    font-size: 10px;
    line-height: 1.4;
    cursor: pointer;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    margin: 0 2px;
    vertical-align: baseline;
    opacity: 0.85;
  }
  .cite:hover { opacity: 1; background: rgba(255, 255, 255, 0.05); }
  .sources {
    margin-top: 20px;
    border-top: 1px solid var(--p-262626);
    padding-top: 10px;
  }
  .sources summary {
    font-size: 11px;
    color: var(--p-888888);
    cursor: pointer;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    padding: 4px 0;
  }
  .sources summary:hover { color: var(--p-dddddd); }
  .sources ul {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .src {
    width: 100%;
    background: transparent;
    border: 1px solid var(--p-262626);
    color: inherit;
    text-align: left;
    padding: 6px 10px;
    font-size: 12px;
    cursor: pointer;
    border-radius: 3px;
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .src:hover { background: var(--p-1a2530); border-color: var(--p-2d5578); }
  .src-kind {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    flex-shrink: 0;
    opacity: 0.7;
  }
  .src-title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--p-dddddd);
  }
</style>
