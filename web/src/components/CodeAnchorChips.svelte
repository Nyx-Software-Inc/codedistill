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
  import { alertDialog } from '../lib/dialog';
  import { onMount } from 'svelte';
  import * as api from '../lib/api';
  import type { CodeAnchor, CodeAnchorOwnerType } from '../lib/types';
  import CodeAnchorEdit from './CodeAnchorEdit.svelte';

  type Props = {
    ownerType: CodeAnchorOwnerType;
    ownerId: string;
    // Version bumps trigger a refetch — parent can bump on external saves.
    reloadKey?: number;
    // UC-9: when provided, clicking a FILE chip jumps to the code
  // (the host closes itself and opens the code canvas); commit
  // details / editing move to the ⓘ button. Without it (or for
  // commit/pr/url anchors) clicks open the editor as before.
  onOpenFile?: (path: string, revision: string) => void;
};
  let { ownerType, ownerId, reloadKey = 0 , onOpenFile }: Props = $props();

  let anchors = $state<CodeAnchor[]>([]);
  let loading = $state(false);
  let loadErr = $state('');

  // Modal state: null = closed; 'new' = create; CodeAnchor = edit that anchor.
  let editing = $state<CodeAnchor | 'new' | null>(null);

  let currentOwner = $state('');
  let currentReloadKey = $state(-1);
  $effect(() => {
    const key = `${ownerType}:${ownerId}`;
    if (key !== currentOwner || reloadKey !== currentReloadKey) {
      currentOwner = key;
      currentReloadKey = reloadKey;
      load();
    }
  });

  async function load() {
    if (!ownerId) {
      anchors = [];
      return;
    }
    loading = true;
    loadErr = '';
    try {
      anchors = await api.listCodeAnchors(ownerType, ownerId);
    } catch (e) {
      loadErr = String(e);
    } finally {
      loading = false;
    }
  }

  async function removeAnchor(a: CodeAnchor) {
    try {
      await api.deleteCodeAnchor(a.id);
      anchors = anchors.filter((x) => x.id !== a.id);
    } catch (e) {
      void alertDialog(`Remove failed: ${e}`);
    }
  }

  function chipLabel(a: CodeAnchor): string {
    if (a.label) return a.label;
    if (a.kind === 'file') {
      const p = a.path ?? '';
      const lr =
        a.line_start && a.line_end
          ? `:${a.line_start}-${a.line_end}`
          : a.line_start
            ? `:${a.line_start}`
            : '';
      const rev = a.revision ? ` @${a.revision.slice(0, 7)}` : '';
      return `${p}${lr}${rev}`;
    }
    if (a.kind === 'commit') {
      return (a.revision ?? '').slice(0, 7);
    }
    if (a.kind === 'pr') {
      // Extract `owner/repo#N` from a well-formed URL; fall back to the full URL.
      const m = /github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/.exec(a.url ?? '');
      if (m) return `${m[1]}/${m[2]}#${m[3]}`;
      const g = /gitlab\.com\/([^/]+)\/([^/]+)\/-\/merge_requests\/(\d+)/.exec(a.url ?? '');
      if (g) return `${g[1]}/${g[2]}!${g[3]}`;
      return a.url ?? '';
    }
    return '(anchor)';
  }

  function chipIcon(kind: string): string {
    if (kind === 'file') return '📄';
    if (kind === 'commit') return '●';
    if (kind === 'pr') return 'PR';
    return '?';
  }

  // Provenance distinction: url-detected and agent-suggested both render
  // dimmed so the user can tell them apart from hand-set anchors. Agent-
  // suggested gets a slightly purple tint to match the sparkle treatment
  // in the code-canvas gutter (stage 3 of experiment/code-canvas).
  function chipClass(a: CodeAnchor): string {
    if (a.provenance === 'agent-suggested') return 'chip suggested';
    if (a.provenance === 'url-detected') return 'chip detected';
    return 'chip';
  }

  // Human-friendly provenance label for the title attribute.
  // Mirrors the helper in ScratchpadGrid so the badge tooltip on
  // the canvas card and the chip tooltip in the detail panel use
  // the same vocabulary. If a third call site appears, extract to
  // a shared lib/anchorProvenance.ts.
  function chipTitle(a: CodeAnchor): string {
    const tag = (() => {
      switch (a.provenance) {
        case 'user-set':       return 'You added this';
        case 'url-detected':   return 'Auto-detected from a URL in the content';
        case 'file-dropped':   return 'From a file you dropped onto the item';
        case 'agent-suggested': return 'LLM-suggested — verify before relying on it';
        default: return a.provenance;
      }
    })();
    return tag;
  }

  function edit(a: CodeAnchor) {
    editing = a;
  }

  function onSaved() {
    load();
  }
