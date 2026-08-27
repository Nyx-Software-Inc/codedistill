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
  import { onMount, onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import { loadUserBool } from '../lib/userSettings';

  // PowerBadge renders a small "on battery" indicator in the header
  // whenever the host reports SourceBattery. On AC (the common case)
  // and on Unknown (degrades to AC) it renders nothing — desktops and
  // servers stay clutter-free.
  //
  // Reflects the throttle policy by reading indexing.battery.pause so
  // the label distinguishes "throttled" from "paused".

  const KEY_PAUSE = 'indexing.battery.pause';
  const SOURCE_POLL_MS = 10_000;
  const PAUSE_POLL_MS = 30_000;

  type Props = { onClick?: () => void };
  let { onClick }: Props = $props();

  let source = $state<api.PowerSource>('unknown');
  let paused = $state(false);

  let sourceTimer: ReturnType<typeof setInterval> | null = null;
  let pauseTimer: ReturnType<typeof setInterval> | null = null;

  async function refreshSource() {
    try {
      source = (await api.getPowerSource()).source;
    } catch {
      source = 'unknown';
    }
  }

  async function refreshPause() {
    paused = await loadUserBool(KEY_PAUSE, false);
  }

  onMount(() => {
    refreshSource();
    refreshPause();
    sourceTimer = setInterval(refreshSource, SOURCE_POLL_MS);
    // Re-read the pause flag periodically so the badge reflects edits
    // made in Settings without a hard reload.
    pauseTimer = setInterval(refreshPause, PAUSE_POLL_MS);
  });

  onDestroy(() => {
    if (sourceTimer) clearInterval(sourceTimer);
    if (pauseTimer) clearInterval(pauseTimer);
  });

  let visible = $derived(source === 'battery');
  let label = $derived(paused ? 'paused' : 'throttled');
  let title = $derived(
    paused
      ? 'On battery — code indexing is paused (Settings → Indexing)'
      : 'On battery — code indexing is throttled (Settings → Indexing)',
  );
</script>

{#if visible}
  <button
    type="button"
    class="power-badge"
    class:paused
    {title}
    aria-label={title}
    onclick={() => onClick?.()}
  >
    <span class="icon" aria-hidden="true">🔋</span>
    <span class="label">{label}</span>
  </button>
{/if}

<style>
  .power-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border: 1px solid rgba(255, 152, 0, 0.5);
    background: rgba(255, 152, 0, 0.12);
    color: var(--p-b26a00);
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    line-height: 1;
  }
  .power-badge:hover {
    background: rgba(255, 152, 0, 0.2);
  }
  .power-badge.paused {
    border-color: rgba(244, 67, 54, 0.5);
    background: rgba(244, 67, 54, 0.12);
    color: var(--p-b71c1c);
  }
  .power-badge.paused:hover {
    background: rgba(244, 67, 54, 0.2);
  }
  .icon {
    font-size: 13px;
  }
</style>
