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
  import { powerSource, batteryPause, loadBatteryPause } from '../lib/power';

  // PowerBadge renders a small "on battery" indicator in the header
  // whenever the host reports SourceBattery. On AC (the common case)
  // and on Unknown (degrades to AC) it renders nothing — desktops and
  // servers stay clutter-free.
  //
  // Reflects the throttle policy by reading indexing.battery.pause so
  // the label distinguishes "throttled" from "paused".
  //
  // No timers here any more. The power source comes from one shared,
  // refcounted poller in lib/power.ts, and the pause flag is app-local state
  // that IndexingSettings pushes into a store — so flipping the toggle updates
  // this badge in the same tick instead of up to 30s later, and costs zero
  // requests (CE-review item 43).

  type Props = { onClick?: () => void };
  let { onClick }: Props = $props();

  void loadBatteryPause();

  let visible = $derived($powerSource === 'battery');
  let paused = $derived($batteryPause ?? false);
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
