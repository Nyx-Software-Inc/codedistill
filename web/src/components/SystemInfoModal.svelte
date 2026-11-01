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
  // What this installation IS: version and edition, the machine, where the data
  // lives and how big it is, which models it talks to.
  //
  // Separate from Stats on purpose. Stats answers "how many KB entries do I
  // have"; this answers "what am I running and where" — a question asked when
  // something is wrong, which is also why every value is one Copy click away.
  import * as api from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = {
    open: boolean;
    onClose: () => void;
  };
  let { open, onClose }: Props = $props();

  let info = $state<api.SystemInfo | null>(null);
  let err = $state('');
  let loading = $state(false);
  let copied = $state(false);

  // Re-read on every open rather than once: uptime and database size are the
  // two things most likely to have changed since you last looked, and a stale
  // uptime is worse than no uptime.
  $effect(() => {
    if (open) void load();
  });

  async function load() {
    loading = true;
    err = '';
    try {
      info = await api.getSystemInfo();
    } catch (e) {
      // 403 here means a non-admin on a multi-user server. Say which, rather
      // than showing a bare error for something that is working as designed.
      const msg = String(e);
      err = /403/.test(msg)
        ? 'System details are available to workspace owners and admins.'
        : msg;
    } finally {
      loading = false;
    }
  }

  function bytes(n: number): string {
    if (!n) return '—';
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    // One decimal below 100 so 1.6 MB does not round to 2 MB.
    return `${v < 100 ? v.toFixed(1) : Math.round(v)} ${u[i]}`;
  }

  function uptime(secs: number): string {
    if (secs < 60) return `${secs}s`;
    const d = Math.floor(secs / 86400);
    const h = Math.floor((secs % 86400) / 3600);
    const m = Math.floor((secs % 3600) / 60);
    if (d) return `${d}d ${h}h`;
    if (h) return `${h}h ${m}m`;
    return `${m}m`;
  }

  /** The whole panel as plain text. The point of a diagnostics screen is
   *  getting its contents to someone else. */
  function asText(i: api.SystemInfo): string {
    const L = [
      `CodeDistill ${i.build.version} (${i.build.edition})`,
      `  commit     ${i.build.git_sha}`,
      `  built      ${i.build.build_date}`,
      `  features   ${i.build.features.length ? i.build.features.join(', ') : 'none'}`,
      '',
      `  os         ${i.runtime.os}/${i.runtime.arch}, ${i.runtime.cpus} cpu`,
      `  go         ${i.runtime.go_version}`,
      `  listening  ${i.runtime.addr}`,
      `  uptime     ${uptime(i.runtime.uptime_seconds)} (pid ${i.runtime.pid})`,
      '',
      `  database   ${i.storage.dialect} ${i.storage.database_path ?? ''} ${bytes(i.storage.database_bytes)}`,
      `  blobs      ${i.storage.blob_path ?? '—'} ${bytes(i.storage.blob_bytes)} in ${i.storage.blob_files} file(s)`,
      '',
      `  models     ${i.models.endpoint ?? '—'} ${i.models.reachable ? '(reachable)' : '(NOT reachable)'}`,
      `  default    ${i.models.default_model ?? '—'}`,
      `  embedding  ${i.models.embed_model ?? '—'}`,
      `  installed  ${i.models.installed?.join(', ') || '—'}`,
    ];
    if (i.models.error) L.push(`  model error ${i.models.error}`);
    return L.join('\n');
  }

  async function copy() {
    if (!info) return;
    try {
      await navigator.clipboard.writeText(asText(info));
      copied = true;
      setTimeout(() => (copied = false), 1600);
    } catch {
      /* clipboard refused (no permission / insecure context) — nothing to say */
    }
  }
</script>

