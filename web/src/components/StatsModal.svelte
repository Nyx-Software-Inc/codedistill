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
  // Cross-project usage stats (UC-64), opened from the profile menu. A read-only
  // snapshot: totals + per-status breakdowns across every project.
  import * as api from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = { open: boolean; onClose: () => void };
  let { open, onClose }: Props = $props();

  let stats = $state<api.GlobalStats | null>(null);
  let loading = $state(false);
  let err = $state('');
  let loaded = $state(false);

  $effect(() => {
    if (open && !loaded) {
      loaded = true;
      void load();
    }
    if (!open) loaded = false;
  });

  async function load() {
    loading = true;
    err = '';
    try {
      stats = await api.getGlobalStats();
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  const total = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);
  // Status → count, sorted by count desc for a stable, useful order.
  const breakdown = (m: Record<string, number>) =>
    Object.entries(m).sort((a, b) => b[1] - a[1]);
  const pretty = (s: string) => s.replace(/[_-]/g, ' ');
</script>

<Modal {open} title="Usage stats" {onClose} width="480px">
  {#if loading && !stats}
    <p class="muted">Loading…</p>
  {:else if err}
    <p class="err">Couldn't load stats — {err}</p>
  {:else if stats}
    <div class="top">
      <div class="stat"><span class="n">{stats.projects}</span><span class="l">projects</span></div>
      <div class="stat"><span class="n">{stats.scratchpads}</span><span class="l">scratchpads</span></div>
      <div class="stat"><span class="n">{stats.kb}</span><span class="l">KB entries</span></div>
    </div>

    {#each [
      { label: 'Todos', m: stats.todos },
      { label: 'Bugs', m: stats.bugs },
      { label: 'Use cases', m: stats.use_cases },
    ] as grp (grp.label)}
      <div class="grp">
        <div class="grp-head">
          <span class="grp-name">{grp.label}</span>
          <span class="grp-total">{total(grp.m)}</span>
        </div>
        {#if total(grp.m) === 0}
          <p class="empty">none yet</p>
        {:else}
          <div class="states">
            {#each breakdown(grp.m) as [st, n] (st)}
              <span class="state"><b>{n}</b> {pretty(st)}</span>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
    <p class="foot">Across all projects.</p>
  {/if}
</Modal>

<style>
  .muted { color: var(--p-777777); font-size: 12px; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .top {
    display: flex; gap: 10px; margin-bottom: 14px;
  }
  .stat {
    flex: 1; display: flex; flex-direction: column; align-items: center;
    background: var(--p-141414); border: 1px solid var(--p-262626);
    border-radius: 8px; padding: 12px 6px;
  }
  .stat .n { font-size: 24px; font-weight: 700; color: var(--p-e8e8e8); line-height: 1; }
  .stat .l { font-size: 11px; color: var(--p-888888); margin-top: 6px; text-transform: uppercase; letter-spacing: 0.4px; }
  .grp { padding: 10px 0; border-top: 1px solid var(--p-222222); }
  .grp-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 6px; }
  .grp-name { font-size: 13px; font-weight: 600; color: var(--p-dddddd); }
  .grp-total {
    font-size: 13px; font-weight: 700; color: var(--p-99ccff);
    background: var(--p-16222e); border-radius: 9px; padding: 1px 9px;
  }
  .states { display: flex; flex-wrap: wrap; gap: 6px; }
  .state {
    font-size: 11.5px; color: var(--p-bbbbbb);
    background: var(--p-141414); border: 1px solid var(--p-262626);
    border-radius: 4px; padding: 2px 8px;
  }
  .state b { color: var(--p-e8e8e8); }
  .empty { font-size: 11.5px; color: var(--p-777777); font-style: italic; margin: 0; }
  .foot { font-size: 11px; color: var(--p-666666); margin: 10px 0 0; text-align: right; }
</style>
