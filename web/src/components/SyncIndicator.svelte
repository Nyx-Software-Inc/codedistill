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
  // Per-item MCP-export sync indicator (v0.8.2 step 8). Rendered on
  // list rows + detail lifecycle sections for todos/bugs/kb/use_cases.
  //
  // States (sync_status column):
  //   - '' / 'local-only' → renders nothing; item isn't routed to a
  //     destination, so there's no sync state worth pixels.
  //   - 'synced'  → subtle green check; tooltip has the timestamp.
  //   - 'pending' → amber pill; queued, worker hasn't pushed yet.
  //   - 'failed'  → red pill; tooltip carries last_sync_error verbatim.

  type Props = {
    syncStatus?: string;
    lastSyncAt?: string; // ISO string from the API
    lastSyncError?: string;
  };
  let { syncStatus = '', lastSyncAt, lastSyncError }: Props = $props();

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

  let title = $derived.by(() => {
    switch (syncStatus) {
      case 'synced':
        return `Synced to MCP destination${lastSyncAt ? ' at ' + formatTimestamp(lastSyncAt) : ''}`;
      case 'pending':
        return 'Sync pending — queued for push to the MCP destination';
      case 'failed':
        return `Sync failed${lastSyncError ? ': ' + lastSyncError : ''}${
          lastSyncAt ? ' (last attempt ' + formatTimestamp(lastSyncAt) + ')' : ''
        }`;
      default:
        return '';
    }
  });
</script>

{#if syncStatus === 'synced'}
  <span class="sync synced" {title} aria-label={title}>✓</span>
{:else if syncStatus === 'pending'}
  <span class="sync pill pending" {title} aria-label={title}>
    <span class="icon" aria-hidden="true">⟳</span>sync
  </span>
{:else if syncStatus === 'failed'}
  <span class="sync pill failed" {title} aria-label={title}>
    <span class="icon" aria-hidden="true">⚠</span>sync failed
  </span>
{/if}

<style>
  .sync {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 10px;
    line-height: 1.2;
    flex-shrink: 0;
    cursor: default;
  }
  .synced {
    color: rgba(110, 200, 130, 0.75);
    font-size: 11px;
  }
  .pill {
    padding: 1px 6px;
    border-radius: 4px;
    font-weight: 500;
    letter-spacing: 0.3px;
    white-space: nowrap;
  }
  .pending {
    border: 1px solid rgba(255, 193, 7, 0.45);
    background: rgba(255, 193, 7, 0.1);
    color: var(--p-c9a227);
  }
  .failed {
    border: 1px solid rgba(244, 92, 92, 0.5);
    background: rgba(244, 92, 92, 0.12);
    color: var(--p-e07a7a);
  }
  .icon {
    font-size: 11px;
    line-height: 1;
  }
</style>
