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
  import * as api from '../lib/api';
  import { buildTree, type TreeNode } from '../lib/files';
  import FileTree from './FileTree.svelte';

  type Props = {
    projectId: string;
    // Click → open in the code canvas. Drag is still handled separately
    // (the drop target on the scratchpad consumes the dataTransfer payload).
    onFileClick: (path: string) => void;
    // Bumped by App when the server publishes files.changed (the
    // code-index watcher saw the repo move). Any change reloads the tree.
    refreshTick?: number;
  };
  let { projectId, onFileClick, refreshTick = 0 }: Props = $props();

  // Folders the user has expanded. Persists across loads so navigation
  // doesn't snap back to a fully-collapsed tree on every refresh.
  let expanded = $state<Set<string>>(new Set());

  // Repo config state. The backend returns 409 when repo_root is unset; we
  // surface a small form in that case so the user can configure it inline.
  let repoRoot = $state<string>('');
  let paths = $state<string[]>([]);
  let loadErr = $state<string>('');
  let loading = $state(false);
  let needsConfig = $state(false);

  // Config form state.
  let configInput = $state('');
  let saving = $state(false);
  let saveErr = $state('');

  let currentProject = $state<string>('');
  $effect(() => {
    if (projectId && projectId !== currentProject) {
      currentProject = projectId;
      load();
    }
  });
  let lastTick = 0;
  $effect(() => {
    if (refreshTick !== lastTick) {
      lastTick = refreshTick;
      if (projectId && !loading) void load();
    }
  });

  async function load() {
    loading = true;
    loadErr = '';
    needsConfig = false;
    try {
      const p = await api.getProject(projectId);
      repoRoot = p.repo_root ?? '';
      if (!repoRoot) {
        needsConfig = true;
        paths = [];
        return;
      }
      try {
        const fileList = await api.listFiles(projectId);
        paths = fileList.paths;
        saveErr = '';
      } catch (e) {
        // repo_root IS set but listing failed (path typo, not a git
        // repo, permissions). Re-show the config form WITH the server's
        // reason and the current value prefilled — silently bouncing
        // back to an empty form looked like "the panel didn't update"
        // (Backlog bug #4).
        needsConfig = true;
        paths = [];
        configInput = repoRoot;
        saveErr = String(e);
      }
    } catch (e) {
      loadErr = String(e);
    } finally {
      loading = false;
    }
  }

  async function saveRepoRoot() {
    if (!configInput.trim() || saving) return;
    saving = true;
    saveErr = '';
    try {
      await api.updateProject(projectId, { repo_root: configInput.trim() });
      configInput = '';
      await load();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  let clearErr = $state('');
  async function clearRepoRoot() {
    clearErr = '';
    try {
      await api.updateProject(projectId, { repo_root: '' });
      // Collapse on repo change so the next repo starts clean.
      expanded = new Set();
      await load();
    } catch (e) {
      // saveErr renders only inside the edit form, which isn't shown here — a
      // failed disconnect leaves the connected view up, so surface it inline
      // next to the action instead (audit M26).
      clearErr = String(e);
    }
  }

  // Directory picker backed by GET /fs/browse — lets the user navigate
  // to the repo instead of typing an absolute path (local-only; the
  // server lists directory names, never file contents).
  let browsing = $state(false);
  let browsePath = $state('');
  let browseParent = $state<string | undefined>(undefined);
  let browseDirs = $state<api.FsBrowseDir[]>([]);
  let browseErr = $state('');

  async function browseTo(path?: string) {
    browseErr = '';
    try {
      const r = await api.fsBrowse(path);
      browsePath = r.path;
      browseParent = r.parent;
      browseDirs = r.dirs;
    } catch (e) {
      browseErr = String(e);
    }
  }
  function openBrowse() {
    browsing = true;
    void browseTo(configInput.trim() || undefined);
  }
  function pickDir(path: string) {
    configInput = path;
    browsing = false;
  }

  // Search filter (case-insensitive substring on full path).
  let filter = $state('');
  let filteredPaths = $derived.by(() => {
    if (!filter.trim()) return paths;
    const q = filter.toLowerCase();
    return paths.filter((p) => p.toLowerCase().includes(q));
  });
  let tree: TreeNode[] = $derived(buildTree(filteredPaths));
  // While a filter is active, force every folder open so matches are visible
  // regardless of the user's prior expansion choices.
  let isFiltering = $derived(filter.trim() !== '');

  function toggleFolder(folderPath: string) {
    const next = new Set(expanded);
    if (next.has(folderPath)) {
      next.delete(folderPath);
    } else {
      next.add(folderPath);
    }
    expanded = next;
  }

  // Drag payload: JSON string with project + path. The modal on the other
  // end reads this and opens with the matching file loaded.
  function onDragStart(e: DragEvent, path: string) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData(
      'application/x-codedistill-file',
      JSON.stringify({ project_id: projectId, path }),
    );
    // Also set a plain-text fallback so dropping on other targets degrades gracefully.
    e.dataTransfer.setData('text/plain', path);
  }
</script>

{#if needsConfig}
  <div class="config">
    <p class="muted">
      Configure the repository root for this project to enable code-aware file drops.
    </p>
    <input
      type="text"
      bind:value={configInput}
      placeholder="/absolute/path/to/git/worktree"
    />
    <div class="config-actions">
      <button
        class="primary"
        onclick={saveRepoRoot}
        disabled={saving || !configInput.trim()}
      >
        {saving ? 'Saving…' : 'Save'}
      </button>
      <button class="secondary" onclick={openBrowse}>Browse…</button>
    </div>
    {#if saveErr}<div class="err">{saveErr}</div>{/if}
    {#if browsing}
      <div class="browse">
        <div class="browse-path" title={browsePath}>{browsePath}</div>
        {#if browseErr}<div class="err">{browseErr}</div>{/if}
        <ul class="browse-list">
          {#if browseParent}
            <li>
              <button class="dir" onclick={() => browseTo(browseParent)}>↰ ..</button>
            </li>
          {/if}
          {#each browseDirs as d (d.name)}
            <li>
              <button class="dir" onclick={() => browseTo(browsePath + '/' + d.name)}>
                📁 {d.name}{#if d.is_git_repo}<span class="git-flag" title="git repository"> ⎇</span>{/if}
              </button>
              {#if d.is_git_repo}
                <button class="use" onclick={() => pickDir(browsePath + '/' + d.name)}>use</button>
              {/if}
            </li>
          {/each}
          {#if browseDirs.length === 0 && !browseErr}
            <li class="muted">No subdirectories.</li>
          {/if}
        </ul>
        <div class="config-actions">
          <button class="secondary" onclick={() => pickDir(browsePath)}>Use this directory</button>
          <button class="link" onclick={() => (browsing = false)}>close</button>
        </div>
      </div>
    {/if}
  </div>
{:else if loading && paths.length === 0}
  <p class="muted">Loading…</p>
{:else if loadErr}
  <p class="err">{loadErr}</p>
{:else}
  <div class="toolbar">
    <input
      type="text"
      bind:value={filter}
      placeholder="Filter files…"
      class="filter"
    />
    <button class="link" onclick={() => load()} title="Refresh file tree" aria-label="Refresh file tree">↻</button>
    <button class="link" onclick={clearRepoRoot} title="Disconnect this repo">change</button>
    {#if clearErr}<span class="err" title={clearErr}>disconnect failed</span>{/if}
  </div>
  <div class="root muted" title={repoRoot}>{repoRoot}</div>
  {#if paths.length === 0}
    <div class="empty">
      No tracked files at HEAD — commit files in this repo and refresh (↻) to see them here.
    </div>
  {:else if filteredPaths.length === 0}
    <div class="empty">No files match.</div>
  {:else}
    <FileTree
      nodes={tree}
      {expanded}
      forceExpand={isFiltering}
      onToggle={toggleFolder}
      {onDragStart}
      {onFileClick}
    />
  {/if}
{/if}

<style>
  .config {
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .config input {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: ui-monospace, monospace;
  }
  .config .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
    align-self: flex-start;
  }
  .config .primary:hover:not(:disabled) { background: var(--p-2d5578); }
  .toolbar {
    display: flex;
    gap: 6px;
    align-items: center;
    padding: 4px 0 2px;
  }
  .filter {
    flex: 1;
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-262626);
    padding: 4px 6px;
    font-size: 12px;
    font-family: ui-monospace, monospace;
  }
  .link {
    background: transparent;
    border: none;
    color: var(--p-666666);
    font-size: 11px;
    padding: 2px 4px;
    cursor: pointer;
  }
  .link:hover { color: var(--p-99ccff); }
  .root {
    font-size: 10px;
    padding: 2px 4px 6px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty {
    color: var(--p-666666);
    font-size: 11px;
    font-style: italic;
    padding: 8px 6px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  }
  .muted { color: var(--p-777777); font-size: 11px; }
  .config-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .config .secondary {
    background: var(--p-1a1a1a);
    color: var(--p-bbbbbb);
    border: 1px solid var(--p-333333);
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .config .secondary:hover { background: var(--p-262626); color: var(--p-eeeeee); }
  .browse {
    border: 1px solid var(--p-262626);
    border-radius: 3px;
    padding: 6px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .browse-path {
    font-size: 10px;
    color: var(--p-99ccff);
    font-family: ui-monospace, monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .browse-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 220px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }
  .browse-list li {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .browse-list .dir {
    flex: 1;
    text-align: left;
    background: transparent;
    border: none;
    color: var(--p-cccccc);
    font-size: 12px;
    padding: 3px 4px;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .browse-list .dir:hover { background: var(--p-1a1a1a); color: var(--p-ffffff); }
  .git-flag { color: var(--p-99cc99); }
  .browse-list .use {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    font-size: 10px;
    padding: 1px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .browse-list .use:hover { background: var(--p-2d5578); }
  .err { color: var(--p-ff8888); font-size: 12px; }
</style>
