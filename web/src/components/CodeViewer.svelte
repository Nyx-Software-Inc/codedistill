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
  import { onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import { highlight, langFromPath } from '../lib/shiki';
  import type { CodeAnchorWithOwner } from '../lib/types';
  import Icon from './Icon.svelte';

  // Read-only renderer for one file at one revision. Shows a status banner
  // (working copy vs. historical SHA), a line-number gutter, an anchor
  // gutter (icons + rails for any code anchor pointing at this file), and
  // Shiki-highlighted code. For working-copy reads, polls mtime so external
  // edits show up without a manual reload.

  type Props = {
    projectId: string;
    path: string;
    // "" = working copy; otherwise full SHA. Historical reads skip the
    // mtime poll because their content is immutable.
    revision: string;
    // Click on an anchor icon in the gutter — bubbled up to App so it can
    // route to a scratchpad item or open a detail modal.
    onAnchorClick: (anchor: CodeAnchorWithOwner) => void;
    // Drop the current selection as a new scratchpad item with a file
    // anchor. App handles the create + anchor + refresh + flash; this
    // component just emits the snippet payload.
    onCreateItem: (payload: {
      path: string;
      revision: string;
      lineStart: number;
      lineEnd: number;
      content: string;
    }) => Promise<void> | void;
    // Whether App has an active scratchpad to create the new item on.
    // When false, the create action is disabled with an explanatory tip.
    hasActiveScratchpad: boolean;
    // App-driven highlight request. When the props (path, revision) match
    // this viewer, we apply it as a selection and scroll the start line
    // into view, then call onHighlightConsumed so the request clears.
    pendingHighlight: {
      path: string;
      revision: string;
      lineStart: number;
      lineEnd: number;
    } | null;
    onHighlightConsumed: () => void;
    // Reverse hover from a scratchpad card's anchor badge — when the
    // hovered card anchor matches this viewer's (path, revision), we
    // tint the line range using the same on-demand mechanism as the
    // gutter-icon hover. Internal gutter hover takes precedence.
    externalHover: import('../lib/types').CodeAnchor | null;
  };
  let {
    projectId, path, revision,
    onAnchorClick, onCreateItem, hasActiveScratchpad,
    pendingHighlight, onHighlightConsumed,
    externalHover,
  }: Props = $props();

  let content = $state('');
  let highlighted = $state('');
  let binary = $state(false);
  let mtime = $state('');
  let anchors = $state<CodeAnchorWithOwner[]>([]);
  let loading = $state(false);
  let err = $state('');

  // Line selection — 1-indexed inclusive. Clicking a line number sets both
  // start and end; shift-clicking another line extends the range. Cleared
  // automatically whenever the file/revision changes (effect below).
  let selStart = $state<number | null>(null);
  let selEnd = $state<number | null>(null);
  let creating = $state(false);

  // On-demand hover-tint: the anchor whose icon is currently hovered. We
  // render a faint coloured overlay on the matching line range so the
  // user can preview "what this anchor covers" without committing to a
  // permanent always-on highlight (which would clash with Shiki).
  let hoveredAnchor = $state<CodeAnchorWithOwner | null>(null);

  // Active anchor for the tint overlay: prefer internal gutter hover,
  // fall back to external (card-badge) hover when it targets this file.
  // CodeAnchorWithOwner extends CodeAnchor so the union is shape-safe
  // for the fields the renderer needs (line_start/end, provenance).
  let activeHover = $derived.by<import('../lib/types').CodeAnchor | null>(() => {
    if (hoveredAnchor) return hoveredAnchor;
    if (externalHover
      && externalHover.path === path
      && (externalHover.revision ?? '') === revision) {
      return externalHover;
    }
    return null;
  });

  // Track which (project, path, revision) we last loaded to avoid duplicate
  // fetches when the parent passes the same props on re-render.
  let loadedKey = '';

  const POLL_MS = 3000;
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  $effect(() => {
    const key = `${projectId}|${path}|${revision}`;
    if (key !== loadedKey) {
      loadedKey = key;
      // Clear selection on tab/file change so we don't carry a range
      // across to a different file.
      selStart = null;
      selEnd = null;
      void load();
      void loadAnchors();
    }
  });

  $effect(() => {
    // Restart the mtime + anchor poll whenever the watched file changes.
    void revision; void path; void projectId;
    if (pollTimer) clearInterval(pollTimer);
    if (path) {
      pollTimer = setInterval(tick, POLL_MS);
    }
    return () => { if (pollTimer) clearInterval(pollTimer); };
  });

  onDestroy(() => { if (pollTimer) clearInterval(pollTimer); });

  // Highlight latch: when a pendingHighlight arrives whose (path, revision)
  // matches this viewer AND the file content is loaded, apply it as a
  // selection + scroll into view, then call onHighlightConsumed so App
  // clears the request. Uses identity tracking so re-applying the same
  // highlight requires App to null-then-set.
  let lastHighlightKey = $state<string | null>(null);
  $effect(() => {
    const h = pendingHighlight;
    if (!h) return;
    if (h.path !== path || h.revision !== revision) return;
    if (!content) return; // wait for load() before applying
    const key = `${h.path}|${h.revision}|${h.lineStart}|${h.lineEnd}`;
    if (key === lastHighlightKey) return;
    lastHighlightKey = key;
    selStart = h.lineStart;
    selEnd = h.lineEnd > h.lineStart ? h.lineEnd : h.lineStart;
    onHighlightConsumed();
    queueMicrotask(() => {
      // Scroll the .code-view container so the start line is visible.
      const view = document.querySelector('.viewer .code-view') as HTMLElement | null;
      if (view) {
        const top = (h.lineStart - 1) * LINE_HEIGHT_PX;
        view.scrollTo({ top: Math.max(0, top - 60), behavior: 'smooth' });
      }
    });
  });

  // Request-sequence guard: a fast file/revision switch can leave an older
  // fetch resolving after a newer one. Each load claims a token and applies its
  // result only if it's still the latest — a superseded response is dropped, so
  // the viewer never shows a stale file (audit M20).
  let loadSeq = 0;
  async function load() {
    if (!projectId || !path) return;
    const my = ++loadSeq;
    loading = true;
    err = '';
    try {
      const fc = await api.fileContent(projectId, path, revision);
      if (my !== loadSeq) return;
      const hl = fc.binary ? '' : await highlight(fc.content, langFromPath(path));
      if (my !== loadSeq) return; // re-check after the highlight await
      binary = fc.binary;
      content = fc.binary ? '' : fc.content;
      mtime = fc.mtime ?? '';
      highlighted = hl;
    } catch (e) {
      if (my !== loadSeq) return;
      err = String(e);
    } finally {
      if (my === loadSeq) loading = false;
    }
  }

  async function loadAnchors() {
    if (!projectId || !path) {
      anchors = [];
      return;
    }
    try {
      anchors = await api.listProjectAnchorsByPath(projectId, path);
    } catch {
      anchors = [];
    }
  }

  async function tick() {
    // Poll: refetch anchors (cheap) and check mtime (working copy only).
    void loadAnchors();
    if (revision !== '' || !path || !projectId) return;
    try {
      const r = await api.fileMtime(projectId, path);
      if (r.mtime && r.mtime !== mtime) {
        await load();
      }
    } catch {
      // Silent — the file may have been deleted; load() would surface that.
    }
  }

  let lineCount = $derived(content ? content.split('\n').length : 0);

  // Selection helpers — same gesture as FileAnchorModal so the muscle
  // memory transfers. Single click = one line; if that line is blank,
  // expand down through the run of blanks to the next non-blank. Shift
  // click extends the range from the current start.
  function isBlank(s: string | undefined): boolean {
    return s === undefined || s.trim() === '';
  }
  function snapBlankSelection(line: number): { start: number; end: number } {
    const lines = content.split('\n');
    if (!isBlank(lines[line - 1])) return { start: line, end: line };
    let down = line;
    while (down <= lines.length && isBlank(lines[down - 1])) down++;
    if (down <= lines.length) return { start: line, end: down };
    let up = line;
    while (up >= 1 && isBlank(lines[up - 1])) up--;
    if (up >= 1) return { start: up, end: line };
    return { start: line, end: line };
  }
  function handleLineClick(line: number, shiftKey: boolean) {
    if (!shiftKey || selStart === null) {
      const snapped = snapBlankSelection(line);
      selStart = snapped.start;
      selEnd = snapped.end;
    } else {
      if (line < selStart) {
        selEnd = selStart;
        selStart = line;
      } else {
        selEnd = line;
      }
    }
  }
  function clearSelection() {
    selStart = null;
    selEnd = null;
  }

  // Selection slice + label + can-create derivation.
  let selectionSlice = $derived.by(() => {
    if (selStart === null || selEnd === null) return '';
    const lines = content.split('\n');
    return lines.slice(selStart - 1, selEnd).join('\n');
  });
  let selectionLabel = $derived.by(() => {
    if (selStart === null || selEnd === null) return '';
    return selStart === selEnd ? `Line ${selStart}` : `Lines ${selStart}-${selEnd}`;
  });
  let canCreate = $derived(
    selStart !== null && selEnd !== null
    && selectionSlice.trim().length > 0
    && hasActiveScratchpad
    && !creating
  );

  async function createFromSelection() {
    if (selStart === null || selEnd === null || !canCreate) return;
    creating = true;
    try {
      await onCreateItem({
        path,
        revision,
        lineStart: selStart,
        lineEnd: selEnd,
        content: selectionSlice,
      });
      // Clear selection after a successful create so the bar dismisses.
      selStart = null;
      selEnd = null;
    } catch (e) {
      err = `Failed to create scratchpad item: ${e}`;
    } finally {
      creating = false;
    }
  }

  // Anchor styling. Icon + color depend on provenance; tooltip carries
  // owner_type + owner_title.
  type AnchorStyle = { icon: 'anchor' | 'sparkle' | 'link'; color: string };
  function anchorStyle(a: CodeAnchorWithOwner): AnchorStyle {
    switch (a.provenance) {
      case 'agent-suggested':
        return { icon: 'sparkle', color: '#c084fc' };
      case 'url-detected':
        return { icon: 'link', color: '#7aa9c8' };
      default:
        return { icon: 'anchor', color: '#6cf' };
    }
  }

  function ownerLabel(t: string): string {
    switch (t) {
      case 'scratchpad_item': return 'Scratchpad item';
      case 'todo_item': return 'TODO';
      case 'bug_item': return 'BUG';
      case 'knowledge_entry': return 'KB';
      case 'use_case_item': return 'Use case';
      default: return t;
    }
  }

  function anchorTooltip(a: CodeAnchorWithOwner): string {
    const range = a.line_start && a.line_end && a.line_end > a.line_start
      ? `lines ${a.line_start}-${a.line_end}`
      : a.line_start
        ? `line ${a.line_start}`
        : '';
    const head = `${ownerLabel(a.owner_type)}: ${a.owner_title || '(untitled)'}`;
    const meta = [range, a.provenance].filter(Boolean).join(' · ');
    return meta ? `${head}\n${meta}` : head;
  }

  // Layout math for the anchor gutter. Each line is 1.5em tall at 12px ⇒
  // 18px. We position icons + rails in absolute pixel offsets so they
  // align with line numbers regardless of Shiki's whitespace handling.
  const LINE_HEIGHT_PX = 18;
  function topPx(line: number): number { return (line - 1) * LINE_HEIGHT_PX; }
  function railHeightPx(a: CodeAnchorWithOwner): number {
    if (!a.line_start) return 0;
    const end = a.line_end && a.line_end >= a.line_start ? a.line_end : a.line_start;
    return Math.max(LINE_HEIGHT_PX, (end - a.line_start + 1) * LINE_HEIGHT_PX);
  }

  function shortSha(s: string): string { return s.length > 7 ? s.slice(0, 7) : s; }
</script>

<div class="viewer">
  <div class="banner" class:historical={revision !== ''}>
    {#if revision === ''}
      <span class="kind">working copy</span>
    {:else}
      <span class="kind">viewing</span>
      <span class="sha">{shortSha(revision)}</span>
      <span class="hint">historical revision</span>
    {/if}
  </div>

  {#if err}
    <div class="err">{err}</div>
  {:else if loading && !content}
    <div class="muted">Loading…</div>
  {:else if binary}
    <div class="muted">Binary file — preview not available.</div>
  {:else}
    {#if selStart !== null && selEnd !== null}
      <div class="action-bar">
        <span class="sel-label">{selectionLabel} selected</span>
        <button
          type="button"
          class="ghost"
          onclick={clearSelection}
          disabled={creating}
        >Clear</button>
        <button
          type="button"
          class="primary"
          onclick={createFromSelection}
          disabled={!canCreate}
          title={!hasActiveScratchpad ? 'Select a scratchpad first' : selectionSlice.trim().length === 0 ? 'Selection has no content' : ''}
        >
          {creating ? 'Creating…' : 'Drop as scratchpad item'}
        </button>
      </div>
    {:else if anchors.length === 0}
      <div class="hint-bar">
        Click a line number to select; <kbd>Shift</kbd>-click to extend. Then drop the selection as a scratchpad item.
      </div>
    {/if}
    <div class="code-view">
      <div class="gutter" role="presentation">
        {#each Array(lineCount) as _, i (i)}
          {@const n = i + 1}
          <button
            type="button"
            class="line-num"
            class:selected={selStart !== null && selEnd !== null && n >= selStart && n <= selEnd}
            class:anchor={selStart === n || selEnd === n}
            onclick={(e) => handleLineClick(n, e.shiftKey)}
            aria-label="Line {n}"
            title="Click to select line {n} · Shift-click to extend"
          >{n}</button>
        {/each}
      </div>
      <div class="anchor-gutter" role="presentation">
        {#each anchors as a (a.id)}
          {@const s = anchorStyle(a)}
          {#if a.line_start}
            <div
              class="rail"
              class:rail-hot={activeHover?.id === a.id}
              style:top="{topPx(a.line_start)}px"
              style:height="{railHeightPx(a)}px"
              style:background={s.color}
              aria-hidden="true"
            ></div>
            <button
              type="button"
              class="anchor-icon"
              style:top="{topPx(a.line_start)}px"
              style:color={s.color}
              title={anchorTooltip(a)}
              aria-label={anchorTooltip(a)}
              onclick={() => onAnchorClick(a)}
              onmouseenter={() => (hoveredAnchor = a)}
              onmouseleave={() => { if (hoveredAnchor?.id === a.id) hoveredAnchor = null; }}
              onfocus={() => (hoveredAnchor = a)}
              onblur={() => { if (hoveredAnchor?.id === a.id) hoveredAnchor = null; }}
            ><Icon name={s.icon} size={12} /></button>
          {/if}
        {/each}
      </div>
      <div class="code-pane">
        {#if activeHover && activeHover.line_start}
          {@const s = anchorStyle(activeHover as CodeAnchorWithOwner)}
          <div
            class="hover-tint"
            style:top="{topPx(activeHover.line_start)}px"
            style:height="{railHeightPx(activeHover as CodeAnchorWithOwner)}px"
            style:background={s.color}
            aria-hidden="true"
          ></div>
        {/if}
        {@html highlighted}
      </div>
    </div>
  {/if}
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--p-0d1117);
  }
  .banner {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 10px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    font-size: 11px;
    color: var(--p-aaaaaa);
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
  }
  .banner.historical {
    background: var(--p-2a2310);
    color: var(--p-d4a54d);
    border-bottom-color: var(--p-4a3a14);
  }
  .banner .kind {
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-size: 10px;
    opacity: 0.7;
  }
  .banner .sha { color: var(--p-99ccff); }
  .banner.historical .sha { color: var(--p-ffc56c); }
  .banner .hint {
    margin-left: auto;
    font-size: 10px;
    opacity: 0.7;
  }
  .code-view {
    flex: 1;
    display: grid;
    grid-template-columns: 48px 22px 1fr;
    overflow: auto;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    font-size: var(--code-font-size, 12px);
    line-height: 1.5;
    min-height: 0;
  }
  .gutter {
    background: var(--p-0a0a0a);
    border-right: 1px solid var(--p-262626);
    color: var(--p-777777);
    user-select: none;
    text-align: right;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .line-num {
    background: transparent;
    border: none;
    text-align: right;
    padding: 0 8px;
    font: inherit;
    color: inherit;
    cursor: pointer;
    border-radius: 0;
    line-height: 1.5;
    transition: background 80ms ease, color 80ms ease;
  }
  .line-num:hover {
    color: var(--p-ffffff);
    background: var(--p-2d5578);
  }
  .line-num.selected { background: var(--p-1a2a38); color: var(--p-99ccff); }
  .line-num.anchor { background: var(--p-2d5578); color: var(--p-ffffff); }
  .action-bar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 10px;
    background: var(--p-1a2a38);
    border-bottom: 1px solid var(--p-2d5578);
    font-size: 11px;
    color: var(--p-99ccff);
  }
  .action-bar .sel-label {
    flex: 1;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
  }
  .action-bar button {
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 3px;
    cursor: pointer;
  }
  .action-bar .ghost {
    background: transparent;
    border: 1px solid var(--p-2d5578);
    color: var(--p-99ccff);
  }
  .action-bar .ghost:hover:not(:disabled) { background: var(--p-1e3a52); }
  .action-bar .primary {
    background: var(--p-2d5578);
    border: 1px solid var(--p-66ccff);
    color: var(--p-ffffff);
  }
  .action-bar .primary:hover:not(:disabled) { background: var(--p-3a6890); }
  .action-bar button:disabled { opacity: 0.5; cursor: not-allowed; }
  .hint-bar {
    flex-shrink: 0;
    background: var(--p-161616);
    color: var(--p-888888);
    border-bottom: 1px solid var(--p-262626);
    padding: 4px 10px;
    font-size: 11px;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .hint-bar kbd {
    background: var(--p-0d1117);
    border: 1px solid var(--p-2d5578);
    border-radius: 2px;
    padding: 0 4px;
    font-size: 10px;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    color: var(--p-99ccff);
  }
  .anchor-gutter {
    position: relative;
    background: var(--p-0a0a0a);
    border-right: 1px solid var(--p-262626);
    user-select: none;
  }
  .rail {
    position: absolute;
    left: 9px;
    width: 2px;
    opacity: 0.6;
    pointer-events: none;
    transition: opacity 100ms ease, width 100ms ease;
  }
  .rail.rail-hot {
    opacity: 1;
    width: 3px;
    left: 8.5px;
  }
  .anchor-icon {
    position: absolute;
    left: 4px;
    width: 14px;
    height: 18px;
    padding: 0;
    background: var(--p-0a0a0a);
    border: none;
    border-radius: 2px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1;
  }
  .anchor-icon:hover {
    background: var(--p-1a1a1a);
    transform: scale(1.15);
  }
  .code-pane {
    overflow-x: auto;
    background: var(--code-bg);
    position: relative;
  }
  .hover-tint {
    position: absolute;
    left: 0;
    right: 0;
    opacity: 0.18;
    pointer-events: none;
    z-index: 0;
  }
  .code-pane :global(pre) { position: relative; z-index: 1; }
  .code-pane :global(pre.shiki),
  .code-pane :global(pre.plain) {
    margin: 0;
    padding: 0 12px;
    background: transparent !important;
    font-family: inherit;
    font-size: inherit;
    line-height: inherit;
  }
  .code-pane :global(code) { font-family: inherit; }
  .muted { color: var(--p-777777); font-size: 12px; padding: 12px; }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 8px 12px;
    background: var(--p-2a1414);
    border-bottom: 1px solid var(--p-4a2020);
  }
</style>
