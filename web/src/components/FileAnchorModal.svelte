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
  import Modal from './Modal.svelte';
  import * as api from '../lib/api';
  import type { CommitInfo } from '../lib/api';
  import { highlight, langFromPath } from '../lib/shiki';

  type Props = {
    open: boolean;
    projectId: string;
    scratchpadId: string;
    // Absolute initial file path (repo-relative). Null when modal is closed.
    path: string | null;
    // Optional drop coordinates so the new item lands where the user dropped.
    gridCol?: number;
    gridRow?: number;
    onClose: () => void;
    onSaved: () => void;
  };
  let {
    open,
    projectId,
    scratchpadId,
    path,
    gridCol,
    gridRow,
    onClose,
    onSaved,
  }: Props = $props();

  // "" → working copy; otherwise a full SHA.
  let revision = $state<string>('');
  let commits = $state<CommitInfo[]>([]);
  let content = $state<string>('');
  let highlighted = $state<string>('');
  let binary = $state<boolean>(false);

  // Line selection — 1-indexed, inclusive. Null when nothing is selected.
  let selStart = $state<number | null>(null);
  let selEnd = $state<number | null>(null);

  let loading = $state(false);
  let saving = $state(false);
  let err = $state('');

  // Reset whenever the modal opens with a new path.
  let snapPath = $state<string>('');
  $effect(() => {
    const key = open && path ? path : '';
    if (key !== snapPath) {
      snapPath = key;
      if (key) {
        revision = '';
        selStart = null;
        selEnd = null;
        commits = [];
        content = '';
        highlighted = '';
        binary = false;
        err = '';
        loadEverything();
      }
    }
  });

  // Request-sequence guard shared by both fetch paths (initial load + revision
  // switch): a fast revision switch could otherwise let an older fetch resolve
  // last, leaving `revision` on B but `content` on A's text — which then gets
  // PERSISTED into the new item's anchor (audit M20). A superseded response is
  // dropped, so content always matches the selected revision.
  let fileSeq = 0;
  async function loadEverything() {
    if (!path) return;
    const my = ++fileSeq;
    loading = true;
    try {
      const [ci, fc] = await Promise.all([
        api.fileCommits(projectId, path, 100).catch(() => [] as CommitInfo[]),
        api.fileContent(projectId, path, ''),
      ]);
      if (my !== fileSeq) return;
      const hl = fc.binary ? '' : await highlight(fc.content, langFromPath(path));
      if (my !== fileSeq) return;
      commits = ci;
      binary = fc.binary;
      content = fc.binary ? '' : fc.content;
      highlighted = hl;
    } catch (e) {
      if (my !== fileSeq) return;
      err = String(e);
    } finally {
      if (my === fileSeq) loading = false;
    }
  }

  async function selectRevision(sha: string) {
    if (sha === revision || !path) return;
    revision = sha;
    const my = ++fileSeq;
    loading = true;
    err = '';
    try {
      const fc = await api.fileContent(projectId, path, sha);
      if (my !== fileSeq) return;
      const hl = fc.binary ? '' : await highlight(fc.content, langFromPath(path));
      if (my !== fileSeq) return;
      binary = fc.binary;
      content = fc.binary ? '' : fc.content;
      highlighted = hl;
    } catch (e) {
      if (my !== fileSeq) return;
      err = String(e);
    } finally {
      if (my === fileSeq) loading = false;
    }
  }

  // Line count for the gutter. Derived from the raw content so selections
  // line up with what the server stored, not with Shiki's rendered HTML.
  let lineCount = $derived(content ? content.split('\n').length : 0);

  function isBlank(s: string | undefined): boolean {
    return s === undefined || s.trim() === '';
  }

  // When the click lands on a blank line, expand the selection down through
  // the run of blanks to the first non-blank line below. If there's nothing
  // non-blank below (clicked into trailing whitespace), expand up instead.
  // For a fully-blank file we fall through to a single-line selection — the
  // canSave guard catches that degenerate case.
  function snapBlankSelection(line: number): { start: number; end: number } {
    const lines = content.split('\n');
    if (!isBlank(lines[line - 1])) {
      return { start: line, end: line };
    }
    let down = line;
    while (down <= lines.length && isBlank(lines[down - 1])) {
      down++;
    }
    if (down <= lines.length) {
      return { start: line, end: down };
    }
    let up = line;
    while (up >= 1 && isBlank(lines[up - 1])) {
      up--;
    }
    if (up >= 1) {
      return { start: up, end: line };
    }
    return { start: line, end: line };
  }

  function handleLineClick(line: number, shiftKey: boolean) {
    if (!shiftKey || selStart === null) {
      // Single click — auto-snap if the clicked line is blank so the user
      // never ends up with an empty-content selection.
      const snapped = snapBlankSelection(line);
      selStart = snapped.start;
      selEnd = snapped.end;
    } else {
      // Shift+click — user-set endpoint; honor it as-is. If the resulting
      // range is all-blank, the canSave guard will block the drop.
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

  function selectionLabel(): string {
    if (selStart === null || selEnd === null) return 'whole file';
    if (selStart === selEnd) return `line ${selStart}`;
    return `lines ${selStart}–${selEnd}`;
  }

  function shortSha(sha: string): string {
    return sha.slice(0, 7);
  }

  function revisionLabel(): string {
    if (revision === '') return 'working copy';
    return shortSha(revision);
  }

  // Compute the content slice the new Scratchpad_Item will carry.
  function buildItemContent(): { content: string; content_type: 'code_snippet' | 'text' } {
    if (selStart !== null && selEnd !== null) {
      const lines = content.split('\n');
      const slice = lines.slice(selStart - 1, selEnd).join('\n');
      return { content: slice, content_type: 'code_snippet' };
    }
    // Whole file → a stub. Embedding the full content would bloat the DB on
    // every drop (v1 decision; revisit after dogfood per plan).
    const rev = revision === '' ? 'working' : shortSha(revision);
    return { content: `File: ${path} @ ${rev}`, content_type: 'text' };
  }

  // Drop is blocked when the user has selected lines that contain no
  // visible characters — the API rejects empty content with 400, and we
  // catch it here with a friendlier inline message.
  let plannedContent = $derived(buildItemContent());
  let canSave = $derived(plannedContent.content.trim().length > 0);

  async function save() {
    if (!path || saving) return;
    if (!canSave) {
      err = 'Selected lines contain no text — pick lines with content, or clear the selection to drop a whole-file anchor.';
      return;
    }
    saving = true;
    err = '';
    try {
      const { content: c, content_type } = plannedContent;
      // 1. Create the Scratchpad_Item.
      const created = await api.createItem(scratchpadId, {
        content: c,
        content_type,
      });
      // 1a. If drop coords are provided, patch grid position.
      if (gridCol !== undefined || gridRow !== undefined) {
        await api.updateItem(created.id, {
          grid_col: gridCol ?? created.grid_col,
          grid_row: gridRow ?? created.grid_row,
        });
      }
      // 2. Bind a file-kind anchor to the new item.
      await api.createCodeAnchor('scratchpad_item', created.id, {
        kind: 'file',
        path,
        line_start: selStart ?? undefined,
        line_end: selEnd ?? undefined,
        revision: revision === '' ? undefined : revision,
      });
      onSaved();
      onClose();
    } catch (e) {
      err = String(e);
    } finally {
      saving = false;
    }
  }

  function fmtDate(s: string): string {
    try {
      return new Date(s).toLocaleDateString();
    } catch {
      return s;
    }
  }
</script>

<Modal {open} title={path ? `Drop: ${path}` : 'Drop file'} onClose={onClose} width="1100px">
  <div class="body">
    {#if err}<div class="err">{err}</div>{/if}

    {#if loading && !content}
      <div class="muted">Loading…</div>
    {:else if binary}
      <div class="muted">Binary file — preview not available. Save as a whole-file anchor below.</div>
    {:else}
      <div class="hint-bar">
        Click a line number to select; <kbd>Shift</kbd>-click another to extend the range. No selection drops as a whole-file anchor.
      </div>
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
        <div class="code-pane">
          {@html highlighted}
        </div>
      </div>
    {/if}
  </div>

  <div class="commits">
    <div class="commits-header">Commits</div>
    <ul>
      <li>
        <button
          type="button"
          class:on={revision === ''}
          onclick={() => selectRevision('')}
        >
          <div class="c-sha">working</div>
          <div class="c-sub">uncommitted state</div>
        </button>
      </li>
      {#each commits as c (c.sha)}
        <li>
          <button
            type="button"
            class:on={revision === c.sha}
            onclick={() => selectRevision(c.sha)}
          >
            <div class="c-sha">{c.short_sha}</div>
            <div class="c-sub">{c.subject}</div>
            <div class="c-meta">{c.author} · {fmtDate(c.date)}</div>
          </button>
        </li>
      {/each}
      {#if commits.length === 0 && !loading}
        <li class="empty muted">No commits for this file.</li>
      {/if}
    </ul>
  </div>

  {#snippet footer()}
    <div class="footer-info">
      <span>Selection: <strong>{selectionLabel()}</strong></span>
      <span>Revision: <strong>{revisionLabel()}</strong></span>
      {#if selStart !== null}
        <button type="button" class="link" onclick={clearSelection}>clear</button>
      {/if}
    </div>
    <button onclick={onClose} disabled={saving}>Cancel</button>
    <button
      class="primary"
      onclick={save}
      disabled={saving || !canSave}
      title={canSave ? '' : 'Selected lines have no content'}
    >
      {saving ? 'Saving…' : 'Drop anchor'}
    </button>
  {/snippet}
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    min-height: 400px;
    max-height: 60vh;
  }
  .code-view {
    display: grid;
    grid-template-columns: 48px 1fr;
    border: 1px solid var(--p-333333);
    overflow: auto;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    line-height: 1.5;
  }
  .gutter {
    display: flex;
    flex-direction: column;
    background: var(--p-0a0a0a);
    border-right: 1px solid var(--p-262626);
    color: var(--p-777777);
    user-select: none;
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
  .hint-bar {
    background: var(--p-1a2a38);
    color: var(--p-99ccff);
    border-bottom: 1px solid var(--p-2d5578);
    padding: 6px 10px;
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
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    color: var(--p-99ccff);
  }
  .code-pane {
    overflow-x: auto;
    background: var(--code-bg); /* themed code canvas, flips light/dark */
  }
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

  .commits {
    margin-top: 12px;
    border-top: 1px solid var(--p-333333);
    padding-top: 8px;
    max-height: 220px;
    overflow-y: auto;
  }
  .commits-header {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 4px;
  }
  .commits ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    gap: 4px;
    overflow-x: auto;
  }
  .commits li button {
    background: var(--p-111111);
    border: 1px solid var(--p-262626);
    color: var(--p-aaaaaa);
    padding: 4px 6px;
    font-size: 11px;
    font-family: ui-monospace, monospace;
    min-width: 150px;
    max-width: 220px;
    text-align: left;
    cursor: pointer;
    border-radius: 3px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .commits li button:hover { background: var(--p-1a1a1a); color: var(--p-ffffff); }
  .commits li button.on {
    background: var(--p-1e3a52);
    border-color: var(--p-2d5578);
    color: var(--p-99ccff);
  }
  .c-sha { font-weight: 600; }
  .c-sub {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-weight: normal;
  }
  .c-meta { font-size: 10px; color: var(--p-666666); }
  .commits .empty {
    color: var(--p-666666);
    font-style: italic;
    padding: 4px;
  }

  .footer-info {
    flex: 1;
    display: flex;
    gap: 12px;
    align-items: center;
    font-size: 11px;
    color: var(--p-aaaaaa);
    padding-right: 8px;
  }
  .footer-info strong { color: var(--p-99ccff); font-weight: 500; }
  .link {
    background: transparent;
    border: none;
    color: var(--p-666666);
    font-size: 11px;
    padding: 0 4px;
    cursor: pointer;
    text-decoration: underline;
  }
  .link:hover { color: var(--p-99ccff); }
  .muted { color: var(--p-777777); font-size: 12px; padding: 8px; }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 6px;
    margin-bottom: 8px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
  }
  .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .primary:hover:not(:disabled) { background: var(--p-2d5578); }
</style>
