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
  import { confirmDialog } from '../lib/dialog';
  // Settings → Server access: view / copy / regenerate the API write-auth
  // token. The UI itself writes via its first-party cookie; this token is
  // what programmatic / agent callers send as `Authorization: Bearer …`.
  import { onMount } from 'svelte';
  import * as api from '../lib/api';

  let loading = $state(true);
  let enabled = $state(false);
  let token = $state('');
  let revealed = $state(false);
  let err = $state('');
  let flash = $state('');
  let busy = $state(false);

  onMount(load);
  async function load() {
    loading = true;
    err = '';
    try {
      const info = await api.getAuthToken();
      enabled = info.enabled;
      token = info.token ?? '';
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  function flashFor(msg: string) {
    flash = msg;
    setTimeout(() => (flash = ''), 1500);
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(token);
      flashFor('Copied');
    } catch {
      flashFor('Copy failed');
    }
  }

  async function regenerate() {
    if (busy) return;
    const ok = await confirmDialog(
      'Regenerate the API token?\n\nThe current token stops working immediately — any agents, scripts, or MCP clients using it must be updated with the new one. The app itself keeps working.',
      { title: 'Regenerate token', confirmLabel: 'Regenerate', danger: true },
    );
    if (!ok) return;
    busy = true;
    err = '';
    try {
      const r = await api.regenerateAuthToken();
      token = r.token;
      revealed = true;
      flashFor('New token generated');
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }

  const masked = $derived(token ? '•'.repeat(Math.min(token.length, 48)) : '');
</script>

<section class="server-access">
  <h3>Server access</h3>
  <p class="lead">
    Reads from this server are open on localhost. <strong>Writes</strong> through
    the API require this token — give it to an agent, script, or MCP client as
    <code>Authorization: Bearer &lt;token&gt;</code>. The app you're using right
    now is already authenticated.
  </p>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if err}
    <p class="err">{err}</p>
  {:else if !enabled}
    <p class="muted">
      Write-auth is disabled (the server is running with <code>-no-auth</code>).
      There's no token to show.
    </p>
  {:else}
    <label class="field">
      <span>API token</span>
      <div class="token-row">
        <input
          class="token"
          type="text"
          readonly
          value={revealed ? token : masked}
        />
        <button type="button" class="btn" onclick={() => (revealed = !revealed)}>
          {revealed ? 'Hide' : 'Reveal'}
        </button>
        <button type="button" class="btn" onclick={copy} disabled={!token}>Copy</button>
      </div>
    </label>

    <div class="actions">
      <button type="button" class="btn danger" onclick={regenerate} disabled={busy}>
        {busy ? 'Regenerating…' : 'Regenerate token'}
      </button>
      {#if flash}<span class="flash">{flash}</span>{/if}
    </div>
    <p class="hint">
      Regenerating invalidates the old token immediately. Use it if the token
      may have leaked.
    </p>
  {/if}
</section>

<style>
  .server-access { max-width: 560px; }
  h3 { margin: 0 0 8px; font-size: 15px; }
  .lead { font-size: 13px; line-height: 1.5; color: var(--p-aaaaaa); margin: 0 0 16px; }
  .lead code, .muted code {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    background: var(--p-1a1a1a);
    padding: 1px 4px;
    border-radius: 3px;
  }
  .muted { color: var(--p-888888); font-size: 13px; }
  .err { color: var(--p-ff8888); font-size: 13px; }
  .field { display: flex; flex-direction: column; gap: 4px; }
  .field > span {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-888888);
  }
  .token-row { display: flex; gap: 6px; align-items: stretch; }
  .token {
    flex: 1;
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    padding: 6px 8px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .btn {
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-333333);
    color: var(--p-dddddd);
    border-radius: 4px;
    padding: 5px 12px;
    font-size: 12px;
    cursor: pointer;
  }
  .btn:hover:not(:disabled) { background: var(--p-262626); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .btn.danger { border-color: var(--p-4a3a14); color: var(--p-d4a54d); }
  .btn.danger:hover:not(:disabled) { background: var(--p-2a2310); }
  .actions { display: flex; align-items: center; gap: 10px; margin-top: 14px; }
  .flash { color: var(--p-66cc66); font-size: 12px; font-style: italic; }
  .hint { font-size: 12px; color: var(--p-777777); margin: 8px 0 0; line-height: 1.5; }
</style>