<Modal {open} title="About CodeDistill" {onClose} width="620px">
  {#if loading && !info}
    <p class="muted">Reading…</p>
  {:else if err}
    <p class="err">{err}</p>
  {:else if info}
    <div class="head">
      <div class="ver">
        <strong>CodeDistill {info.build.version}</strong>
        <span class="edition">{info.build.edition}</span>
      </div>
      <button class="copy" onclick={copy} title="Copy all of this as text">
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>

    <dl>
      <dt>Commit</dt><dd class="mono">{info.build.git_sha}</dd>
      <dt>Built</dt><dd>{info.build.build_date}</dd>
      <dt>Paid features</dt>
      <dd>{info.build.features.length ? info.build.features.join(', ') : 'none — community build'}</dd>
    </dl>

    <h4>Machine</h4>
    <dl>
      <dt>System</dt><dd>{info.runtime.os}/{info.runtime.arch} · {info.runtime.cpus} CPUs</dd>
      <dt>Go</dt><dd class="mono">{info.runtime.go_version}</dd>
      <dt>Listening on</dt><dd class="mono">{info.runtime.addr}</dd>
      <dt>Uptime</dt>
      <dd>{uptime(info.runtime.uptime_seconds)} <span class="dim">· pid {info.runtime.pid}</span></dd>
    </dl>

    <h4>Storage</h4>
    <dl>
      <dt>Database</dt>
      <dd>
        <strong>{bytes(info.storage.database_bytes)}</strong>
        <span class="dim">{info.storage.dialect}</span>
        {#if info.storage.database_path}
          <div class="path mono">{info.storage.database_path}</div>
        {:else}
          <!-- Postgres: no file to measure, and the DSN is never shown because
               it carries a password. -->
          <div class="dim small">Server-managed; no local file to measure.</div>
        {/if}
      </dd>
      <dt>Attachments</dt>
      <dd>
        <strong>{bytes(info.storage.blob_bytes)}</strong>
        <span class="dim">{info.storage.blob_files} file{info.storage.blob_files === 1 ? '' : 's'}</span>
        {#if info.storage.blob_path}<div class="path mono">{info.storage.blob_path}</div>{/if}
      </dd>
    </dl>

    <h4>Models</h4>
    <dl>
      <dt>Endpoint</dt>
      <dd>
        <span class="mono">{info.models.endpoint ?? '—'}</span>
        {#if info.models.reachable}
          <span class="ok">reachable</span>
        {:else}
          <span class="bad">not reachable</span>
        {/if}
        {#if info.models.error}<div class="dim small">{info.models.error}</div>{/if}
      </dd>
      <dt>Default</dt><dd class="mono">{info.models.default_model ?? '—'}</dd>
      <dt>Embedding</dt><dd class="mono">{info.models.embed_model ?? '—'}</dd>
      <dt>Installed</dt>
      <dd>
        {#if info.models.installed?.length}
          <div class="chips">
            {#each info.models.installed as m (m)}<span class="chip mono">{m}</span>{/each}
          </div>
        {:else}
          <span class="dim">{info.models.reachable ? 'none installed' : 'unknown — endpoint did not answer'}</span>
        {/if}
      </dd>
    </dl>
  {/if}
</Modal>

<style>
  .muted { color: var(--p-777777); font-size: 12px; }
  .err { color: var(--p-ff8888); font-size: 12.5px; }

  .head {
    display: flex; align-items: center; justify-content: space-between;
    gap: 10px; margin-bottom: 10px;
  }
  .ver { display: flex; align-items: baseline; gap: 8px; }
  .ver strong { font-size: 15px; color: var(--p-eeeeee); }
  .edition {
    font-size: 10px; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--p-99ccff); background: var(--p-1e3a52);
    border-radius: 3px; padding: 1px 7px;
  }
  .copy { font-size: 11px; padding: 3px 11px; flex: none; }

  h4 {
    margin: 16px 0 6px; font-size: 11px; text-transform: uppercase;
    letter-spacing: 0.6px; color: var(--p-888888); font-weight: 600;
  }

  dl {
    display: grid;
    grid-template-columns: 8.5rem 1fr;
    gap: 5px 12px;
    margin: 0;
    font-size: 12.5px;
  }
  dt { color: var(--p-888888); }
  dd { margin: 0; color: var(--p-dddddd); min-width: 0; }
  .mono { font-family: ui-monospace, monospace; font-size: 11.5px; }
  .dim { color: var(--p-777777); }
  .small { font-size: 11px; }
  /* Paths wrap rather than overflow — a long data directory should not force
     the modal to scroll sideways. */
  .path { color: var(--p-7d97b0); margin-top: 2px; word-break: break-all; }

  .ok { color: var(--p-99cc99); font-size: 11px; }
  .bad { color: var(--p-ff8888); font-size: 11px; }

  .chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip {
    background: var(--p-1a1a1a); border: 1px solid var(--p-2a2a2a);
    border-radius: 3px; padding: 1px 6px;
  }
</style>
