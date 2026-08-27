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
  // Docked item cards — kept beside the code window so the INTENT stays in
  // view while you work its code. Deliberately NOT the code location (the code
  // window's own header already shows the file/commit — that'd be redundant):
  // the card shows the item's subject, status, and its acceptance criteria —
  // the definition of done you're checking your work against.
  import * as api from '../lib/api';
  import type { AcceptanceCriterion, BugItem, UseCaseItem } from '../lib/types';
  import {
    dock, undock, togglePin, toggleCollapse, dockOwnerLabel, asAnchorOwner,
    type DockedItem,
  } from '../lib/dock.svelte';

  type Props = {
    onOpenFile: (path: string, revision: string) => void;
    onOpenFull: (ownerType: DockedItem['ownerType'], ownerId: string) => void;
  };
  let { onOpenFile, onOpenFull }: Props = $props();

  type Loaded = { number: number; subject: string; status: string; brief: string; criteria: AcceptanceCriterion[] };
  let cache = $state<Record<string, Loaded>>({});

  $effect(() => {
    for (const d of dock.items) {
      if (cache[d.key]) continue;
      void load(d);
    }
  });
  async function load(d: DockedItem) {
    try {
      const [item, criteria] = await Promise.all([
        d.ownerType === 'todo_item' ? api.getTodo(d.ownerId)
          : d.ownerType === 'bug_item' ? api.getBug(d.ownerId)
          : api.getUseCase(d.ownerId),
        api.listAcceptanceCriteria(asAnchorOwner(d.ownerType), d.ownerId),
      ]);
      // A one-line "what's the gist" — bug's actual behavior, use-case's want,
      // else the subject speaks for itself.
      let brief = '';
      if (d.ownerType === 'bug_item') brief = (item as BugItem).actual_behavior ?? '';
      else if (d.ownerType === 'use_case_item') brief = (item as UseCaseItem).want ?? '';
      cache[d.key] = {
        number: item.number, subject: item.subject, status: item.status,
        brief: brief.trim(), criteria: criteria ?? [],
      };
    } catch {
      cache[d.key] = { number: 0, subject: '(could not load)', status: '', brief: '', criteria: [] };
    }
  }

  const CRIT_MARK: Record<string, string> = {
    satisfied: '✓', failed: '✗', accepted: '○', proposed: '·', rejected: '–',
  };
  function critClass(state: string): string {
    return state === 'satisfied' ? 'sat' : state === 'failed' ? 'fail'
      : state === 'accepted' ? 'acc' : 'prop';
  }
</script>

{#if dock.items.length > 0}
  <div class="dock">
    <div class="dock-head">Docked <span class="n">{dock.items.length}/3</span></div>
    {#each dock.items as d (d.key)}
      {@const info = cache[d.key]}
      <div class="card" class:pinned={d.pinned}>
        <div class="card-head">
          <button class="chev" onclick={() => toggleCollapse(d.key)} title={d.collapsed ? 'Expand' : 'Collapse'}>
            {d.collapsed ? '▸' : '▾'}
          </button>
          <span class="type">{dockOwnerLabel(d.ownerType)}</span>
          <span class="title" title={info?.subject ?? ''}>
            {#if info}<span class="num">#{info.number}</span>{info.subject}{:else}Loading…{/if}
          </span>
          <button class="ico" class:on={d.pinned} onclick={() => togglePin(d.key)} title={d.pinned ? 'Unpin' : 'Pin (keep docked)'}>📌</button>
          <button class="ico" onclick={() => onOpenFull(d.ownerType, d.ownerId)} title="Open full detail">↗</button>
          <button class="ico" onclick={() => undock(d.key)} title="Close">✕</button>
        </div>
        {#if !d.collapsed && info}
          <div class="card-body">
            {#if info.status}<span class="status">{info.status}</span>{/if}
            {#if info.brief}<p class="brief">{info.brief.slice(0, 180)}{info.brief.length > 180 ? '…' : ''}</p>{/if}
            {#if info.criteria.length > 0}
              <div class="crit-lbl">Definition of done</div>
              <ul class="crits">
                {#each info.criteria as c (c.id)}
                  <li class="crit {critClass(c.state)}" title={c.state}>
                    <span class="mark">{CRIT_MARK[c.state] ?? '·'}</span>{c.text}
                  </li>
                {/each}
              </ul>
            {:else}
              <p class="muted">No acceptance criteria.</p>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .dock {
    height: 100%; display: flex; flex-direction: column; gap: 6px;
    overflow-y: auto; background: var(--p-0d0d0d); border-left: 1px solid var(--p-262626);
    padding: 8px; min-height: 0;
  }
  .dock-head { font-size: 10px; text-transform: uppercase; letter-spacing: 0.5px; color: var(--p-777777); padding: 2px 2px 4px; display: flex; gap: 6px; align-items: baseline; }
  .dock-head .n { color: var(--p-99ccff); }
  .card { background: var(--p-141414); border: 1px solid var(--p-2a2a2a); border-radius: 6px; flex: none; }
  .card.pinned { border-color: var(--p-2d5578); }
  .card-head { display: flex; align-items: center; gap: 5px; padding: 6px 7px; }
  .chev { background: none; border: none; color: var(--p-888888); cursor: pointer; font-size: 10px; padding: 0 2px; }
  .type { flex: none; font-size: 9px; text-transform: uppercase; letter-spacing: 0.4px; color: var(--p-ccaaff); border: 1px solid var(--p-3a2a55); border-radius: 3px; padding: 0 5px; }
  .title { flex: 1; min-width: 0; font-size: 12px; color: var(--p-e8e8e8); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .num { color: var(--p-888888); margin-right: 4px; font-family: ui-monospace, monospace; font-size: 10px; }
  .ico { background: none; border: none; cursor: pointer; font-size: 11px; padding: 1px 3px; opacity: 0.6; filter: grayscale(1); }
  .ico:hover, .ico.on { opacity: 1; filter: none; }
  .card-body { padding: 0 9px 9px 22px; }
  .status { display: inline-block; font-size: 10px; color: var(--p-999999); text-transform: lowercase; margin-bottom: 5px; }
  .brief { font-size: 11.5px; color: var(--p-bbbbbb); margin: 0 0 7px; line-height: 1.4; }
  .crit-lbl { font-size: 9px; text-transform: uppercase; letter-spacing: 0.4px; color: var(--p-777777); margin-bottom: 3px; }
  .crits { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 3px; }
  .crit { display: flex; gap: 5px; font-size: 11px; line-height: 1.35; color: var(--p-cccccc); }
  .crit .mark { flex: none; width: 12px; text-align: center; font-weight: 600; }
  .crit.sat { color: var(--p-a9d5a9); }
  .crit.sat .mark { color: var(--p-66cc66); }
  .crit.fail { color: var(--p-e0a0a0); }
  .crit.fail .mark { color: var(--p-e07a7a); }
  .crit.acc .mark { color: var(--p-99ccff); }
  .crit.prop { color: var(--p-999999); }
  .muted { font-size: 11px; color: var(--p-666666); margin: 2px 0; font-style: italic; }
</style>
