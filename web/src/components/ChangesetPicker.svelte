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
  // The changeset picker (docs/design/code-review.md §3): branches, then the
  // recent commits on the chosen branch, with the working copy on top. One
  // pick returns base..head in whichever shape was chosen:
  //   working copy  → base "" (HEAD), head "working"
  //   a branch      → base = merge-base with the default branch, head = tip
  //   a commit      → base "" (its parent), head = the sha
  // A popover: the caller positions it and owns open/close.
  import * as api from '../lib/api';
  import type { Branch, CommitInfo } from '../lib/api';
  import Icon from './Icon.svelte';

  export type ChangesetPick = {
    base: string;
    head: string;
    kind: 'working' | 'branch' | 'commit';
    label: string;
    /** The branch name, for a branch pick. */
    ref?: string;
  };

  type Props = {
    projectId: string;
    /** The element the popover hangs under. The pane can be narrower than
     *  the popover, so it is positioned against the viewport, not the pane. */
    anchor: HTMLElement | null;
    onPick: (cs: ChangesetPick) => void;
    onClose: () => void;
  };
  let { projectId, anchor, onPick, onClose }: Props = $props();

  const WIDTH = 420;
  // Svelte 5 flushes synchronously inside an event handler, so this popover
  // is mounted while the click that opened it is still propagating; the
  // window listener below would see that very click, outside the popover,
  // and close it in the same dispatch. Events older than the mount are not
  // ours to act on. (The profile menu never hit this: its trigger is inside
  // its own root.)
  const openedAt = performance.now();
  let left = $state(8);
  let top = $state(8);
  $effect(() => {
    const r = anchor?.getBoundingClientRect();
    const w = Math.min(WIDTH, window.innerWidth - 16);
    if (r) {
      // Right-align under the anchor, then keep it on screen.
      left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8));
      top = r.bottom + 4;
    }
  });

  let branches = $state<Branch[]>([]);
  let defaultBranch = $state('');
  let selected = $state('');
  let commits = $state<CommitInfo[]>([]);
  let loadingBranches = $state(true);
  let loadingCommits = $state(false);
  let err = $state('');
  let root: HTMLDivElement;

  $effect(() => {
    void projectId;
    void loadBranches();
  });

  async function loadBranches() {
    loadingBranches = true;
    err = '';
    try {
      const r = await api.listBranches(projectId);
      branches = r.branches;
      defaultBranch = r.default;
      const cur = r.branches.find((b) => b.is_current) ?? r.branches[0];
      if (cur) await selectBranch(cur.name);
    } catch (e) {
      err = String(e);
    } finally {
      loadingBranches = false;
    }
  }

  async function selectBranch(name: string) {
    selected = name;
    loadingCommits = true;
    try {
      commits = await api.listCommits(projectId, name, '', 50);
    } catch (e) {
      err = String(e);
      commits = [];
    } finally {
      loadingCommits = false;
    }
  }

  function pickWorking() {
    onPick({ base: '', head: api.WORKING, kind: 'working', label: 'working copy' });
  }

  async function pickBranch(b: Branch) {
    if (b.name === defaultBranch) {
      // The default branch against itself is nothing; its tip commit is the
      // sensible reading of "review main".
      onPick({ base: '', head: b.sha, kind: 'commit', label: `${b.name} tip` });
      return;
    }
    try {
      const mb = await api.mergeBase(projectId, b.name, defaultBranch);
      onPick({ base: mb.sha, head: b.sha, kind: 'branch', label: `${b.name} vs ${defaultBranch}`, ref: b.name });
    } catch (e) {
      err = String(e);
    }
  }

  function pickCommit(c: CommitInfo) {
    onPick({ base: '', head: c.sha, kind: 'commit', label: `${c.short_sha} ${c.subject}` });
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { e.stopPropagation(); onClose(); }
  }
  function onWindowClick(e: MouseEvent) {
    if (e.timeStamp <= openedAt) return;
    if (root && !root.contains(e.target as Node)) onClose();
  }
  function when(d: string): string {
    const t = new Date(d);
    return Number.isNaN(t.getTime()) ? '' : t.toLocaleDateString();
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKey} />

