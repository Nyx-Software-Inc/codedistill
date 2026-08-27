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
  // Compact orange pill rendered inline on list rows + detail headers
  // for items in the in_progress / in-progress state. Shows the
  // claimer name (truncated). Hover tooltip surfaces the full claim
  // timestamp + claimer.

  type Props = {
    claimedBy?: string;
    claimedAt?: string; // ISO string from the API
  };
  let { claimedBy = '', claimedAt }: Props = $props();

  function formatTimestamp(iso: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toLocaleString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  }

  let label = $derived(claimedBy || 'In progress');
  let title = $derived(
    claimedBy
      ? `In progress — claimed by ${claimedBy}${claimedAt ? ' at ' + formatTimestamp(claimedAt) : ''}`
      : 'In progress',
  );
</script>

<span class="ip-badge" {title}>
  <span class="icon" aria-hidden="true">◐</span>
  <span class="label">{label}</span>
</span>

<style>
  .ip-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 6px;
    border: 1px solid rgba(255, 152, 0, 0.5);
    background: rgba(255, 152, 0, 0.12);
    color: var(--p-b26a00);
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
    line-height: 1.2;
    max-width: 140px;
    overflow: hidden;
  }
  .icon { font-size: 12px; line-height: 1; }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
