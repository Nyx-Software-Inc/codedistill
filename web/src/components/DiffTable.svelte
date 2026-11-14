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
  // The rows of a diff: two-sided numbered lines, deletions and insertions
  // marked in place. Shared by the single-file diff mode (DiffViewer, which
  // owns fetching, the banner and its own scroll) and the changeset pane
  // (which stacks one of these per file inside one scroll).
  //
  // Highlighting: the old side and the new side are each highlighted once as
  // whole texts and split per line; a row takes its HTML from the side it
  // belongs to. Deletions therefore highlight as the code they were.
  //
  // With `headers`, each hunk opens with an @@ row that doubles as the
  // expand control; the caller says whether there is anything above or below
  // to expand into and does the expanding.
  //
  // Comments (slice 4): with `review`, every row grows a "+" in the gutter.
  // Click it to open a composer under that line; shift-click another row on
  // the same side to make it a range. A thread renders under the last line
  // it covers. A deletion comments on the old side, anything else on the new.
  import type { DiffHunk, DiffLine, ReviewComment, CommentSide } from '../lib/api';
  import type { CommentAnchor, ReviewActions } from '../lib/reviewComments.svelte';
  import { highlightLines, langFromPath } from '../lib/shiki';
  import CommentThread from './CommentThread.svelte';

  type Props = {
    path: string;
    hunks: DiffHunk[];
    /** Hunk header rows between hunks (off for a whole-file view). */
    headers?: boolean;
    /** Expand controls; absent means not expandable. `above(i)` is for hunk i,
     *  `below` is for the last hunk's tail. */
    expand?: {
      above: (i: number) => boolean;
      below: () => boolean;
      up: (i: number) => void;
      down: (i: number) => void;
      all: () => void;
    } | null;
    /** The comments on this file, and what may be done with them. */
    comments?: ReviewComment[];
    review?: ReviewActions | null;
    /** Called once rows are in the DOM after each (re)highlight. */
    onRendered?: () => void;
  };
  let { path, hunks, headers = false, expand = null, comments = [], review = null, onRendered }: Props = $props();

  // Per-line HTML keyed by side: oldHtml[old-1], newHtml[new-1].
  let oldHtml = $state<string[]>([]);
  let newHtml = $state<string[]>([]);
  // The hunks the HTML was computed for. Rendering reads THESE, so a hunk
  // change never shows new text with stale colours for a frame.
  let shown = $state<DiffHunk[]>([]);
  let seq = 0;

  $effect(() => {
    const hs = hunks;
    const p = path;
    const my = ++seq;
    void (async () => {
      const [o, n] = await highlightSides(p, hs);
      if (my !== seq) return;
      oldHtml = o;
      newHtml = n;
      shown = hs;
      queueMicrotask(() => onRendered?.());
    })();
  });

  // Highlight each side as the text the hunks carry. Lines a hunk does not
  // carry (outside the context window) have no HTML and are never rendered.
  async function highlightSides(p: string, hs: DiffHunk[]): Promise<[string[], string[]]> {
    const lang = langFromPath(p);
    const oldLines: string[] = [];
    const newLines: string[] = [];
    const oldIdx: number[] = [];
    const newIdx: number[] = [];
    for (const h of hs) {
      for (const l of h.lines) {
        if (l.op !== 'add' && l.old) { oldLines.push(l.text); oldIdx.push(l.old); }
        if (l.op !== 'del' && l.new) { newLines.push(l.text); newIdx.push(l.new); }
      }
    }
    const [oh, nh] = await Promise.all([highlightLines(oldLines, lang), highlightLines(newLines, lang)]);
    const o: string[] = [];
    const n: string[] = [];
    oldIdx.forEach((num, i) => { o[num - 1] = oh[i]; });
    newIdx.forEach((num, i) => { n[num - 1] = nh[i]; });
    return [o, n];
  }

  function html(l: DiffLine): string {
    if (l.op === 'del') return oldHtml[(l.old ?? 0) - 1] ?? escape(l.text);
    return newHtml[(l.new ?? 0) - 1] ?? escape(l.text);
  }
  function escape(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
  function hunkHeader(h: DiffHunk): string {
    return `@@ -${h.old_start},${h.old_lines} +${h.new_start},${h.new_lines} @@`;
  }

  // ── Comments ──────────────────────────────────────────────────────────────
  // The side a row comments on, and that side's line number.
  function sideOf(l: DiffLine): CommentSide { return l.op === 'del' ? 'old' : 'new'; }
  function lineOf(l: DiffLine): number { return (l.op === 'del' ? l.old : l.new) ?? 0; }

  // Threads keyed by the row they render under: `${side}:${line_end}`.
  const threads = $derived.by(() => {
    const m = new Map<string, ReviewComment[]>();
    for (const c of comments) {
      const k = `${c.side}:${c.line_end}`;
      const arr = m.get(k);
      if (arr) arr.push(c);
      else m.set(k, [c]);
    }
    return m;
  });
  function threadsAt(l: DiffLine): { key: string; list: ReviewComment[] }[] {
    const out: { key: string; list: ReviewComment[] }[] = [];
    if (l.op !== 'add' && l.old) {
      const list = threads.get(`old:${l.old}`);
      if (list) out.push({ key: `old:${l.old}`, list });
    }
    if (l.op !== 'del' && l.new) {
      const list = threads.get(`new:${l.new}`);
      if (list) out.push({ key: `new:${l.new}`, list });
    }
    return out;
  }
  function anchorOf(list: ReviewComment[]): CommentAnchor {
    const first = list[0];
    return { side: first.side, line_start: first.line_start, line_end: first.line_end };
  }

  let composer = $state<CommentAnchor | null>(null);
  let draft = $state('');
  let sending = $state(false);
  let composeErr = $state('');

  function plus(e: MouseEvent, l: DiffLine) {
    const side = sideOf(l);
    const n = lineOf(l);
    if (!n) return;
    if (e.shiftKey && composer && composer.side === side) {
      composer = { side, line_start: Math.min(composer.line_start, n), line_end: Math.max(composer.line_end, n) };
      return;
    }
    composer = { side, line_start: n, line_end: n };
    composeErr = '';
  }
  function cancelCompose() {
    composer = null;
    draft = '';
    composeErr = '';
  }
  async function submitCompose() {
    const b = draft.trim();
    if (!b || !composer || !review || sending) return;
    sending = true;
    composeErr = '';
    try {
      await review.add(path, composer, b);
      cancelCompose();
    } catch (e) {
      composeErr = String(e);
    } finally {
      sending = false;
    }
  }
  function composerUnder(l: DiffLine): boolean {
    return !!composer && composer.side === sideOf(l) && composer.line_end === lineOf(l);
  }
  function inRange(l: DiffLine): boolean {
    if (!composer) return false;
    const n = composer.side === 'old' ? l.old : l.new;
    if (!n) return false;
    if (composer.side === 'old' && l.op === 'add') return false;
    if (composer.side === 'new' && l.op === 'del') return false;
    return n >= composer.line_start && n <= composer.line_end;
  }
  function rangeLabel(a: CommentAnchor): string {
    return a.line_start === a.line_end ? `${a.side} ${a.line_start}` : `${a.side} ${a.line_start}–${a.line_end}`;
  }
</script>

<table class="lines" class:commentable={!!review}>
  <tbody>
    {#each shown as h, hi (hi)}
      {#if headers}
        <tr class="hunk">
          <td class="num" colspan="2">
            {#if expand}
              {#if hi > 0}
                <button type="button" class="ex" title="Show 10 more lines below the previous hunk" aria-label="Expand previous hunk down" onclick={() => expand?.down(hi - 1)}>▾</button>
              {/if}
              {#if expand.above(hi)}
                <button type="button" class="ex" title="Show 10 more lines above" aria-label="Expand up" onclick={() => expand?.up(hi)}>▴</button>
              {/if}
            {/if}
          </td>
          <td class="text">{hunkHeader(h)}</td>
        </tr>
      {/if}
      {#each h.lines as l, li (hi + ':' + li)}
        <tr class={l.op} class:selected={inRange(l)}>
          <td class="num old">
            {#if review && lineOf(l)}
              <button type="button" class="plus" title="Comment on this line (shift-click to extend a range)" aria-label="Comment on line" onclick={(e) => plus(e, l)}>+</button>
            {/if}
            {l.old ?? ''}
          </td>
          <td class="num new">{l.new ?? ''}</td>
          <td class="text"><span class="mark" aria-hidden="true">{l.op === 'add' ? '+' : l.op === 'del' ? '−' : ' '}</span><span class="shiki frag">{@html html(l)}</span></td>
        </tr>
        {#if review}
          {#each threadsAt(l) as t (t.key)}
            <tr class="thread-row">
              <td colspan="3"><CommentThread {path} anchor={anchorOf(t.list)} comments={t.list} {review} /></td>
            </tr>
          {/each}
          {#if composer && composerUnder(l)}
            <tr class="thread-row compose">
              <td colspan="3">
                <div class="composer">
                  <div class="c-head">New comment on <span class="range">{rangeLabel(composer)}</span><span class="hint">shift-click another line to extend</span></div>
                  <!-- svelte-ignore a11y_autofocus -->
                  <textarea class="box" bind:value={draft} rows={3} autofocus placeholder="What should change here, or why?"
                    onkeydown={(e) => {
                      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void submitCompose(); }
                      if (e.key === 'Escape') { e.preventDefault(); cancelCompose(); }
                    }}></textarea>
                  {#if composeErr}<div class="err">{composeErr}</div>{/if}
                  <div class="row">
                    <button type="button" class="btn primary" disabled={sending || !draft.trim()} onclick={() => void submitCompose()}>Comment</button>
                    <button type="button" class="btn" disabled={sending} onclick={cancelCompose}>Cancel</button>
                    <span class="hint">Ctrl+Enter</span>
                  </div>
                </div>
              </td>
            </tr>
          {/if}
        {/if}
      {/each}
    {/each}
    {#if headers && expand && shown.length > 0 && expand.below()}
      <tr class="hunk tail">
        <td class="num" colspan="2">
          <button type="button" class="ex" title="Show 10 more lines below" aria-label="Expand down" onclick={() => expand?.down(shown.length - 1)}>▾</button>
        </td>
        <td class="text"><button type="button" class="all" onclick={() => expand?.all()}>whole file</button></td>
      </tr>
    {/if}
  </tbody>
</table>

<style>
  table.lines {
    border-collapse: collapse;
    width: 100%;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    font-size: var(--code-font-size, 12px);
    line-height: 1.5;
  }
  td { padding: 0; vertical-align: top; white-space: pre; }
  td.num {
    width: 1%;
    min-width: 40px;
    text-align: right;
    padding: 0 8px;
    color: var(--p-777777);
    background: var(--p-0a0a0a);
    user-select: none;
    border-right: 1px solid var(--p-262626);
    position: relative;
  }
  .commentable td.num.old { padding-left: 22px; }
  td.num.new { border-right: 1px solid var(--p-262626); }
  td.text { padding: 0 12px 0 4px; }
  /* app.css colours tokens under .shiki and paints its background; here the
     row owns the background, so the fragment stays transparent. */
  td.text :global(.frag),
  td.text :global(.frag .line) { background: transparent !important; }
  .mark { display: inline-block; width: 12px; color: var(--p-777777); user-select: none; }
  tr.add td.text { background: var(--diff-add-bg); }
  tr.del td.text { background: var(--diff-del-bg); }
  tr.add td.num { background: var(--diff-add-gutter); color: var(--diff-add-fg); }
  tr.del td.num { background: var(--diff-del-gutter); color: var(--diff-del-fg); }
  tr.add .mark { color: var(--diff-add-fg); }
  tr.del .mark { color: var(--diff-del-fg); }
  tr.selected td.text { box-shadow: inset 3px 0 0 var(--p-99ccff); }
  tr.hunk td { background: var(--p-161616); color: var(--p-888888); font-size: 11px; padding: 1px 12px; }
  tr.hunk td.num { border-right: none; text-align: center; padding: 0 4px; white-space: nowrap; }
  tr.hunk.tail td.text { padding: 2px 12px; }
  .ex {
    background: transparent; border: none; color: var(--p-99ccff); cursor: pointer;
    font-size: 12px; line-height: 1; padding: 1px 3px; border-radius: 3px;
  }
  .ex:hover { background: var(--p-1e3a52); color: var(--p-ffffff); }
  .all {
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-99ccff);
    cursor: pointer; font-size: 10px; padding: 0 6px; border-radius: 3px; font-family: inherit;
  }
  .all:hover { background: var(--p-1e3a52); color: var(--p-ffffff); }

  /* The "+" lives in the old-number cell's left padding and shows on hover —
     the gutter stays a gutter until you point at it. */
  .plus {
    position: absolute; left: 3px; top: 1px;
    width: 15px; height: 15px; line-height: 13px; padding: 0;
    border-radius: 3px; border: none; background: var(--p-2d5578); color: var(--p-ffffff);
    font-size: 13px; font-weight: 700; cursor: pointer; text-align: center;
    opacity: 0;
  }
  tr:hover .plus, .plus:focus-visible { opacity: 1; }
  .plus:hover { background: var(--p-99ccff); color: var(--p-0a0a0a); }

  tr.thread-row td { padding: 0; white-space: normal; }
  .composer {
    font-family: var(--font-family, system-ui, sans-serif); font-size: 12px;
    background: var(--p-161616); border-top: 1px solid var(--p-2a2a2a); border-bottom: 1px solid var(--p-2a2a2a);
    border-left: 3px solid var(--p-2d5578); padding: 6px 10px 6px 12px; color: var(--p-dddddd);
  }
  .c-head { display: flex; align-items: baseline; gap: 10px; color: var(--p-aaaaaa); font-size: 11px; }
  .range { color: var(--p-99ccff); font-family: var(--code-font-family, ui-monospace, monospace); }
  .hint { color: var(--p-666666); font-size: 10px; margin-left: auto; }
  .box {
    width: 100%; box-sizing: border-box; margin-top: 4px;
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-eeeeee);
    border-radius: 4px; padding: 5px 8px; font-size: 12px; font-family: inherit; resize: vertical;
  }
  .box:focus { outline: none; border-color: var(--p-99ccff); }
  .row { display: flex; gap: 6px; align-items: center; margin-top: 4px; }
  .btn {
    font-size: 11px; padding: 3px 10px; border-radius: 4px; cursor: pointer;
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-aaaaaa);
  }
  .btn.primary { background: var(--p-2d5578); border-color: var(--p-2d5578); color: var(--p-cceeff); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .err { color: var(--p-ff8888); font-size: 11px; margin-top: 4px; }
</style>