<div class="picker" bind:this={root} role="dialog" aria-label="Pick a changeset" style:left="{left}px" style:top="{top}px">
  {#if err}<div class="err">{err}</div>{/if}
  <button type="button" class="row working" onclick={pickWorking}>
    <Icon name="diff" size={13} /> <span class="lbl">Working copy</span><span class="hint">vs HEAD</span>
  </button>
  <div class="section">Branches</div>
  {#if loadingBranches}
    <div class="muted">Loading…</div>
  {:else}
    <div class="branches">
      {#each branches as b (b.name)}
        <div class="branch" class:selected={b.name === selected}>
          <button type="button" class="row" onclick={() => selectBranch(b.name)} title="Show commits on {b.name}">
            <Icon name="branch" size={13} />
            <span class="lbl">{b.name}</span>
            {#if b.is_default}<span class="tag">default</span>{/if}
            {#if b.is_current}<span class="tag">current</span>{/if}
            {#if b.remote}<span class="tag">remote</span>{/if}
          </button>
          <button
            type="button"
            class="vs"
            onclick={() => pickBranch(b)}
            title={b.name === defaultBranch ? `Review the tip of ${b.name}` : `Review everything on ${b.name} since it left ${defaultBranch}`}
          >{b.name === defaultBranch ? 'tip' : `vs ${defaultBranch}`}</button>
        </div>
      {/each}
    </div>
  {/if}
  <div class="section">Commits on {selected || '…'}</div>
  <div class="commits">
    {#if loadingCommits}
      <div class="muted">Loading…</div>
    {:else if commits.length === 0}
      <div class="muted">No commits.</div>
    {:else}
      {#each commits as c (c.sha)}
        <button type="button" class="row commit" onclick={() => pickCommit(c)} title="{c.sha}\n{c.author} · {when(c.date)}">
          <span class="sha">{c.short_sha}</span>
          <span class="lbl">{c.subject}</span>
          <span class="hint">{when(c.date)}</span>
        </button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .picker {
    position: fixed;
    z-index: 300;
    width: 420px;
    max-width: calc(100vw - 16px);
    max-height: min(70vh, 560px);
    display: flex;
    flex-direction: column;
    background: var(--p-161616);
    border: 1px solid var(--p-333333);
    border-radius: 6px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
    font-size: 12px;
    overflow: hidden;
  }
  .section {
    padding: 6px 10px 2px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-777777);
    border-top: 1px solid var(--p-262626);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
    background: transparent;
    border: none;
    color: var(--p-dddddd);
    text-align: left;
    padding: 5px 10px;
    cursor: pointer;
    border-radius: 0;
    font-size: 12px;
  }
  .row:hover { background: var(--p-252525); }
  .row .lbl { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .row .hint { color: var(--p-777777); font-size: 11px; }
  .row.working { padding-top: 8px; }
  .branches { max-height: 180px; overflow: auto; }
  .branch { display: flex; align-items: stretch; }
  .branch.selected .row { background: var(--p-1e3a52); color: var(--p-99ccff); }
  .branch .vs {
    flex-shrink: 0;
    background: transparent;
    border: none;
    border-left: 1px solid var(--p-262626);
    color: var(--p-99ccff);
    font-size: 11px;
    padding: 0 10px;
    cursor: pointer;
    border-radius: 0;
    white-space: nowrap;
  }
  .branch .vs:hover { background: var(--p-2d5578); color: var(--p-ffffff); }
  .tag {
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    color: var(--p-888888);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 0 4px;
  }
  .commits { flex: 1; overflow: auto; min-height: 80px; }
  .commit .sha {
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    color: var(--p-d4a54d);
    flex-shrink: 0;
  }
  .muted { color: var(--p-777777); padding: 6px 10px; }
  .err { color: var(--p-ff8888); padding: 6px 10px; background: var(--p-2a1414); }
</style>
