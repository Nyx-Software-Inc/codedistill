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
  import Modal from './Modal.svelte';
  import * as api from '../lib/api';

  type Props = {
    open: boolean;
    build: api.VersionInfo | null;
    onClose: () => void;
  };
  let { open, build, onClose }: Props = $props();

  let stats = $state<api.CreditsSummary | null>(null);
  let error = $state<string>('');

  // Fetch on open, reset on close. Cheap query, fine to redo every time.
  $effect(() => {
    if (open && !stats && !error) {
      api.getCredits()
        .then((s) => (stats = s))
        .catch((e) => (error = String(e)));
    }
    if (!open) {
      stats = null;
      error = '';
    }
  });

  // "Distilled X commits into Y items" — Y is the rollup of every item
  // type that gets distilled out of raw scratchpad content.
  let itemsRollup = $derived(
    stats ? stats.items_created + stats.todos_completed + stats.bugs_filed : 0
  );
</script>

<Modal {open} title="Credits" {onClose} width="480px">
  {#if build}
    <div class="build">
      <span class="label">Build</span>
      <span class="mono">v{build.version} · {build.git_sha} · {build.build_date}</span>
    </div>
  {/if}

  {#if error}
    <p class="error">Couldn't load stats: {error}</p>
  {:else if !stats}
    <p class="loading">Counting…</p>
  {:else}
    <div class="oneliner">
      You've distilled <strong>{itemsRollup}</strong> items from your scratchpads.
    </div>

    <div class="grid">
      <div class="stat">
        <div class="n">{stats.items_created}</div>
        <div class="k">scratchpad items</div>
      </div>
      <div class="stat">
        <div class="n">{stats.todos_completed}</div>
        <div class="k">todos completed</div>
      </div>
      <div class="stat">
        <div class="n">{stats.bugs_filed}</div>
        <div class="k">bugs filed</div>
      </div>
    </div>
  {/if}

  <p class="tagline">CodeDistill — distill what you do.</p>
</Modal>

<style>
  .build {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 10px;
    background: var(--p-141414);
    border: 1px solid var(--p-2a2a2a);
    border-radius: 4px;
    margin-bottom: 14px;
  }
  .build .label {
    font-size: 11px;
    color: var(--p-888888);
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }
  .mono {
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', Consolas, monospace;
    font-size: 12px;
    color: var(--p-dddddd);
  }
  .oneliner {
    font-size: 14px;
    color: var(--p-dddddd);
    margin: 0 0 16px 0;
    text-align: center;
  }
  .oneliner strong {
    color: var(--p-66ccff);
    font-weight: 600;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 10px;
  }
  .stat {
    background: var(--p-141414);
    border: 1px solid var(--p-2a2a2a);
    border-radius: 4px;
    padding: 10px;
    text-align: center;
  }
  .stat .n {
    font-size: 20px;
    font-weight: 600;
    color: var(--p-eeeeee);
    font-variant-numeric: tabular-nums;
  }
  .stat .k {
    font-size: 11px;
    color: var(--p-888888);
    margin-top: 2px;
  }
  .loading, .error {
    color: var(--p-888888);
    text-align: center;
    padding: 20px 0;
  }
  .error { color: var(--p-ff8888); }
  .tagline {
    margin-top: 16px;
    font-size: 11px;
    color: var(--p-555555);
    text-align: center;
    font-style: italic;
  }
</style>
