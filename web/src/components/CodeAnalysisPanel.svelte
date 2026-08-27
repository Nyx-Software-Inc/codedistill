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
  // Code Analysis results page (UC-14). Static analyzers detect — gosec +
  // staticcheck for Go, semgrep for every other language (Kotlin, Java, Ruby,
  // C#, C/C++, TS/JS, Python, …); findings list here ranked by severity. Check the ones
  // worth acting on and Push them to the active scratchpad (the classifier turns
  // them into bugs/todos, carrying a code anchor to the file/line); dismiss the
  // noise. Both make the finding leave the page. Scan now runs on request.
  import { onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import type { CodeFinding, AnalysisScan } from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = {
    open: boolean;
    projectId: string;
    projectName?: string;
    scratchpadId: string;
    scratchpadName?: string;
    onClose: () => void;
    onPushed?: () => void;
  };
  let { open, projectId, projectName = '', scratchpadId, scratchpadName = '', onClose, onPushed }: Props = $props();

  let findings = $state<CodeFinding[]>([]);
  let scan = $state<AnalysisScan | null>(null);
  let running = $state(false);
  let loading = $state(false);
  let err = $state('');
  let loadedFor = $state('');
  let checked = $state<Set<string>>(new Set());
  let busy = $state(false);

  // Filters.
  let sevFilter = $state<'all' | 'high' | 'medium' | 'low' | 'info'>('all');
  // 'all' or any analyzer name (gosec/staticcheck/semgrep); the dropdown is
  // populated dynamically from the findings actually present.
  let analyzerFilter = $state<string>('all');
  let fileFilter = $state('');

  const SEV_COLOR: Record<string, string> = {
    high: '#e07a7a', medium: '#d4a54d', low: '#99ccff', info: '#888888',
  };

  const visible = $derived(
    findings.filter(
      (f) =>
        (sevFilter === 'all' || f.severity === sevFilter) &&
        (analyzerFilter === 'all' || f.analyzer === analyzerFilter) &&
        (fileFilter === '' || f.file_path.toLowerCase().includes(fileFilter.toLowerCase())),
    ),
  );
  const analyzers = $derived([...new Set(findings.map((f) => f.analyzer))]);

  // Load when opened or the project changes. Closing the panel (or unmounting)
  // stops any in-flight scan poll — a closed panel must not keep polling.
  $effect(() => {
    if (open && projectId && loadedFor !== projectId) {
      loadedFor = projectId;
      void reload();
    }
    if (!open) {
      loadedFor = '';
      stopPolling();
    }
  });
  onDestroy(stopPolling);

  async function reload() {
    loading = true;
    err = '';
    try {
      const [fs, ls] = await Promise.all([api.listCodeFindings(projectId), api.latestCodeScan(projectId)]);
      findings = fs;
      scan = ls.scan;
      running = ls.running;
      checked = new Set();
      if (running) pollUntilDone();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Failed to load findings';
    } finally {
      loading = false;
    }
  }

  async function scanNow() {
    err = '';
    try {
      await api.startCodeScan(projectId);
      running = true;
      pollUntilDone();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Could not start scan';
    }
  }

  // Poll the scan-status endpoint until the run finishes, then refresh findings.
  // The handle is stored so the poll can be stopped (panel close, component
  // destroy); the poll is pinned to the project it started for; and a run of
  // consecutive errors ends it instead of looping forever (audit BUG-95).
  let polling = false;
  let pollHandle: ReturnType<typeof setTimeout> | undefined;
  const POLL_MAX_ERRORS = 5;

  function stopPolling() {
    clearTimeout(pollHandle);
    pollHandle = undefined;
    polling = false;
    running = false;
  }

  async function pollUntilDone() {
    if (polling) return;
    polling = true;
    const pollProject = projectId; // pin: a project switch must not adopt this poll
    let errors = 0;
    const tick = async () => {
      // The user switched projects (or closed the panel) since this poll
      // started — abandon it so it can't write another project's findings.
      if (!open || projectId !== pollProject) {
        stopPolling();
        return;
      }
      try {
        const ls = await api.latestCodeScan(pollProject);
        errors = 0;
        running = ls.running;
        scan = ls.scan;
        if (!running) {
          findings = await api.listCodeFindings(pollProject);
          checked = new Set();
          stopPolling();
          return;
        }
      } catch (e) {
        // Transient errors are tolerated; a persistent failure ends the poll
        // and surfaces, rather than hammering the endpoint every 2.5s forever.
        if (++errors >= POLL_MAX_ERRORS) {
          err = `Scan status unavailable: ${e instanceof Error ? e.message : e}`;
          stopPolling();
          return;
        }
      }
      pollHandle = setTimeout(tick, 2500);
    };
    pollHandle = setTimeout(tick, 2500);
  }

  function toggle(id: string) {
    const next = new Set(checked);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    checked = next;
  }
  function toggleAll() {
    if (visible.every((f) => checked.has(f.id))) {
      checked = new Set();
    } else {
      checked = new Set(visible.map((f) => f.id));
    }
  }

  async function pushSelected() {
    if (!scratchpadId || checked.size === 0) return;
    busy = true;
    err = '';
    const ids = [...checked];
    try {
      for (const id of ids) {
        await api.pushCodeFinding(id, scratchpadId);
        findings = findings.filter((f) => f.id !== id);
      }
      checked = new Set();
      onPushed?.();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Push failed';
    } finally {
      busy = false;
    }
  }

  async function dismiss(id: string) {
    busy = true;
    err = '';
    try {
      await api.dismissCodeFinding(id);
      findings = findings.filter((f) => f.id !== id);
      const next = new Set(checked);
      next.delete(id);
      checked = next;
    } catch (e) {
      err = e instanceof Error ? e.message : 'Dismiss failed';
    } finally {
      busy = false;
    }
  }

  function scanLine(): string {
    if (running) return 'Scanning…';
    if (!scan) return 'No scan yet.';
    const when = scan.finished_at ? new Date(scan.finished_at).toLocaleString() : '—';
    return `Last scan ${when} · ${scan.files_scanned} files · ${scan.findings_new} new · ${scan.findings_resolved} resolved`;
  }
</script>

<Modal {open} title={`Code Analysis — ${projectName}`} {onClose} width="980px">
  <div class="ca">
    <div class="status">
      <div class="status-line" class:running>
        {scanLine()}
        {#if scan?.skipped}
          <span class="skip" title={scan.skipped}>⚠ analyzers skipped</span>
        {/if}
        {#if scan?.error}
          <span class="skip" title={scan.error}>⚠ notes</span>
        {/if}
      </div>
      <button class="scan-btn" onclick={scanNow} disabled={running}>
        {running ? 'Scanning…' : '⟳ Scan now'}
      </button>
    </div>

    {#if scan?.skipped}
      <div class="hint install">{scan.skipped}</div>
    {/if}

    <div class="filters">
      <select bind:value={sevFilter}>
        <option value="all">All severities</option>
        <option value="high">High</option>
        <option value="medium">Medium</option>
        <option value="low">Low</option>
        <option value="info">Info</option>
      </select>
      <select bind:value={analyzerFilter}>
        <option value="all">All analyzers</option>
        {#each analyzers as a (a)}
          <option value={a}>{a}</option>
        {/each}
      </select>
      <input class="file-filter" type="text" placeholder="Filter by file…" bind:value={fileFilter} spellcheck="false" />
      <span class="count">{visible.length} of {findings.length}</span>
    </div>

    {#if err}<div class="err">{err}</div>{/if}

    {#if loading}
      <div class="empty">Loading…</div>
    {:else if findings.length === 0}
      <div class="empty">
        {#if running}Scanning…{:else if scan}✓ No open findings — clean.{:else}No scan yet. Hit <strong>Scan now</strong> to analyze this repo.{/if}
      </div>
    {:else}
      <div class="toolbar">
        <label class="selall">
          <input
            type="checkbox"
            checked={visible.length > 0 && visible.every((f) => checked.has(f.id))}
            onchange={toggleAll}
          />
          Select all visible
        </label>
        <button
          class="push-btn"
          disabled={checked.size === 0 || !scratchpadId || busy}
          onclick={pushSelected}
          title={scratchpadId ? `Push to “${scratchpadName}”` : 'Open a scratchpad to push into'}
        >
          ↗ Push selected ({checked.size})
        </button>
      </div>

      <div class="list">
        {#each visible as f (f.id)}
          <div class="row" class:on={checked.has(f.id)}>
            <input type="checkbox" checked={checked.has(f.id)} onchange={() => toggle(f.id)} />
            <span class="sev" style="background:{SEV_COLOR[f.severity] ?? '#888'}">{f.severity}</span>
            <div class="body">
              <div class="title">{f.title}</div>
              <div class="loc">
                <span class="rule">{f.analyzer} {f.rule_id}</span>
                · {f.file_path}:{f.line_start}
              </div>
              {#if f.detail}<pre class="detail">{f.detail}</pre>{/if}
            </div>
            <button class="dismiss" onclick={() => dismiss(f.id)} disabled={busy} title="Dismiss (won't return on re-scan)">✕</button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</Modal>

<style>
  .ca { display: flex; flex-direction: column; gap: 10px; min-height: 320px; }
  .status { display: flex; align-items: center; gap: 12px; }
  .status-line { flex: 1; font-size: 12px; color: var(--p-aaaaaa); }
  .status-line.running { color: var(--p-99ccff); }
  .skip { margin-left: 8px; color: var(--p-d4a54d); cursor: help; }
  .scan-btn {
    background: var(--p-1e3a52); border: 1px solid var(--p-2d5578); color: var(--p-cceeff);
    padding: 5px 12px; border-radius: 4px; font-size: 12px; cursor: pointer;
  }
  .scan-btn:disabled { opacity: 0.5; cursor: default; }
  .hint.install {
    font-size: 11px; color: var(--p-888888); background: var(--p-141414);
    border: 1px solid var(--p-2a2a2a); border-radius: 4px; padding: 6px 8px; white-space: pre-wrap;
  }
  .filters { display: flex; align-items: center; gap: 8px; }
  .filters select, .file-filter {
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-dddddd);
    border-radius: 4px; padding: 4px 8px; font-size: 12px;
  }
  .file-filter { flex: 1; }
  .count { font-size: 11px; color: var(--p-777777); }
  .err { color: var(--p-e07a7a); font-size: 12px; }
  .empty { padding: 40px 8px; text-align: center; color: var(--p-888888); font-size: 13px; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; }
  .selall { font-size: 12px; color: var(--p-aaaaaa); display: flex; align-items: center; gap: 6px; cursor: pointer; }
  .push-btn {
    background: var(--p-1e3a52); border: 1px solid var(--p-2d5578); color: var(--p-cceeff);
    padding: 5px 12px; border-radius: 4px; font-size: 12px; cursor: pointer;
  }
  .push-btn:disabled { opacity: 0.45; cursor: default; }
  .list { display: flex; flex-direction: column; gap: 6px; max-height: 52vh; overflow-y: auto; }
  .row {
    display: grid; grid-template-columns: auto auto 1fr auto; gap: 10px; align-items: start;
    background: var(--p-141414); border: 1px solid var(--p-2a2a2a); border-radius: 5px; padding: 8px 10px;
  }
  .row.on { border-color: var(--p-2d5578); background: var(--p-15212c); }
  .sev {
    font-size: 10px; text-transform: uppercase; letter-spacing: 0.04em; color: #111;
    padding: 1px 6px; border-radius: 3px; font-weight: 600; margin-top: 2px; height: fit-content;
  }
  .body { min-width: 0; }
  .title { font-size: 13px; color: var(--p-e0e0e0); line-height: 1.35; }
  .loc { font-size: 11px; color: var(--p-888888); margin-top: 2px; }
  .rule { color: var(--p-aaaaaa); }
  .detail {
    margin: 6px 0 0; font-size: 11px; color: var(--p-999999); background: var(--p-0d0d0d);
    border: 1px solid var(--p-222222); border-radius: 4px; padding: 6px 8px; overflow-x: auto;
    white-space: pre-wrap; word-break: break-word; max-height: 120px;
  }
  .dismiss {
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-888888);
    width: 24px; height: 24px; border-radius: 4px; cursor: pointer; font-size: 12px;
  }
  .dismiss:hover { color: var(--p-e07a7a); border-color: var(--p-5a3030); }
</style>
