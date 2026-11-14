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
  // A right-click menu rendered from the command registry: the caller says
  // where (x, y) and which command ids; labels, shortcuts, visibility and
  // enablement come from the registry, so a context menu never disagrees
  // with the toolbar or the keyboard (docs/design/commands.md).
  import { commandsById, runCommand, isVisible, isEnabled, shortcutLabel } from '../lib/commands.svelte';

  type Props = {
    x: number;
    y: number;
    ids: readonly string[];
    onClose: () => void;
  };
  let { x, y, ids, onClose }: Props = $props();

  const items = $derived(commandsById(ids).filter(isVisible));
  // Mounted synchronously inside the contextmenu handler that opened it, so
  // that same event reaches the window listener below; anything older than
  // the mount is the opening gesture, not a dismissal.
  const openedAt = performance.now();
  let root: HTMLDivElement;

  // Keep the menu on screen: flip it up/left when it would overflow.
  let left = $state(0);
  let top = $state(0);
  $effect(() => {
    left = x; top = y;
    queueMicrotask(() => {
      if (!root) return;
      const r = root.getBoundingClientRect();
      if (x + r.width > window.innerWidth - 8) left = Math.max(8, x - r.width);
      if (y + r.height > window.innerHeight - 8) top = Math.max(8, y - r.height);
    });
  });

  function pick(id: string) {
    onClose();
    runCommand(id);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { e.stopPropagation(); onClose(); }
  }
  function onWindowPointer(e: Event) {
    if (e.timeStamp <= openedAt) return;
    if (root && !root.contains(e.target as Node)) onClose();
  }
</script>

<svelte:window onmousedown={onWindowPointer} onkeydown={onKey} oncontextmenu={onWindowPointer} />

<div class="ctx" bind:this={root} role="menu" style:left="{left}px" style:top="{top}px">
  {#each items as cmd (cmd.id)}
    <button
      type="button"
      class="item"
      role="menuitem"
      disabled={!isEnabled(cmd)}
      onclick={() => pick(cmd.id)}
      title={cmd.description ?? ''}
    >
      <span class="lbl">{cmd.label}</span>
      {#if shortcutLabel(cmd)}<span class="kbd">{shortcutLabel(cmd)}</span>{/if}
    </button>
  {/each}
  {#if items.length === 0}
    <div class="empty">Nothing to do here.</div>
  {/if}
</div>

<style>
  .ctx {
    position: fixed;
    z-index: 400;
    min-width: 200px;
    padding: 4px;
    background: var(--p-161616);
    border: 1px solid var(--p-333333);
    border-radius: 6px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
    display: flex;
    flex-direction: column;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 16px;
    background: transparent;
    border: none;
    color: var(--p-dddddd);
    text-align: left;
    padding: 6px 10px;
    font-size: 12px;
    border-radius: 4px;
    cursor: pointer;
  }
  .item:hover:not(:disabled) { background: var(--p-252525); }
  .item:disabled { opacity: 0.45; cursor: default; }
  .item .lbl { flex: 1; }
  .item .kbd { color: var(--p-777777); font-size: 11px; }
  .empty { color: var(--p-777777); font-size: 12px; padding: 6px 10px; }
</style>
