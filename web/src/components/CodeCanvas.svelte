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
  import DiffViewer from './DiffViewer.svelte';
  import ChangesetPicker, { type ChangesetPick } from './ChangesetPicker.svelte';
  import Icon from './Icon.svelte';
  import type { CodeAnchorWithOwner } from '../lib/types';

  // Multi-tab code viewer column. Conditional — App.svelte only renders
  // this when there's at least one open tab and the user hasn't hidden it.
  // Tab strip scrolls horizontally when many files are open.

  export type CodeTab = {
    path: string;
    revision: string; // "" = working copy
    // Set on a diff tab: the file rendered as base..head with insertions and
    // deletions marked (docs/design/code-review.md §1). revision is then the
    // head ("" for the working copy) so anchors and tooltips keep working.
    diff?: { base: string; head: string; label: string };
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
    // The changeset picker: a popover under the tab strip. App owns whether
    // it is open (a command opens it, so the keyboard and the context menu
    // reach it too) and what a pick does.
    pickerOpen: boolean;
    onPickerOpen: () => void;
    onPickerClose: () => void;
    onPicked: (cs: ChangesetPick) => void;
    // Right-click anywhere in the pane: App renders the registry-driven menu.
    onContextMenu: (e: MouseEvent) => void;
    // From a diff tab whose file is not in its changeset: open another file
    // of that changeset as a diff tab, or the whole changeset in its pane.
    onOpenDiffFile: (path: string, diff: { base: string; head: string; label: string }) => void;
    onOpenChangeset: (diff: { base: string; head: string; label: string }) => void;
    onOpenItem: (ownerType: 'bug_item' | 'todo_item', ownerId: string) => void;
    /** The centre's first tab: the scratchpad canvas
     *  (docs/design/center-tabs.md). While it is active the code below stays
     *  mounted but hidden, so a file keeps its scroll across a trip to the
     *  canvas and back. */
    canvasTab?: { label: string; active: boolean; onSelect: () => void };
    /** Controls for whatever the active tab shows, at the right of the strip:
     *  the canvas's view switch and filters while the canvas is showing. */
    trailing?: import('svelte').Snippet;
  };
  let {
    projectId, tabs, activeIndex,
    onActivate, onClose, onHide, onAnchorClick,
    onCreateItem, hasActiveScratchpad,
    pendingHighlight, onHighlightConsumed,
    externalHover,
    pickerOpen, onPickerOpen, onPickerClose, onPicked,
    onContextMenu, onOpenDiffFile, onOpenChangeset, onOpenItem, canvasTab, trailing,
  }: Props = $props();
  const onCanvas = $derived(!!canvasTab?.active);

  let active = $derived(
    activeIndex >= 0 && activeIndex < tabs.length ? tabs[activeIndex] : null,
  );

  // Auto-scroll the tab strip so the active tab is visible. Triggers on
  // both activate (user clicked) and open-from-canvas (reverse direction
  // dropped a new tab past the visible edge).
  let tabsEl: HTMLDivElement;
  let pickBtn = $state<HTMLButtonElement | null>(null);
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
    if (t.diff) return `${t.path} — diff: ${t.diff.label}`;
    if (t.revision === '') return t.path;
    return `${t.path} @ ${t.revision.slice(0, 7)}`;
  }
  function tabKey(t: CodeTab): string {
    return `${t.path}|${t.revision}|${t.diff ? `${t.diff.base}..${t.diff.head}` : ''}`;
  }

  function closeTab(e: MouseEvent, index: number) {
    e.stopPropagation();
    onClose(index);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="canvas" class:strip-only={onCanvas} oncontextmenu={(e) => { e.preventDefault(); onContextMenu(e); }}>
  <div class="tabstrip">
    <div class="tabs tab-scroll" bind:this={tabsEl}>
      {#if canvasTab}
        <div
          class="tab canvas-tab"
          class:active={onCanvas}
          title="The scratchpad canvas"
          role="tab"
          tabindex="0"
          aria-selected={onCanvas}
          onclick={canvasTab.onSelect}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); canvasTab.onSelect(); } }}
        >
          <Icon name="canvas" size={12} />
          <span class="label">{canvasTab.label}</span>
        </div>
      {/if}
      {#each tabs as t, i (tabKey(t))}
        <div
          class="tab"
          class:active={i === activeIndex && !onCanvas}
          class:historical={t.revision !== '' && !t.diff}
          class:diff={!!t.diff}
          title={tabTooltip(t)}
          role="tab"
          tabindex="0"
          aria-selected={i === activeIndex && !onCanvas}
          onclick={() => onActivate(i)}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onActivate(i); } }}
        >
          <span class="label">{tabLabel(t)}</span>
          {#if t.diff}
            <span class="rev">±{t.diff.head === 'working' ? 'wc' : t.diff.head.slice(0, 7)}</span>
          {:else if t.revision !== ''}
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
    {#if trailing}<div class="trailing">{@render trailing()}</div>{/if}
    <button
      type="button"
      class="pick"
      bind:this={pickBtn}
      class:on={pickerOpen}
      title="Pick a branch, commit or the working copy to review"
      aria-label="Pick changeset"
      aria-expanded={pickerOpen}
      onclick={() => (pickerOpen ? onPickerClose() : onPickerOpen())}
    ><Icon name="branch" size={14} /></button>
    {#if !canvasTab}
      <button
        type="button"
        class="hide"
        title="Hide code canvas"
        aria-label="Hide code canvas"
        onclick={onHide}
      >×</button>
    {/if}
    {#if pickerOpen}
      <ChangesetPicker {projectId} anchor={pickBtn} onPick={onPicked} onClose={onPickerClose} />
    {/if}
  </div>

  <div class="body" class:hidden={onCanvas}>
    {#if active?.diff}
      {@const d = active.diff}
      <DiffViewer
        {projectId}
        path={active.path}
        base={d.base}
        head={d.head}
        onOpenFile={(p) => onOpenDiffFile(p, d)}
        onOpenChangeset={() => onOpenChangeset(d)}
        {onOpenItem}
      />
    {:else if active}
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
      <div class="muted">No file open. Pick a file in the project tree, or a changeset from the branch icon above.</div>
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
  .tab.diff { color: var(--p-99cc99); }
  .tab.active.diff { border-bottom-color: var(--diff-add-fg); }
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
  .pick {
    flex-shrink: 0;
    background: transparent;
    border: none;
    border-left: 1px solid var(--p-262626);
    color: var(--p-888888);
    padding: 0 8px;
    cursor: pointer;
    border-radius: 0;
    display: inline-flex;
    align-items: center;
  }
  .pick:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .pick.on { color: var(--p-99ccff); background: var(--p-1e3a52); }
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
  .canvas.strip-only { height: auto; flex: 0 0 auto; }
  .body.hidden { display: none; }
  .canvas-tab { gap: 0.35rem; }
  .trailing { display: flex; align-items: center; gap: 2px; padding: 0 6px; flex-shrink: 0; }
</style>
