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
  // The changeset pane (docs/design/code-review.md §2): every file a
  // changeset touched, one after another, each under a header you cannot
  // miss, hunks at 3 lines of context with expand. A pane of its own because
  // cross-file reading wants width and is a different act from reading one
  // file; the code pane keeps the single-file mode.
  //
  // Expanding: the server shapes the 3-line hunks. The first expand on a file
  // fetches it once at full context, after which lib/hunks.ts shapes windows
  // here — the initial windows reproduce the server's hunks exactly, so
  // expanding is a strict superset of what was on screen.
  //
  // Generated and vendored files are collapsed, never hidden: the list says
  // the churn was set aside, and one click shows it.
  import * as api from '../lib/api';
  import type { Changeset, DiffHunk, DiffLine, FileDiff } from '../lib/api';
  import {
    windowsFor, expandUp, expandDown, expandAll, hunksFor, hasGapAbove, hasGapBelow, type Window,
  } from '../lib/hunks';
  import ChangesetPicker, { type ChangesetPick } from './ChangesetPicker.svelte';
  import DiffTable from './DiffTable.svelte';
  import ChangesetReviewBar from './ChangesetReviewBar.svelte';
  import Icon from './Icon.svelte';
  import { ReviewThreads, reviewVersion } from '../lib/reviewComments.svelte';

  type Props = {
    projectId: string;
    /** The changeset showing; App owns it so the code pane's picks land here too. */
    changeset: ChangesetPick | null;
    onPick: (cs: ChangesetPick) => void;
    /** Open one file of the changeset in the code pane's diff mode. */
    onOpenFile: (path: string) => void;
    /** Open the bug or todo a comment was promoted to. */
    onOpenItem: (ownerType: 'bug_item' | 'todo_item', ownerId: string) => void;
    /** Bumped by App on its data cycle so the review bar follows decisions
     *  made in Needs review. */
    tick?: number;
    /** After a review action here, so App refreshes Needs review. */
    onReviewChanged?: () => void;
  };
  let { projectId, changeset, onPick, onOpenFile, onOpenItem, tick = 0, onReviewChanged }: Props = $props();

  // The comments of the showing changeset (slice 4). Loaded alongside the
  // diff; the tables get their file's slice and the actions.
  const threads = new ReviewThreads();
  const review = threads.actions((t, id) => onOpenItem(t, id));
  $effect(() => {
    void reviewVersion(); // any host's edit reloads this one
    if (changeset) void threads.load(projectId, changeset.base, changeset.head, changeset.kind, changeset.ref);
    else threads.reset();
  });

  const CONTEXT = 3;
  const EXPAND_BY = 10;

  type FileState = {
    collapsed: boolean;
    /** Every line at full context, once fetched; windows index into it. */
    full: DiffLine[] | null;
    windows: Window[] | null;
    /** What is rendered: the server's hunks until the first expand. */
    hunks: DiffHunk[];
    busy: boolean;
  };

  let cs = $state<Changeset | null>(null);
  let files = $state<Record<string, FileState>>({});
  let loading = $state(false);
  let err = $state('');
  let pickerOpen = $state(false);
  let pickBtn = $state<HTMLButtonElement | null>(null);

  let loadedKey = '';
  let seq = 0;
  $effect(() => {
    const key = changeset ? `${projectId}|${changeset.base}|${changeset.head}` : `${projectId}|`;
    if (key === loadedKey) return;
    loadedKey = key;
    void load();
  });

  async function load() {
    const my = ++seq;
    cs = null;
    files = {};
    err = '';
    if (!projectId || !changeset) return;
    loading = true;
    try {
      const r = await api.changesetDiff(projectId, { base: changeset.base, head: changeset.head, context: CONTEXT });
      if (my !== seq) return;
      const next: Record<string, FileState> = {};
      for (const f of r.files) {
        next[f.path] = {
          collapsed: f.generated || f.binary || f.too_large,
          full: null,
          windows: null,
          hunks: f.hunks ?? [],
          busy: false,
        };
      }
      files = next;
      cs = r;
    } catch (e) {
      if (my !== seq) return;
      err = String(e);
    } finally {
      if (my === seq) loading = false;
    }
  }

  const totals = $derived.by(() => {
    let added = 0;
    let deleted = 0;
    for (const f of cs?.files ?? []) { added += f.added; deleted += f.deleted; }
    return { added, deleted, files: cs?.files.length ?? 0 };
  });

  async function toggle(f: FileDiff) {
    const st = files[f.path];
    if (!st) return;
    if (st.collapsed && st.hunks.length === 0 && f.generated && !f.binary && !f.too_large && changeset) {
      // A generated file comes back without hunks in a multi-file diff; the
      // single-file fetch is the only way to get them.
      st.busy = true;
      try {
        const r = await api.changesetDiff(projectId, { base: changeset.base, head: changeset.head, path: f.path, context: CONTEXT });
        st.hunks = r.files[0]?.hunks ?? [];
      } catch (e) {
        err = String(e);
      } finally {
        st.busy = false;
      }
    }
    st.collapsed = !st.collapsed;
  }

  /** Make sure the file's full line list and current windows are in hand. */
  async function ensureFull(path: string): Promise<FileState | null> {
    const st = files[path];
    if (!st || !changeset) return null;
    if (st.full) return st;
    st.busy = true;
    try {
      const r = await api.changesetDiff(projectId, { base: changeset.base, head: changeset.head, path, context: 'full' });
      const full = r.files[0]?.hunks?.[0]?.lines ?? [];
      st.full = full;
      st.windows = windowsFor(full, CONTEXT);
      return st;
    } catch (e) {
      err = String(e);
      return null;
    } finally {
      st.busy = false;
    }
  }

  function apply(st: FileState, ws: Window[]) {
    st.windows = ws;
    st.hunks = hunksFor(st.full ?? [], ws);
  }

  async function up(path: string, i: number) {
    const st = await ensureFull(path);
    if (st?.full && st.windows) apply(st, expandUp(st.windows, i, EXPAND_BY));
  }
  async function down(path: string, i: number) {
    const st = await ensureFull(path);
    if (st?.full && st.windows) apply(st, expandDown(st.windows, i, EXPAND_BY, st.full.length));
  }
  async function all(path: string) {
    const st = await ensureFull(path);
    if (st?.full) apply(st, expandAll(st.full.length));
  }

  // Before the full fetch, the server's hunks tell us what we can: a first
  // hunk not starting at line 1 has lines above; between hunks there is
  // always a gap (touching hunks are merged); below the last one, unknown
  // until fetched, so offer it.
  function above(path: string, i: number): boolean {
    const st = files[path];
    if (!st) return false;
    if (st.windows) return hasGapAbove(st.windows, i);
    if (i > 0) return true;
    const h = st.hunks[0];
    return !!h && (h.old_start > 1 || h.new_start > 1);
  }
  function below(path: string): boolean {
    const st = files[path];
    if (!st) return false;
    if (st.windows && st.full) return hasGapBelow(st.windows, st.full.length);
    return true;
  }
  function expandFor(path: string) {
    return {
      above: (i: number) => above(path, i),
      below: () => below(path),
      up: (i: number) => void up(path, i),
      down: (i: number) => void down(path, i),
      all: () => void all(path),
    };
  }

  function statusWord(f: FileDiff): string {
    if (f.status === 'renamed' && f.old_path) return `renamed from ${f.old_path}`;
    return f.status;
  }
  function fileName(p: string): string {
    const i = p.lastIndexOf('/');
    return i >= 0 ? p.slice(i + 1) : p;
  }
  function dirName(p: string): string {
    const i = p.lastIndexOf('/');
    return i >= 0 ? p.slice(0, i + 1) : '';
  }
