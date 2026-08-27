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
  // Glass-box wedge UI: the complete intent → commit → code throughline for
  // an item, built from its code anchors (the gold ones are agent-authored
  // by record_implementation). Read-only, navigational — click a file to
  // jump to the exact lines that satisfied the intent.
  import * as api from '../lib/api';
  import type {
    CodeAnchor,
    CodeAnchorOwnerType,
    CodeMetrics,
    CommitMetrics,
    ComplexityBand,
  } from '../lib/types';

  type Props = {
    ownerType: CodeAnchorOwnerType;
    ownerId: string;
    intentTitle: string;
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { ownerType, ownerId, intentTitle, onOpenFile }: Props = $props();

  let anchors = $state<CodeAnchor[]>([]);
  let metrics = $state<CodeMetrics | null>(null);
  let loading = $state(false);
  let loadErr = $state('');
  let loadedKey = $state('');

  $effect(() => {
    const key = `${ownerType}:${ownerId}`;
    if (!ownerId || key === loadedKey) return;
    loadedKey = key;
    loading = true;
    loadErr = '';
    anchors = [];
    metrics = null;
    // Anchors drive the throughline; metrics enrich it. Fetch in parallel;
    // a metrics failure (e.g. repo not configured) must not blank the panel.
    api
      .listCodeAnchors(ownerType, ownerId)
      .then((a) => { anchors = a; })
      .catch((e) => { loadErr = String(e); })
      .finally(() => { loading = false; });
    api
      .getCodeMetrics(ownerType, ownerId)
      .then((m) => { metrics = m; })
      .catch(() => { metrics = null; });
  });

  const commits = $derived(anchors.filter((a) => a.kind === 'commit'));
  const files = $derived(anchors.filter((a) => a.kind === 'file'));
  const prs = $derived(anchors.filter((a) => a.kind === 'pr'));
  const hasAny = $derived(anchors.length > 0);
  const totals = $derived(metrics?.totals ?? null);
  const showTotals = $derived(!!totals && totals.commits > 0);

  function shortSha(s?: string): string {
    return s ? s.slice(0, 7) : '';
  }
  function lineLabel(a: CodeAnchor): string {
    if (a.line_start && a.line_end) return `L${a.line_start}–${a.line_end}`;
    if (a.line_start) return `L${a.line_start}`;
    return 'whole file';
  }
  // Match a commit anchor to its computed metrics (anchor.revision may be a
  // short SHA; the backend resolves it to the full sha on the metric row).
  function metricFor(rev?: string): CommitMetrics | undefined {
    if (!rev || !metrics) return undefined;
    return metrics.commits.find(
      (m) => m.sha === rev || m.sha.startsWith(rev) || rev.startsWith(m.short_sha),
    );
  }
  // Transparent heuristic → a colour cue for human risk-routing.
  function complexityClass(band?: ComplexityBand): string {
    switch (band) {
      case 'Very High': return 'cx-vhigh';
      case 'High': return 'cx-high';
      case 'Moderate': return 'cx-mod';
      default: return 'cx-low';
    }
  }
</script>

<section class="throughline">
  {#if loading}
    <p class="muted">Loading throughline…</p>
  {:else if loadErr}
    <p class="err">Failed to load: {loadErr}</p>
  {:else}
    <div class="node intent">
      <span class="node-k">Intent</span>
      <span class="node-v">{intentTitle || '(this item)'}</span>
    </div>

    {#if showTotals && totals}
      <div class="cost" title="Churn across the commits that implemented this intent. Complexity is a heuristic for routing review attention, not a guarantee.">
        <span class="cost-k">Cost</span>
        <span class="churn add">+{totals.added}</span>
        <span class="churn del">−{totals.deleted}</span>
        <span class="cost-meta">net {totals.net >= 0 ? '+' : ''}{totals.net}</span>
        <span class="cost-meta">{totals.files_changed} file{totals.files_changed === 1 ? '' : 's'}</span>
        <span class="cost-meta">{totals.hunks} hunk{totals.hunks === 1 ? '' : 's'}</span>
        <span class="cost-meta">{totals.commits} commit{totals.commits === 1 ? '' : 's'}</span>
        <span
          class="cx {complexityClass(totals.complexity)}"
          title="Heuristic review-effort score {totals.complexity_score} (churn + 8·files + 4·hunks). Bands: Low/Moderate/High/Very High."
        >{totals.complexity}</span>
        {#if totals.excluded_files > 0}
          <span
            class="excluded"
            title="Generated/vendored files (build output, lockfiles, minified bundles) are set aside so complexity reflects real review burden — not regenerated artifacts."
          >· {totals.excluded_files} generated excluded</span>
        {/if}
      </div>
    {/if}

    {#if !hasAny}
      <div class="empty">
        <div class="empty-icon">⛓</div>
        <p class="empty-head">No implementation recorded yet.</p>
        <p class="sub">
          When an agent implements this item, the commit and the exact lines
          it changed appear here — the complete throughline from intent to
          code, captured as the work happens.
        </p>
      </div>
    {:else}
      {#if commits.length}
        <div class="rail">↓ implemented in</div>
        {#each commits as c (c.id)}
          {@const m = metricFor(c.revision)}
          <div class="node commit">
            <span class="sha">◆ {shortSha(c.revision)}</span>
            {#if c.label}<span class="node-v">{c.label}</span>{/if}
            {#if m && m.found}
              <span class="commit-metrics">
                <span class="churn add">+{m.added}</span>
                <span class="churn del">−{m.deleted}</span>
                <span
                  class="cx-dot {complexityClass(m.complexity)}"
                  title="{m.files_changed} file{m.files_changed === 1 ? '' : 's'}, {m.hunks} hunk{m.hunks === 1 ? '' : 's'} · complexity {m.complexity} (score {m.complexity_score})"
                ></span>
              </span>
            {:else if m && !m.found}
              <span class="missing" title="This commit no longer resolves in the project repo">commit missing</span>
            {/if}
          </div>
        {/each}
      {/if}

      {#if files.length}
        <div class="rail">↓ touching</div>
        <ul class="files">
          {#each files as f (f.id)}
            <li>
              <button
                class="file-btn"
                type="button"
                title={onOpenFile ? 'Open in code canvas' : f.path}
                onclick={() => onOpenFile && f.path && onOpenFile(f.path, f.revision ?? '')}
                disabled={!onOpenFile}
              >
                <span class="path">{f.path}</span>
                <span class="lines">{lineLabel(f)}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}

      {#if prs.length}
        <div class="rail">↓ referenced</div>
        {#each prs as p (p.id)}
          <div class="node">
            <a class="pr" href={p.url} target="_blank" rel="noopener">{p.label || p.url}</a>
          </div>
        {/each}
      {/if}
    {/if}
  {/if}
</section>

<style>
  .throughline { padding: 4px 2px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .node {
    display: flex;
    align-items: baseline;
    gap: 8px;
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-radius: 4px;
    padding: 8px 10px;
  }
  .node.intent { border-left: 3px solid var(--p-66ccff); }
  .node.commit { border-left: 3px solid var(--p-d4a54d); }
  .node-k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-888888);
    flex: none;
  }
  .node-v { color: var(--p-eeeeee); font-size: 13px; }
  .sha {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    color: var(--p-d4a54d);
    flex: none;
  }
  .rail {
    color: var(--p-777777);
    font-size: 11px;
    margin: 4px 0 4px 10px;
    padding-left: 8px;
    border-left: 1px dashed var(--p-333333);
  }
  .files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 3px; }
  .file-btn {
    display: flex;
    align-items: baseline;
    gap: 10px;
    width: 100%;
    text-align: left;
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-radius: 4px;
    padding: 6px 10px;
    cursor: pointer;
    color: var(--p-dddddd);
  }
  .file-btn:hover:not(:disabled) { background: var(--p-1c1c1c); border-color: var(--p-2d5578); }
  .file-btn:disabled { cursor: default; }
  .path {
    flex: 1;
    font-family: ui-monospace, monospace;
    font-size: 12px;
    color: var(--p-cceeff);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lines {
    flex: none;
    font-size: 11px;
    color: var(--p-888888);
    font-variant-numeric: tabular-nums;
  }
  .pr { color: var(--p-99ccff); font-size: 12px; }

  /* Code metrics (UC-100) */
  .cost {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin: 6px 0 2px;
    padding: 7px 10px;
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-left: 3px solid var(--p-888888);
    border-radius: 4px;
  }
  .cost-k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-888888);
    flex: none;
  }
  .cost-meta { font-size: 11px; color: var(--p-999999); font-variant-numeric: tabular-nums; }
  .churn {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .churn.add { color: var(--p-66cc66); }
  .churn.del { color: var(--p-e07a7a); }
  .commit-metrics { display: inline-flex; align-items: center; gap: 7px; margin-left: auto; flex: none; }
  .cx {
    font-size: 11px;
    font-weight: 600;
    padding: 1px 7px;
    border-radius: 10px;
    flex: none;
  }
  .cx.cx-low    { background: rgba(102,204,102,0.16); color: var(--p-66cc66); }
  .cx.cx-mod    { background: rgba(212,165,77,0.18);  color: var(--p-d4a54d); }
  .cx.cx-high   { background: rgba(255,152,0,0.18);   color: var(--p-ff9800); }
  .cx.cx-vhigh  { background: rgba(224,122,122,0.20); color: var(--p-e07a7a); }
  .cx-dot { width: 9px; height: 9px; border-radius: 50%; flex: none; }
  .cx-dot.cx-low { background: var(--p-66cc66); }
  .cx-dot.cx-mod { background: var(--p-d4a54d); }
  .cx-dot.cx-high { background: var(--p-ff9800); }
  .cx-dot.cx-vhigh { background: var(--p-e07a7a); }
  .missing { font-size: 11px; color: var(--p-e07a7a); margin-left: auto; flex: none; }
  .excluded { font-size: 10px; color: var(--p-777777); font-style: italic; }
  .empty {
    text-align: center;
    padding: 18px 12px;
    color: var(--p-888888);
  }
  .empty-icon { font-size: 22px; opacity: 0.5; margin-bottom: 6px; }
  .empty-head { color: var(--p-bbbbbb); font-size: 13px; margin: 0 0 4px; }
  .sub { font-size: 12px; line-height: 1.5; color: var(--p-777777); margin: 0 auto; max-width: 340px; }
</style>
