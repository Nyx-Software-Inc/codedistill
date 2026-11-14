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
  // One file of a changeset in the code pane: the whole file as it reads at
  // head, with what left it still visible — the single-file diff mode of
  // docs/design/code-review.md §1. The rows are DiffTable's; this component
  // owns the fetch, the banner, the "not in this changeset" case and the
  // scroll.
  //
  // When the file is not in the changeset, the view shows it whole as
  // context AND lists the files that did change, each one a click away. You
  // picked the changeset to see what it changed; the pane should not answer a
  // question you did not ask and stop there.
  import * as api from '../lib/api';
  import type { FileDiff } from '../lib/api';
  import DiffTable from './DiffTable.svelte';
  import { ReviewThreads, reviewVersion } from '../lib/reviewComments.svelte';

  type Props = {
    projectId: string;
    path: string;
    base: string;
    head: string; // full sha or "working"
    /** Open another file of the same changeset as a diff tab. */
    onOpenFile?: (path: string) => void;
    /** Open the whole changeset in its pane. */
    onOpenChangeset?: () => void;
    /** Open the bug or todo a comment was promoted to. */
    onOpenItem?: (ownerType: 'bug_item' | 'todo_item', ownerId: string) => void;
  };
  let { projectId, path, base, head, onOpenFile, onOpenChangeset, onOpenItem }: Props = $props();

  // Comments on this file of this changeset (slice 4).
  const threads = new ReviewThreads();
  const review = threads.actions((t, id) => onOpenItem?.(t, id));
  $effect(() => {
    void reviewVersion(); // any host's edit reloads this one
    void threads.load(projectId, base, head);
  });

  let fd = $state<FileDiff | null>(null);
  /** The changeset's changed files, fetched only when this file is not among them. */
  let others = $state<FileDiff[] | null>(null);
  let loading = $state(false);
  let err = $state('');
  let root = $state<HTMLDivElement | null>(null);

  let loadedKey = '';
  let seq = 0;
  $effect(() => {
    const key = `${projectId}|${path}|${base}|${head}`;
    if (key === loadedKey) return;
    loadedKey = key;
    void load();
  });

  async function load() {
    if (!projectId || !path) return;
    const my = ++seq;
    loading = true;
    err = '';
    others = null;
    try {
      const cs = await api.changesetDiff(projectId, { base, head, path, context: 'full' });
      if (my !== seq) return;
      let f: FileDiff | null = cs.files[0] ?? null;
      if (!f) {
        // Not in the changeset: unchanged between base and head. Show it whole
        // as it reads at head, so "nothing changed here" is something you can
        // see, and fetch what did change so the pick still leads somewhere.
        const [fc, list] = await Promise.all([
          api.fileContent(projectId, path, head === api.WORKING ? '' : head),
          api.changesetDiff(projectId, { base, head, context: 0 }),
        ]);
        if (my !== seq) return;
        const lines = fc.binary ? [] : fc.content.split('\n');
        if (lines.length && lines[lines.length - 1] === '') lines.pop();
        f = {
          path, status: 'unchanged', binary: fc.binary, too_large: false, generated: false,
          added: 0, deleted: 0,
          hunks: fc.binary ? null : [{
            old_start: 1, old_lines: lines.length, new_start: 1, new_lines: lines.length,
            lines: lines.map((text, i) => ({ op: 'ctx' as const, old: i + 1, new: i + 1, text })),
          }],
        };
        others = list.files;
      }
      fd = f;
    } catch (e) {
      if (my !== seq) return;
      err = String(e);
    } finally {
      if (my === seq) loading = false;
    }
  }

  // Land on the first change: a reviewer opens a diff to see what moved,
  // not the package clause. Runs once the rows are in the DOM.
  let landed = '';
  function onRendered() {
    if (landed === loadedKey) return;
    landed = loadedKey;
    root?.querySelector('tr.add, tr.del')?.scrollIntoView({ block: 'center' });
  }

  function shortSha(s: string): string { return s.length > 7 ? s.slice(0, 7) : s; }
  function fileName(p: string): string {
    const i = p.lastIndexOf('/');
    return i >= 0 ? p.slice(i + 1) : p;
  }
  function dirName(p: string): string {
    const i = p.lastIndexOf('/');
    return i >= 0 ? p.slice(0, i + 1) : '';
  }
  const headLabel = $derived(head === api.WORKING ? 'working copy' : shortSha(head));
  const baseLabel = $derived(base ? shortSha(base) : 'parent');