</script>

<div class="cs">
  <div class="bar">
    <button
      type="button"
      class="pick"
      bind:this={pickBtn}
      class:on={pickerOpen}
      title="Pick a branch, commit or the working copy"
      aria-expanded={pickerOpen}
      onclick={() => (pickerOpen = !pickerOpen)}
    ><Icon name="branch" size={13} /><span class="pick-label">{changeset?.label ?? 'Pick a changeset…'}</span><span class="caret" aria-hidden="true">▾</span></button>
    {#if cs}
      <span class="totals">
        {totals.files} file{totals.files === 1 ? '' : 's'}
        <span class="add">+{totals.added}</span>
        <span class="del">−{totals.deleted}</span>
        {#if threads.unresolved > 0}<span class="cc" title="Unresolved comments">💬 {threads.unresolved}</span>{/if}
      </span>
    {/if}
    {#if pickerOpen}
      <ChangesetPicker {projectId} anchor={pickBtn} onPick={(c) => { pickerOpen = false; onPick(c); }} onClose={() => (pickerOpen = false)} />
    {/if}
  </div>

  <ChangesetReviewBar {projectId} changesetId={threads.changeset?.id ?? null} {tick} onChanged={onReviewChanged} />

  <div class="body">
    {#if err}
      <div class="err">{err}</div>
    {/if}
    {#if threads.error}
      <div class="err">{threads.error}</div>
    {/if}
    {#if !projectId}
      <div class="muted">Open a project to review its changes.</div>
    {:else if !changeset}
      <div class="muted">Nothing picked. Choose a branch, a commit or the working copy above, and every file it changed lands here.</div>
    {:else if loading && !cs}
      <div class="muted">Loading {changeset.label}…</div>
    {:else if cs && cs.files.length === 0}
      <div class="muted">No changes in {changeset.label}.</div>
    {:else if cs}
      {#if cs.truncated}
        <div class="note">This changeset is large; not every file is listed.</div>
      {/if}
      {#each cs.files as f (f.path)}
        {@const st = files[f.path]}
        <section class="file" class:collapsed={st?.collapsed} class:generated={f.generated}>
          <header class="fh">
            <button type="button" class="tog" aria-label={st?.collapsed ? 'Expand file' : 'Collapse file'} onclick={() => void toggle(f)}>{st?.collapsed ? '▸' : '▾'}</button>
            <span class="path" title={f.path}><span class="dir">{dirName(f.path)}</span><span class="name">{fileName(f.path)}</span></span>
            <span class="chip {f.status}">{statusWord(f)}</span>
            {#if f.generated}<span class="chip gen">generated</span>{/if}
            {#if f.binary}<span class="chip">binary</span>{/if}
            {#if f.too_large}<span class="chip">too large</span>{/if}
            <span class="counts"><span class="add">+{f.added}</span> <span class="del">−{f.deleted}</span></span>
            {#if threads.countFor(f.path) > 0}<span class="cc" title="Unresolved comments on this file">💬 {threads.countFor(f.path)}</span>{/if}
            {#if st?.busy}<span class="busy">…</span>{/if}
            <button type="button" class="open" title="Open this file in the code pane, diff marked" onclick={() => onOpenFile(f.path)}><Icon name="diff" size={12} /></button>
          </header>
          {#if st && !st.collapsed}
            {#if f.binary}
              <div class="muted small">Binary file — no line diff.</div>
            {:else if f.too_large}
              <div class="muted small">Too large to diff line by line.</div>
            {:else if st.hunks.length === 0}
              <div class="muted small">No line changes.</div>
            {:else}
              <DiffTable path={f.path} hunks={st.hunks} headers expand={expandFor(f.path)} comments={threads.forPath(f.path)} review={threads.changeset ? review : null} />
            {/if}
          {/if}
        </section>
      {/each}
    {/if}
  </div>
</div>

<style>
  .cs {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--code-bg);
    container-type: inline-size;
  }
  .bar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 8px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    font-size: 11px;
    color: var(--p-aaaaaa);
  }
  .pick {
    display: inline-flex; align-items: center; gap: 6px; min-width: 0; max-width: 70%;
    background: transparent; border: 1px solid var(--p-333333); border-radius: 5px;
    color: var(--p-dddddd); padding: 3px 8px; font-size: 11px; cursor: pointer;
  }
  .pick:hover { border-color: var(--p-99ccff); color: var(--p-ffffff); }
  .pick.on { background: var(--p-1e3a52); border-color: var(--p-99ccff); }
  .pick-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .caret { opacity: 0.6; }
  .totals { margin-left: auto; white-space: nowrap; font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace); }
  .add { color: var(--diff-add-fg); }
  .del { color: var(--diff-del-fg); }
  .body { flex: 1; min-height: 0; overflow: auto; }
  .muted { color: var(--p-777777); font-size: 12px; padding: 14px 12px; }
  .muted.small { font-size: 11px; padding: 6px 12px; }
  .note { font-size: 11px; color: var(--p-d4a54d); padding: 4px 12px; background: var(--p-161616); border-bottom: 1px solid var(--p-262626); }
  .err { color: var(--p-ff8888); font-size: 12px; padding: 8px 12px; background: var(--p-2a1414); border-bottom: 1px solid var(--p-4a2020); }

  /* The break between files you cannot miss: a thick rule and a header that
     stays put while its hunks scroll under it. */
  .file { border-top: 6px solid var(--p-1a1a1a); }
  .file:first-of-type { border-top: none; }
  .fh {
    position: sticky; top: 0; z-index: 2;
    display: flex; align-items: center; gap: 8px; min-width: 0;
    padding: 5px 8px;
    background: var(--p-161616);
    border-bottom: 1px solid var(--p-2a2a2a);
    font-size: 11px;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    color: var(--p-dddddd);
  }
  .file.collapsed .fh { border-bottom: none; }
  .file.generated .fh .path { opacity: 0.6; }
  .tog { background: transparent; border: none; color: var(--p-888888); cursor: pointer; padding: 0 2px; font-size: 11px; width: 14px; }
  .tog:hover { color: var(--p-ffffff); }
  .path { display: inline-flex; min-width: 0; flex: 1; align-items: baseline; }
  .path .dir { color: var(--p-888888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .path .name { white-space: nowrap; font-weight: 600; }
  .chip {
    font-size: 10px; padding: 0 6px; border-radius: 9px; white-space: nowrap;
    background: var(--p-262626); color: var(--p-aaaaaa); font-family: inherit;
    max-width: 240px; overflow: hidden; text-overflow: ellipsis;
  }
  .chip.added { color: var(--diff-add-fg); background: var(--diff-add-gutter); }
  .chip.deleted { color: var(--diff-del-fg); background: var(--diff-del-gutter); }
  .chip.renamed { color: var(--p-d4a54d); }
  .chip.gen { color: var(--p-888888); border: 1px dashed var(--p-444444); background: transparent; }
  .counts { white-space: nowrap; margin-left: 4px; }
  .cc { white-space: nowrap; color: var(--p-99ccff); font-size: 11px; }
  .busy { color: var(--p-888888); }
  .open {
    background: transparent; border: none; color: var(--p-888888); cursor: pointer;
    display: inline-flex; align-items: center; padding: 2px 4px; border-radius: 3px;
  }
  .open:hover { color: var(--p-ffffff); background: var(--p-1e3a52); }
  @container (max-width: 520px) {
    .chip.modified, .chip.renamed { display: none; }
  }
</style>
