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
  import type { TreeNode } from '../lib/files';
  import { iconForFile } from '../lib/files';
  import Self from './FileTree.svelte';

  type Props = {
    nodes: TreeNode[];
    // Set of folder paths the user has expanded.
    expanded: Set<string>;
    // When true, every folder is rendered open regardless of `expanded`.
    // Used while a search filter is active so matches are always visible.
    forceExpand: boolean;
    onToggle: (folderPath: string) => void;
    onDragStart: (e: DragEvent, path: string) => void;
    onFileClick: (path: string) => void;
    // Indent level for nested calls. Top-level callers omit this; the
    // recursive call below increments it by one.
    depth?: number;
  };
  let {
    nodes,
    expanded,
    forceExpand,
    onToggle,
    onDragStart,
    onFileClick,
    depth = 0,
  }: Props = $props();

  function isOpen(node: TreeNode): boolean {
    return forceExpand || expanded.has(node.path);
  }
  // 12px per depth level + a 6px gutter so depth-0 rows still have left padding.
  function indentPx(d: number): string {
    return d * 12 + 6 + 'px';
  }
</script>

<ul class="tree">
  {#each nodes as node (node.path)}
    {#if node.isDir}
      {@const open = isOpen(node)}
      <li>
        <button
          class="row folder"
          style:padding-left={indentPx(depth)}
          onclick={() => onToggle(node.path)}
          aria-expanded={open}
          title={node.path}
        >
          <span class="caret">{open ? '▾' : '▸'}</span>
          <span class="icon">{open ? '📂' : '📁'}</span>
          <span class="name">{node.name}</span>
        </button>
        {#if open && node.children.length > 0}
          <Self
            nodes={node.children}
            {expanded}
            {forceExpand}
            {onToggle}
            {onDragStart}
            {onFileClick}
            depth={depth + 1}
          />
        {/if}
      </li>
    {:else}
      <!-- File rows: click opens in the code canvas; drag drops onto the
           scratchpad canvas (handled by ScratchpadPane via dataTransfer). -->
      <li>
        <button
          type="button"
          class="row file"
          style:padding-left={indentPx(depth)}
          draggable="true"
          ondragstart={(e) => onDragStart(e, node.path)}
          onclick={() => onFileClick(node.path)}
          title={node.path}
        >
          <span class="caret"></span>
          <span class="icon mono">{iconForFile(node.name)}</span>
          <span class="name">{node.name}</span>
        </button>
      </li>
    {/if}
  {/each}
</ul>

<style>
  .tree {
    list-style: none;
    margin: 0;
    padding: 0;
    font-family: var(--code-font-family, ui-monospace, SFMono-Regular, Consolas, monospace);
    font-size: var(--code-font-size, 11px);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 4px;
    width: 100%;
    background: transparent;
    border: none;
    color: var(--p-bbbbbb);
    padding-top: 2px;
    padding-bottom: 2px;
    padding-right: 6px;
    text-align: left;
    border-radius: 2px;
    overflow: hidden;
    line-height: 1.4;
  }
  .row.folder {
    cursor: pointer;
    font: inherit;
  }
  .row.file {
    cursor: pointer;
    font: inherit;
  }
  .row.file:active {
    cursor: grabbing;
  }
  .row:hover {
    background: var(--p-1a1a1a);
    color: var(--p-ffffff);
  }
  .caret {
    width: 10px;
    color: var(--p-555555);
    flex-shrink: 0;
    text-align: center;
  }
  .icon {
    width: 14px;
    flex-shrink: 0;
    text-align: center;
    font-size: 11px;
    line-height: 1;
  }
  .icon.mono {
    color: var(--p-66ccff);
    opacity: 0.7;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
