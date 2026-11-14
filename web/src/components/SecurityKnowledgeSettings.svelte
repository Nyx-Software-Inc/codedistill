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
  import * as api from '../lib/api';
  import type { SecurityKnowledge, SecuritySource } from '../lib/api';

  // The security knowledge base: the published sources the security agent
  // reasons from, and how fresh each one is. Freshness is shown, never
  // assumed: a source past its schedule says so here, with its last error.

  type Props = { userId: string };
  let { userId }: Props = $props();

  let kb = $state<SecurityKnowledge | null>(null);
  let error = $state('');
  let note = $state('');
  let busy = $state(false);

  async function load() {
    try {
      kb = await api.getSecurityKnowledge();
      error = '';
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
  $effect(() => { void load(); });

  async function refresh() {
    busy = true;
    note = '';
    try {
      await api.refreshSecurityKnowledge();
      note = 'Refreshing every source now. This takes a few seconds.';
      // The refresh runs in the background; look again once it has had time.
      setTimeout(() => { void load(); busy = false; }, 6000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      busy = false;
    }
  }

  async function setEnabled(on: boolean) {
    try {
      await api.setUserSetting(userId, 'security.refresh_enabled', on);
      note = on ? 'Refreshing is on.' : 'Refreshing is off: nothing will be fetched. Sources will turn stale on their schedule.';
      await load();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  const STATE: Record<SecuritySource['state'], string> = {
    current: 'Current', retrying: 'Retrying', stale: 'Stale', never_synced: 'Never synced',
  };

  function when(iso?: string): string {
    if (!iso) return 'never';
    const d = new Date(iso);
    const mins = Math.round((Date.now() - d.getTime()) / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins} min ago`;
    const h = Math.round(mins / 60);
    if (h < 48) return `${h} h ago`;
    return `${Math.round(h / 24)} days ago`;
  }
</script>

<section class="seckb">
  <h3>Security knowledge</h3>
  <p class="intro">
    Published sources the security agent checks its verdicts against, kept on this machine and
    refreshed on a schedule. What you are shown about a security matter comes from these sources'
    own text, not a model's.
  </p>

  {#if error}<p class="err" role="alert">{error}</p>{/if}
  {#if note}<p class="ok">{note}</p>{/if}

  {#if kb}
    {#if kb.stale.length}
      <p class="warn" role="status">
        Out of date: {kb.stale.join(', ')}. The security agent still rules, and says its data is old.
      </p>
    {/if}

    <table>
      <thead>
        <tr><th>Source</th><th>State</th><th>Refreshed</th><th>Schedule</th><th>Version</th><th>Entries</th></tr>
      </thead>
      <tbody>
        {#each kb.sources as s (s.id)}
          <tr>
            <td>{s.name}</td>
            <td><span class="state {s.state}">{STATE[s.state] ?? s.state}</span></td>
            <td title={s.last_success ?? ''}>{when(s.last_success)}</td>
            <td>{s.every}</td>
            <td class="mono">{s.version || '—'}</td>
            <td>{s.entries.toLocaleString()}</td>
          </tr>
          {#if s.last_error}
            <tr class="errrow"><td colspan="6">Last attempt failed {when(s.last_attempt)}: {s.last_error}</td></tr>
          {/if}
        {/each}
      </tbody>
    </table>

    <div class="acts">
      <button class="btn" disabled={busy || !kb.refresh_enabled} onclick={refresh}>
        {busy ? 'Refreshing…' : 'Refresh now'}
      </button>
      <label class="check">
        <input type="checkbox" checked={kb.refresh_enabled} onchange={(e) => setEnabled(e.currentTarget.checked)} />
        Refresh from the internet
      </label>
    </div>
    <p class="help">
      Untick on a machine that must not reach the internet. It then fetches nothing, and its sources
      turn stale on their schedule; bring them up to date by importing a knowledge pack exported from a
      connected CodeDistill.
    </p>
  {/if}
</section>

<style>
  .seckb { display: flex; flex-direction: column; gap: 0.75rem; }
  h3 { margin: 0; font-size: 1rem; }
  .intro, .help { margin: 0; font-size: 0.85rem; color: var(--text-dim); max-width: 46rem; }
  .help { font-size: 0.78rem; }
  .err { color: var(--danger, #f88); font-size: 0.85rem; margin: 0; }
  .ok { color: var(--ok, #4caf72); font-size: 0.85rem; margin: 0; }
  .warn { color: var(--warn, #e0a030); font-size: 0.85rem; margin: 0; }
  table { border-collapse: collapse; font-size: 0.82rem; max-width: 46rem; }
  th { text-align: left; color: var(--text-dim); font-weight: normal; padding: 0.25rem 1rem 0.25rem 0; }
  td { padding: 0.3rem 1rem 0.3rem 0; border-top: 1px solid var(--p-2a2a2a); }
  .errrow td { border-top: 0; color: var(--danger, #e74c3c); font-size: 0.75rem; padding-top: 0; }
  .mono { font-family: ui-monospace, monospace; font-size: 0.76rem; }
  .state { border: 1px solid var(--p-3a3a3a); border-radius: 3px; padding: 0 0.3rem; font-size: 0.72rem; }
  .state.current { color: var(--ok, #4caf72); }
  .state.retrying { color: var(--warn, #e0a030); }
  .state.stale, .state.never_synced { color: var(--danger, #e74c3c); border-color: var(--danger, #e74c3c); }
  .acts { display: flex; align-items: center; gap: 1rem; }
  .btn { border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a); color: var(--text);
         border-radius: 4px; padding: 0.25rem 0.7rem; font: inherit; font-size: 0.82rem; cursor: pointer; }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .check { display: flex; align-items: center; gap: 0.4rem; font-size: 0.85rem; }
</style>
