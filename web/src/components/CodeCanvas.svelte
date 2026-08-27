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
  import CodeViewer from './CodeViewer.svelte';
  import type { CodeAnchorWithOwner } from '../lib/types';

  // Multi-tab code viewer column. Conditional — App.svelte only renders
  // this when there's at least one open tab and the user hasn't hidden it.
  // Tab strip scrolls horizontally when many files are open.

  export type CodeTab = {
    path: string;
    revision: string; // "" = working copy
  };

  type Props = {
    projectId: string;
    tabs: CodeTab[];
    activeIndex: number;
    onActivate: (index: number) => void;
    onClose: (index: number) => void;
    onHide: () => void;
    onAnchorClick: (anchor: CodeAnchorWithOwner) => void;
    onCreateItem: (payload: {
      path: string;
      revision: string;
      lineStart: number;
      lineEnd: number;
      content: string;
    }) => Promise<void> | void;
    hasActiveScratchpad: boolean;
    // Highlight request from App: when set, the active CodeViewer scrolls
    // to + selects the requested line range. CodeViewer applies it once
    // its (path, revision) match, then calls onHighlightConsumed.
    pendingHighlight: {
      path: string;
      revision: string;
      lineStart: number;
      lineEnd: number;
    } | null;
    onHighlightConsumed: () => void;
    // Reverse-hover: a scratchpad-card anchor badge is being hovered.
    // Forwarded to the active CodeViewer which lights up the matching
    // line range when (path, revision) match.
    externalHover: import('../lib/types').CodeAnchor | null;
  };
  let {
    projectId, tabs, activeIndex,
    onActivate, onClose, onHide, onAnchorClick,
    onCreateItem, hasActiveScratchpad,
    pendingHighlight, onHighlightConsumed,
    externalHover,
  }: Props = $props();

  let active = $derived(
    activeIndex >= 0 && activeIndex < tabs.length ? tabs[activeIndex] : null,
  );

  // Auto-scroll the tab strip so the active tab is visible. Triggers on
  // both activate (user clicked) and open-from-canvas (reverse direction
  // dropped a new tab past the visible edge).
  let tabsEl: HTMLDivElement;
  $effect(() => {
    void activeIndex;
    void tabs.length;
    if (!tabsEl) return;
    queueMicrotask(() => {
      const el = tabsEl.querySelector('.tab.active') as HTMLElement | null;
      el?.scrollIntoView({ inline: 'nearest', block: 'nearest', behavior: 'smooth' });
    });
  });

  function tabLabel(t: CodeTab): string {
    const slash = t.path.lastIndexOf('/');
    return slash >= 0 ? t.path.slice(slash + 1) : t.path;
  }

  function tabTooltip(t: CodeTab): string {
    if (t.revision === '') return t.path;
    return `${t.path} @ ${t.revision.slice(0, 7)}`;
  }

  function closeTab(e: MouseEvent, index: number) {
    e.stopPropagation();
    onClose(index);
  }
</script>

<div class="canvas">
  <div class="tabstrip">
    <div class="tabs tab-scroll" bind:this={tabsEl}>
      {#each tabs as t, i (`${t.path}|${t.revision}`)}
        <div
          class="tab"
          class:active={i === activeIndex}
          class:historical={t.revision !== ''}
          title={tabTooltip(t)}
          role="tab"
          tabindex="0"
          aria-selected={i === activeIndex}
          onclick={() => onActivate(i)}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onActivate(i); } }}
        >
          <span class="label">{tabLabel(t)}</span>
          {#if t.revision !== ''}
            <span class="rev">@{t.revision.slice(0, 7)}</span>
          {/if}
          <button
            type="button"
            class="x"
            aria-label="Close tab"
            onclick={(e) => closeTab(e, i)}
          >×</button>
        </div>
      {/each}
    </div>
    <button
      type="button"
      class="hide"
      title="Hide code canvas"
      aria-label="Hide code canvas"
      onclick={onHide}
    >×</button>
  </div>

  <div class="body">
    {#if active}
      <CodeViewer
        {projectId}
        path={active.path}
        revision={active.revision}
        {onAnchorClick}
        {onCreateItem}
        {hasActiveScratchpad}
        {pendingHighlight}
        {onHighlightConsumed}
        {externalHover}
      />
    {:else}
      <div class="muted">No file open.</div>
    {/if}
  </div>
</div>

<style>
  .canvas {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--p-0d0d0d);
  }
  .tabstrip {
    flex-shrink: 0;
    display: flex;
    align-items: stretch;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    min-width: 0;
  }
  .tabs {
    flex: 1;
    display: flex;
    min-width: 0;
  }
  .tab {
    background: transparent;
    border: none;
    border-right: 1px solid var(--p-1e1e1e);
    border-bottom: 2px solid transparent;
    color: var(--p-aaaaaa);
    padding: 6px 4px 4px 10px;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    white-space: nowrap;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border-radius: 0;
    max-width: 240px;
    flex-shrink: 0;
  }
  .tab:hover { background: var(--p-161616); color: var(--p-dddddd); }
  .tab.active {
    background: var(--p-1a1a1a);
    color: var(--p-ffffff);
    border-bottom-color: var(--p-66ccff);
    margin-bottom: -1px;
  }
  .tab.historical { color: var(--p-d4a54d); }
  .tab.active.historical { border-bottom-color: var(--p-d4a54d); }
  .tab .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tab .rev {
    font-size: 10px;
    opacity: 0.7;
  }
  .tab .x {
    border: none;
    background: transparent;
    color: inherit;
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
    cursor: pointer;
    border-radius: 2px;
  }
  .tab .x:hover { background: var(--p-2d5578); color: var(--p-ffffff); }
  .hide {
    flex-shrink: 0;
    background: transparent;
    border: none;
    border-left: 1px solid var(--p-262626);
    color: var(--p-888888);
    font-size: 16px;
    line-height: 1;
    padding: 0 10px;
    cursor: pointer;
    border-radius: 0;
  }
  .hide:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .body {
    flex: 1;
    overflow: hidden;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .muted {
    color: var(--p-666666);
    font-size: 12px;
    padding: 12px;
    font-style: italic;
  }
</style>