</script>

<div class="diff" bind:this={root}>
  <div class="banner">
    <span class="kind">diff</span>
    <span class="sha">{baseLabel}</span>
    <span class="arrow" aria-hidden="true">→</span>
    <span class="sha">{headLabel}</span>
    {#if fd}
      <span class="counts"><span class="add">+{fd.added}</span> <span class="del">−{fd.deleted}</span></span>
      <span class="status">{fd.status}</span>
    {/if}
  </div>
  {#if threads.error}
    <div class="err">{threads.error}</div>
  {/if}
  {#if err}
    <div class="err">{err}</div>
  {:else if loading && !fd}
    <div class="muted">Loading…</div>
  {:else if fd?.binary}
    <div class="muted">Binary file — no line diff.</div>
  {:else if fd?.too_large}
    <div class="muted">Too large to diff line by line.</div>
  {:else if fd && (fd.hunks ?? []).length === 0}
    <div class="muted">Empty file.</div>
  {:else if fd}
    {#if fd.status === 'unchanged'}
      <div class="note">
        <div class="note-head">
          <span>This file did not change between {baseLabel} and {headLabel}. Shown as it reads at {headLabel}.</span>
          {#if onOpenChangeset && others && others.length > 0}
            <button type="button" class="link" onclick={onOpenChangeset}>Show the whole changeset</button>
          {/if}
        </div>
        {#if others && others.length > 0}
          <div class="changed">
            <span class="changed-title">{others.length} file{others.length === 1 ? '' : 's'} changed:</span>
            {#each others as o (o.path)}
              <button
                type="button"
                class="file"
                class:generated={o.generated}
                disabled={!onOpenFile}
                title={o.generated ? `${o.path} (generated)` : o.path}
                onclick={() => onOpenFile?.(o.path)}
              ><span class="dir">{dirName(o.path)}</span><span class="name">{fileName(o.path)}</span>
                <span class="counts"><span class="add">+{o.added}</span> <span class="del">−{o.deleted}</span></span></button>
            {/each}
          </div>
        {:else if others}
          <div class="changed"><span class="changed-title">Nothing changed in this changeset.</span></div>
        {/if}
      </div>
    {/if}
    <div class="code-view">
      <DiffTable path={fd.path} hunks={fd.hunks ?? []} {onRendered} comments={threads.forPath(fd.path)} review={threads.changeset ? review : null} />
    </div>
  {/if}
</div>

<style>
  .diff {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--code-bg);
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
  .banner .kind { text-transform: uppercase; letter-spacing: 0.5px; font-size: 10px; opacity: 0.7; }
  .banner .sha { color: var(--p-99ccff); }
  .banner .arrow { opacity: 0.6; }
  .banner .counts { margin-left: auto; }
  .add { color: var(--diff-add-fg); }
  .del { color: var(--diff-del-fg); }
  .banner .status { opacity: 0.7; }
  .code-view {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }
  .muted { color: var(--p-777777); font-size: 12px; padding: 12px; }
  .note {
    flex-shrink: 0;
    background: var(--p-161616);
    color: var(--p-aaaaaa);
    border-bottom: 1px solid var(--p-262626);
    padding: 6px 10px;
    font-size: 11px;
    max-height: 40%;
    overflow: auto;
  }
  .note-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
  .link {
    background: transparent; border: none; color: var(--p-99ccff); cursor: pointer;
    font-size: 11px; padding: 0; text-decoration: underline; text-underline-offset: 2px;
  }
  .link:hover { color: var(--p-ffffff); }
  .changed { display: flex; flex-direction: column; gap: 1px; margin-top: 6px; }
  .changed-title { color: var(--p-888888); margin-bottom: 2px; }
  .file {
    display: flex; align-items: baseline; gap: 0; min-width: 0;
    background: transparent; border: none; color: var(--p-dddddd); cursor: pointer;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    font-size: 11px; padding: 2px 6px; border-radius: 4px; text-align: left;
  }
  .file:hover { background: var(--p-1e3a52); color: var(--p-ffffff); }
  .file:disabled { cursor: default; }
  .file .dir { color: var(--p-888888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .file .name { white-space: nowrap; }
  .file .counts { margin-left: auto; padding-left: 10px; flex-shrink: 0; }
  .file.generated .name, .file.generated .dir { opacity: 0.55; }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 8px 12px;
    background: var(--p-2a1414);
    border-bottom: 1px solid var(--p-4a2020);
  }
</style>
