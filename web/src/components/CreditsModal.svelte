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
    license: api.LicenseInfo | null;
    onClose: () => void;
  };
  let { open, build, license, onClose }: Props = $props();

  // AGPL-3.0 §13 requires that users interacting with a modified program over a
  // network are PROMINENTLY offered the Corresponding Source, and §5(d) requires
  // interactive interfaces to display Appropriate Legal Notices. CodeDistill
  // serves its UI over HTTP, so this dialog is where both obligations are met —
  // which is why the version chip opens it on a single click rather than hiding
  // it behind the old triple-click easter egg.
  const SOURCE_URL = 'https://github.com/Nyx-Software-Inc/codedistill';

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

<Modal {open} title="About CodeDistill" {onClose} width="480px">
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

  <div class="legal">
    <p>Copyright © 2026 Nyx&nbsp;Software,&nbsp;Inc. All rights reserved.</p>
    {#if license?.oss_build}
      <p>
        This is the <strong>Community Edition</strong>, free software licensed under the
        <a href="https://www.gnu.org/licenses/agpl-3.0.html" target="_blank" rel="noreferrer noopener">GNU
        Affero General Public License, version 3</a>. It comes with
        <strong>absolutely no warranty</strong>, to the extent permitted by law.
      </p>
      <p>
        The complete corresponding source for this version is available at
        <a href={SOURCE_URL} target="_blank" rel="noreferrer noopener">{SOURCE_URL}</a>.
      </p>
    {:else}
      <p>
        Licensed commercially from Nyx&nbsp;Software,&nbsp;Inc. CodeDistill is dual-licensed: a
        Community Edition is also published as free software under the
        <a href="https://www.gnu.org/licenses/agpl-3.0.html" target="_blank" rel="noreferrer noopener">GNU
        AGPL&nbsp;v3</a>, with source at
        <a href={SOURCE_URL} target="_blank" rel="noreferrer noopener">{SOURCE_URL}</a>.
      </p>
    {/if}
  </div>
</Modal>

<style>
  .legal {
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--p-2a2a2a);
    font-size: 11px;
    line-height: 1.55;
    color: var(--p-888888);
  }
  .legal p { margin: 0 0 6px 0; }
  .legal p:last-child { margin-bottom: 0; }
  .legal a { color: var(--p-99ccff); }
  .legal strong { color: var(--p-bbbbbb); font-weight: 600; }

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
