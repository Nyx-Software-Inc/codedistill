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
  import type { AgentsStatus } from '../lib/api';

  // Settings → Agents: what runs each agent role, and whether it can. A role
  // runs on the model chosen for it (Settings → Workflows), through that
  // model's connector (Settings → Models). CodeDistill's own agent is always
  // here; Claude Code is used when it is installed and signed in, so a Claude
  // subscription can pay for the work.

  type Props = { userId: string };
  let { userId }: Props = $props();

  let st = $state<AgentsStatus | null>(null);
  let error = $state('');
  let note = $state('');
  let steps = $state('');

  async function load() {
    try {
      st = await api.agentsStatus();
      steps = String(st.step_limit ?? 100);
      error = '';
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
  $effect(() => { void load(); });

  async function saveSteps() {
    const n = Number(steps);
    if (!Number.isInteger(n) || n < 1) {
      error = 'The step limit is a whole number of turns, 1 or more.';
      return;
    }
    try {
      await api.setUserSetting(userId, 'conductor.step_limit', n);
      note = `Each run of CodeDistill's agent now stops after ${n} turns.`;
      error = '';
      setTimeout(() => (note = ''), 4000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  function runsOn(r: AgentsStatus['roles'][number]): string {
    const how = r.runs_on === 'claude-code' ? 'Claude Code' : r.runs_on === 'built-in' ? "CodeDistill's agent" : r.runs_on;
    return r.model ? `${how} · ${r.model}` : `${how} · its default model`;
  }
</script>

<section class="agents">
  <h3>Agents</h3>
  <p class="intro">
    Each agent role runs on the model chosen for it in Workflows. Claude models can run through
    Claude Code, on your Claude subscription; every model can run on CodeDistill's own agent,
    billed to the model's API key. Which one a model uses is set on the model, in Models.
  </p>

  {#if error}<p class="err" role="alert">{error}</p>{/if}
  {#if note}<p class="ok">{note}</p>{/if}

  {#if st}
    {@const cc = st.connectors.claude_code}
    <table>
      <tbody>
        <tr>
          <td class="what">CodeDistill's agent</td>
          <td><span class="state ok">Ready</span></td>
          <td class="dim">Built in. Runs any model you have added.</td>
        </tr>
        {#if cc}
          <tr>
            <td class="what">Claude Code</td>
            <td>
              {#if cc.ready}<span class="state ok">Ready</span>
              {:else if cc.installed}<span class="state warn">Signed out</span>
              {:else}<span class="state off">Not installed</span>{/if}
            </td>
            <td class="dim">
              {#if cc.ready}Signed in · <span class="mono">{cc.path}</span>
              {:else}{cc.why}{/if}
            </td>
          </tr>
        {/if}
      </tbody>
    </table>

    <h4>Fix scanner findings</h4>
    <table>
      <tbody>
        {#each st.roles as r (r.worker_type)}
          <tr>
            <td class="what">{r.role[0].toUpperCase() + r.role.slice(1)}</td>
            {#if r.unavailable}
              <td colspan="2" class="bad">Cannot run: {r.unavailable}</td>
            {:else}
              <td colspan="2">{runsOn(r)}</td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
    {#if st.roles.some((r) => r.unavailable)}
      <p class="help">A job will not start while a role it needs cannot run.</p>
    {/if}

    <h4>Limits</h4>
    <div class="acts">
      <label class="field">
        Turns per run
        <input type="number" min="1" step="1" bind:value={steps} onchange={saveSteps} />
      </label>
    </div>
    <p class="help">
      How many times one run of CodeDistill's agent may ask its model before it stops with what it has.
      The job's checks then judge the result as they would any other. Claude Code keeps its own limits.
    </p>
  {/if}
</section>

<style>
  .agents { display: flex; flex-direction: column; gap: 0.75rem; }
  h3 { margin: 0; font-size: 1rem; }
  h4 { margin: 0.4rem 0 0; font-size: 0.85rem; font-weight: 600; }
  .intro, .help { margin: 0; font-size: 0.85rem; color: var(--text-dim); max-width: 46rem; }
  .help { font-size: 0.78rem; }
  .err { color: var(--danger, #f88); font-size: 0.85rem; margin: 0; }
  .ok { color: var(--ok, #4caf72); font-size: 0.85rem; margin: 0; }
  table { border-collapse: collapse; font-size: 0.82rem; max-width: 46rem; }
  td { padding: 0.35rem 1rem 0.35rem 0; border-top: 1px solid var(--p-2a2a2a); vertical-align: top; }
  .what { white-space: nowrap; }
  .dim { color: var(--text-dim); }
  .bad { color: var(--danger, #e74c3c); }
  .mono { font-family: ui-monospace, monospace; font-size: 0.76rem; }
  .state { border: 1px solid var(--p-3a3a3a); border-radius: 3px; padding: 0 0.3rem; font-size: 0.72rem; white-space: nowrap; }
  .state.ok { color: var(--ok, #4caf72); }
  .state.warn { color: var(--warn, #e0a030); border-color: var(--warn, #e0a030); }
  .state.off { color: var(--text-dim); }
  .acts { display: flex; align-items: center; gap: 1rem; }
  .field { display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; }
  .field input {
    width: 5rem; background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 4px; padding: 0.2rem 0.4rem; font: inherit;
  }
</style>