</script>

<div class="wrap">
  <div class="header">
    <span class="label">Code links</span>
    <button type="button" class="add" onclick={() => (editing = 'new')}>+ Add</button>
  </div>

  {#if loading && anchors.length === 0}
    <div class="muted">Loading…</div>
  {:else if loadErr}
    <div class="err">{loadErr}</div>
  {:else if anchors.length === 0}
    <div class="muted">No code links. Paste a GitHub blob/PR URL into content or click "+ Add".</div>
  {:else}
    <div class="chips">
      {#each anchors as a (a.id)}
        <span class={chipClass(a)} title={chipTitle(a)}>
          <button
            type="button"
            class="chip-main"
            title={a.kind === 'file' && onOpenFile ? 'Open in code canvas' : 'Edit code link'}
            onclick={() => {
              if (a.kind === 'file' && onOpenFile) onOpenFile(a.path ?? '', a.revision ?? '');
              else edit(a);
            }}
          >
            <span class="icon">{chipIcon(a.kind)}</span>
            <span class="text">{chipLabel(a)}</span>
          </button>
          {#if a.kind === 'file' && onOpenFile}
            <button
              type="button"
              class="chip-info"
              onclick={() => edit(a)}
              title="Edit / details"
              aria-label="Anchor details"
            >ⓘ</button>
          {/if}
          <button
            type="button"
            class="chip-x"
            onclick={() => removeAnchor(a)}
            aria-label="Remove anchor"
          >×</button>
        </span>
      {/each}
    </div>
  {/if}
</div>

<CodeAnchorEdit
  open={editing !== null}
  {ownerType}
  {ownerId}
  anchor={editing === 'new' ? null : editing}
  onClose={() => (editing = null)}
  onSaved={onSaved}
/>

<style>
  .wrap { margin-bottom: 12px; }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 4px;
  }
  .label {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .add {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-99ccff);
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .add:hover { background: var(--p-1a1a1a); }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    background: var(--p-0a0a0a);
    border: 1px solid var(--p-262626);
    padding: 4px 6px;
    border-radius: 3px;
    min-height: 30px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    font-size: 11px;
    border-radius: 3px;
    font-family: ui-monospace, monospace;
    overflow: hidden;
  }
  .chip.detected {
    background: var(--p-1a2a38);
    color: var(--p-7aa9c8);
  }
  .chip.suggested {
    background: var(--p-2a1e3a);
    color: var(--p-c084fc);
  }
  .chip-main {
    background: transparent;
    border: none;
    color: inherit;
    font: inherit;
    padding: 2px 4px 2px 6px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    max-width: 360px;
  }
  .chip-main:hover { background: var(--p-2d5578); }
  .chip .icon {
    font-size: 10px;
    opacity: 0.8;
    flex-shrink: 0;
  }
  .chip .text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chip-x {
    border: none;
    background: transparent;
    color: inherit;
    font-size: 12px;
    padding: 0 6px 0 2px;
    line-height: 1;
    cursor: pointer;
  }
  .chip-x:hover { color: var(--p-ffffff); }
  .muted {
    color: var(--p-666666);
    font-size: 11px;
    font-style: italic;
  }
  .err { color: var(--p-ff8888); font-size: 11px; }
  .chip-info {
    background: transparent;
    border: none;
    color: var(--p-777777);
    padding: 0 3px;
    font-size: 11px;
    cursor: pointer;
    line-height: 1;
  }
  .chip-info:hover { color: var(--p-99ccff); }
</style>
