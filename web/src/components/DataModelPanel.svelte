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
  // Data-model / ER diagram (UC-45, slice 2). Deterministic — drawn from the
  // project's SQL schema (computed on demand, no persistence). Solid lines are
  // declared FOREIGN KEYs; dashed lines are inferred *_id naming matches (honest
  // guesses you can toggle off). Complements the AI-drafted architecture diagram.
  import * as api from '../lib/api';
  import type { DataModelTable, DataModelColumn, DataModelRelation } from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = { open: boolean; projectId: string; projectName?: string; onClose: () => void; onPublished?: () => void };
  let { open, projectId, projectName = '', onClose, onPublished }: Props = $props();

  let tables = $state<DataModelTable[]>([]);
  let relations = $state<DataModelRelation[]>([]);
  let sources = $state<string[]>([]);
  let warnings = $state<string[]>([]);
  let repoConfigured = $state(false);
  let loading = $state(false);
  let err = $state('');
  let loadedFor = $state('');

  // View controls (Rich's defaults): keys shown, expand-per-table for all
  // columns, inferred edges on with a toggle to hide the dashed guesses.
  let showAllCols = $state(false);
  let showInferred = $state(true);
  let expanded = $state<Set<string>>(new Set()); // lower(table) individually expanded

  const BOX_W = 208, HEADER_H = 26, ROW_H = 16, PAD_Y = 8, GAP_X = 34, GAP_Y = 26, MARGIN = 24;

  $effect(() => {
    if (open && projectId && loadedFor !== projectId) {
      loadedFor = projectId;
      void load();
    }
    if (!open) loadedFor = '';
  });

  async function load() {
    loading = true;
    err = '';
    try {
      const r = await api.getDataModel(projectId);
      tables = r.tables ?? [];
      relations = r.relations ?? [];
      sources = r.sources_parsed ?? [];
      warnings = r.warnings ?? [];
      repoConfigured = r.repo_configured;
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  function toggleExpand(name: string) {
    const k = name.toLowerCase();
    const next = new Set(expanded);
    if (next.has(k)) next.delete(k);
    else next.add(k);
    expanded = next;
  }

  // Columns worth showing when collapsed: primary keys, foreign keys, and any
  // column that's an endpoint of a relation (declared or inferred, so a key
  // column stays visible even when its inferred edge is toggled off).
  const keyNamesByTable = $derived.by<Map<string, Set<string>>>(() => {
    const m = new Map<string, Set<string>>();
    const add = (t: string, c: string) => {
      const k = t.toLowerCase();
      const s = m.get(k) ?? new Set<string>();
      s.add(c.toLowerCase());
      m.set(k, s);
    };
    for (const t of tables) for (const c of t.columns) if (c.pk || c.fk) add(t.name, c.name);
    for (const r of relations) { add(r.from_table, r.from_col); add(r.to_table, r.to_col); }
    return m;
  });

  function shownCols(t: DataModelTable): DataModelColumn[] {
    if (showAllCols || expanded.has(t.name.toLowerCase())) return t.columns;
    const keys = keyNamesByTable.get(t.name.toLowerCase());
    if (!keys || keys.size === 0) return t.columns;
    const filtered = t.columns.filter((c) => keys.has(c.name.toLowerCase()));
    return filtered.length ? filtered : t.columns;
  }

  type Box = { t: DataModelTable; x: number; y: number; w: number; h: number; cx: number; cy: number; cols: DataModelColumn[] };

  // Auto-layout: order tables by connectedness (hubs first) then masonry-pack
  // into balanced columns (place each box in the currently-shortest column).
  const placed = $derived.by<Box[]>(() => {
    const deg = new Map<string, number>();
    const bump = (n: string) => deg.set(n.toLowerCase(), (deg.get(n.toLowerCase()) ?? 0) + 1);
    for (const r of relations) { bump(r.from_table); bump(r.to_table); }
    const ordered = [...tables].sort(
      (a, b) => (deg.get(b.name.toLowerCase()) ?? 0) - (deg.get(a.name.toLowerCase()) ?? 0) || a.name.localeCompare(b.name),
    );
    const nCols = Math.min(5, Math.max(1, Math.round(Math.sqrt(ordered.length || 1))));
    const colH = new Array(nCols).fill(MARGIN);
    const out: Box[] = [];
    for (const t of ordered) {
      const cols = shownCols(t);
      const h = HEADER_H + cols.length * ROW_H + PAD_Y;
      let c = 0;
      for (let i = 1; i < nCols; i++) if (colH[i] < colH[c]) c = i;
      const x = MARGIN + c * (BOX_W + GAP_X);
      const y = colH[c];
      colH[c] = y + h + GAP_Y;
      out.push({ t, x, y, w: BOX_W, h, cx: x + BOX_W / 2, cy: y + h / 2, cols });
    }
    return out;
  });

  const boxByName = $derived(new Map(placed.map((b) => [b.t.name.toLowerCase(), b])));
  const width = $derived(Math.max(MARGIN * 2 + BOX_W, ...placed.map((b) => b.x + b.w + MARGIN)));
  const height = $derived(Math.max(MARGIN * 2, ...placed.map((b) => b.y + b.h + MARGIN)));
  const shownRels = $derived(relations.filter((r) => showInferred || !r.inferred));
  const inferredCount = $derived(relations.filter((r) => r.inferred).length);

  function colTag(c: DataModelColumn): string {
    if (c.pk) return 'PK';
    if (c.fk) return 'FK';
    return '';
  }

  // --- Provenance (glass-box: every element says where it came from) ---
  // Tables are tinted by the directory of their defining file, so schema areas
  // (e.g. foundational vs experimental migrations) are visible at a glance;
  // hover tooltips carry the exact files.
  const DIR_TINTS = ['#5f8fbf', '#7aa860', '#b08fc0', '#c09060', '#5fb0a0', '#c07878', '#8f8fd0', '#b0b060'];
  function dirOf(p?: string): string {
    if (!p) return '';
    const i = p.lastIndexOf('/');
    return i >= 0 ? p.slice(0, i) : '(root)';
  }
  const dirLegend = $derived.by<Map<string, string>>(() => {
    const m = new Map<string, string>();
    for (const t of tables) {
      const d = dirOf(t.defined_in);
      if (d && !m.has(d)) m.set(d, DIR_TINTS[m.size % DIR_TINTS.length]);
    }
    return m;
  });
  const showTints = $derived(dirLegend.size > 1);
  function tintOf(t: DataModelTable): string {
    return dirLegend.get(dirOf(t.defined_in)) ?? '#3a3a3a';
  }
  function tableTip(t: DataModelTable): string {
    if (!t.defined_in) return t.name;
    const lines = [`${t.name}`, `defined in ${t.defined_in}`];
    for (const f of t.modified_by ?? []) lines.push(`modified by ${f}`);
    return lines.join('\n');
  }
  function colTip(t: DataModelTable, c: DataModelColumn): string {
    if (!c.added_in) return `${t.name}.${c.name}`;
    return `${t.name}.${c.name}\nadded in ${c.added_in}`;
  }

  // --- Export / Publish (mirrors the architecture panel) ---
  let svgEl = $state<SVGSVGElement | null>(null);
  let copyMenu = $state(false);
  let publishOpen = $state(false);
  let pubFormat = $state<'image' | 'mermaid'>('image');
  let pubPadId = $state('');
  let pads = $state<{ id: string; name: string }[]>([]);
  let exporting = $state(false);
  let exportMsg = $state('');
  let exportMsgTimer: ReturnType<typeof setTimeout> | undefined;
  function flash(msg: string) {
    exportMsg = msg;
    clearTimeout(exportMsgTimer);
    exportMsgTimer = setTimeout(() => (exportMsg = ''), 2800);
  }

  function serializeSVG(): string {
    if (!svgEl) return '';
    const clone = svgEl.cloneNode(true) as SVGSVGElement;
    clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
    clone.setAttribute('font-family', 'system-ui, sans-serif');
    return new XMLSerializer().serializeToString(clone);
  }

  async function svgToPngFile(name: string): Promise<File | null> {
    const svg = serializeSVG();
    if (!svg) return null;
    const url = URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' }));
    try {
      const img = new Image();
      await new Promise((res, rej) => { img.onload = res; img.onerror = rej; img.src = url; });
      const scale = 2;
      const canvas = document.createElement('canvas');
      canvas.width = Math.max(1, width) * scale;
      canvas.height = Math.max(1, height) * scale;
      const ctx = canvas.getContext('2d');
      if (!ctx) return null;
      ctx.fillStyle = '#0d0d0d';
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.scale(scale, scale);
      ctx.drawImage(img, 0, 0);
      return api.dataURLtoFile(canvas.toDataURL('image/png'), name);
    } catch {
      return null;
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  async function copyPng() {
    copyMenu = false;
    try {
      const file = await svgToPngFile('datamodel.png');
      if (!file) { flash('Could not render image'); return; }
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': file })]);
      flash('Copied PNG to clipboard');
    } catch {
      flash('Clipboard blocked — try Publish instead');
    }
  }
  async function copySvg() {
    copyMenu = false;
    try {
      await navigator.clipboard.writeText(serializeSVG());
      flash('Copied SVG markup');
    } catch {
      flash('Clipboard blocked');
    }
  }
  // Mermaid erDiagram renders natively on GitHub/GitLab/Notion — paste into a
  // README or PR and the schema diagram lives there.
  async function copyMermaid() {
    copyMenu = false;
    try {
      await navigator.clipboard.writeText(mermaidDoc());
      flash('Copied mermaid — paste into GitHub/Notion');
    } catch {
      flash('Clipboard blocked');
    }
  }

  async function togglePublish() {
    publishOpen = !publishOpen;
    copyMenu = false;
    if (publishOpen && pads.length === 0) {
      try {
        pads = await api.listScratchpads(projectId);
        if (pads[0]) pubPadId = pads[0].id;
      } catch (e) { err = String(e); }
    }
  }

  // Portable mermaid erDiagram (renders on GitHub, mermaid.live, etc.). Declared
  // FKs use an identifying relationship (--), inferred ones a non-identifying
  // dashed one (..), so the honesty distinction survives the export.
  function mermaidDoc(): string {
    const ent = (s: string) => (s || 'table').replace(/[^A-Za-z0-9_]/g, '_');
    const attr = (s: string) => (s || '').replace(/[^A-Za-z0-9_]/g, '_') || 'x';
    const lines = ['```mermaid', 'erDiagram'];
    for (const t of tables) {
      lines.push(`  ${ent(t.name)} {`);
      for (const c of t.columns) {
        const key = c.pk ? ' PK' : c.fk ? ' FK' : '';
        lines.push(`    ${attr(c.type) || 'col'} ${attr(c.name)}${key}`);
      }
      lines.push('  }');
    }
    for (const r of shownRels) {
      const rel = r.inferred ? '||..o{' : '||--o{';
      lines.push(`  ${ent(r.to_table)} ${rel} ${ent(r.from_table)} : "${r.from_col}${r.inferred ? ' (inferred)' : ''}"`);
    }
    lines.push('```');
    return `# Data model — ${projectName}\n\n${lines.join('\n')}\n`;
  }

  async function doPublish() {
    if (!pubPadId) return;
    exporting = true;
    try {
      if (pubFormat === 'image') {
        const file = await svgToPngFile(`datamodel-${projectName || 'project'}.png`);
        if (!file) throw new Error('render failed');
        await api.uploadBlob(pubPadId, file);
      } else {
        await api.createItem(pubPadId, { content: mermaidDoc(), content_type: 'composite' });
      }
      publishOpen = false;
      flash('Published to scratchpad');
      onPublished?.();
    } catch (e) {
      flash(`Publish failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      exporting = false;
    }
  }
</script>

<Modal open={open} title={`Data model — ${projectName}`} {onClose} width="960px">
  {#snippet children()}
    <div class="ctrls">
      <button class="btn" class:active={showAllCols} onclick={() => (showAllCols = !showAllCols)}
        title="Show every column, or just keys">
        {showAllCols ? 'Keys only' : 'Show all columns'}
      </button>
      {#if inferredCount > 0}
        <button class="btn" class:active={!showInferred} onclick={() => (showInferred = !showInferred)}
          title="Inferred edges are *_id naming guesses, not declared foreign keys">
          {showInferred ? `Hide inferred (${inferredCount})` : `Show inferred (${inferredCount})`}
        </button>
      {/if}
      <span class="legend">
        <i class="sw solid"></i>foreign key
        <i class="sw dashed"></i>inferred (*_id)
      </span>
      {#if err}<span class="err">{err}</span>{/if}

      {#if tables.length > 0}
        <div class="export">
          {#if exportMsg}<span class="export-msg">{exportMsg}</span>{/if}
          <div class="menu">
            <button class="btn" onclick={() => { copyMenu = !copyMenu; publishOpen = false; }}>⎘ Copy ▾</button>
            {#if copyMenu}
              <div class="pop">
                <button onclick={copyPng}>Copy as PNG</button>
                <button onclick={copySvg}>Copy as SVG</button>
                <button onclick={copyMermaid} title="Renders natively on GitHub, GitLab, Notion…">Copy as Mermaid</button>
              </div>
            {/if}
          </div>
          <div class="menu">
            <button class="btn" onclick={togglePublish}>↗ Publish ▾</button>
            {#if publishOpen}
              <div class="pop wide">
                <label class="pub-row"><span>Format</span>
                  <select bind:value={pubFormat}>
                    <option value="image">Image (PNG)</option>
                    <option value="mermaid">Doc (mermaid ER)</option>
                  </select>
                </label>
                <label class="pub-row"><span>Scratchpad</span>
                  <select bind:value={pubPadId}>
                    {#if pads.length === 0}<option value="">No scratchpads</option>{/if}
                    {#each pads as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
                  </select>
                </label>
                <button class="btn primary" disabled={!pubPadId || exporting} onclick={doPublish}>
                  {exporting ? 'Publishing…' : 'Publish'}
                </button>
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </div>

    {#if loading && tables.length === 0}
      <p class="muted">Loading…</p>
    {:else if !repoConfigured}
      <div class="empty">
        <p class="head">No repository connected.</p>
        <p class="sub">Connect a Git repository to this project (in project settings) and the
          data-model diagram is drawn automatically from its SQL schema.</p>
      </div>
    {:else if tables.length === 0}
      <div class="empty">
        <p class="head">No SQL schema detected.</p>
        <p class="sub">This diagram is generated deterministically from SQL <code>CREATE TABLE</code>
          statements — in a <code>schema.sql</code> or your migrations. None were found in this repo.</p>
      </div>
    {:else}
      <div class="diagram">
        <svg bind:this={svgEl} viewBox="0 0 {width} {height}" {width} {height}>
          <defs>
            <marker id="er-arrow" markerWidth="9" markerHeight="9" refX="8" refY="3" orient="auto">
              <path d="M0,0 L7,3 L0,6 Z" fill="#6a86a8"></path>
            </marker>
            <marker id="er-arrow-inf" markerWidth="9" markerHeight="9" refX="8" refY="3" orient="auto">
              <path d="M0,0 L7,3 L0,6 Z" fill="#8a8a8a"></path>
            </marker>
          </defs>
          {#each shownRels as r (r.from_table + '.' + r.from_col + '->' + r.to_table)}
            {@const a = boxByName.get(r.from_table.toLowerCase())}
            {@const b = boxByName.get(r.to_table.toLowerCase())}
            {#if a && b && a !== b}
              <line
                x1={a.cx} y1={a.cy} x2={b.cx} y2={b.cy}
                stroke={r.inferred ? '#8a8a8a' : '#6a86a8'} stroke-width="1"
                stroke-dasharray={r.inferred ? '5 4' : 'none'}
                marker-end={r.inferred ? 'url(#er-arrow-inf)' : 'url(#er-arrow)'}
              ></line>
              <text x={(a.cx + b.cx) / 2} y={(a.cy + b.cy) / 2 - 3} fill="#7d7d7d" font-size="8" text-anchor="middle">{r.from_col}</text>
            {:else if a && b && a === b}
              <!-- self-reference: small loop label near the box -->
              <text x={a.x + a.w - 6} y={a.y + 12} fill="#7d7d7d" font-size="8" text-anchor="end">↺ {r.from_col}</text>
            {/if}
          {/each}
          {#each placed as b (b.t.name)}
            {@const isExp = showAllCols || expanded.has(b.t.name.toLowerCase())}
            <g class="tbl" role="button" tabindex="0"
              onclick={() => toggleExpand(b.t.name)}
              onkeydown={(ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); toggleExpand(b.t.name); } }}>
              <rect x={b.x} y={b.y} width={b.w} height={b.h} rx="6" fill="#131313" stroke="#3a3a3a" stroke-width="1"></rect>
              <rect x={b.x} y={b.y} width={b.w} height={HEADER_H} rx="6" fill="#1b2733" stroke="#2d5578" stroke-width="1">
                <title>{tableTip(b.t)}</title>
              </rect>
              <rect x={b.x} y={b.y + HEADER_H - 6} width={b.w} height="6" fill="#1b2733"></rect>
              {#if showTints}
                <!-- Provenance tint: colored band keyed to the defining file's directory. -->
                <rect x={b.x} y={b.y} width="4" height={b.h} rx="2" fill={tintOf(b.t)}>
                  <title>{tableTip(b.t)}</title>
                </rect>
              {/if}
              <text x={b.x + 10} y={b.y + 17} fill="#cfe6ff" font-size="12" font-weight="600">{b.t.name}<title>{tableTip(b.t)}</title></text>
              <text x={b.x + b.w - 8} y={b.y + 17} fill="#5f7f9f" font-size="9" text-anchor="end">
                {b.t.columns.length}{isExp || b.cols.length === b.t.columns.length ? '' : ` · ${b.cols.length} keys`}
              </text>
              {#each b.cols as c, i (c.name)}
                {@const cy = b.y + HEADER_H + i * ROW_H + 12}
                <text x={b.x + 10} y={cy} fill={c.pk ? '#e0c07a' : c.fk ? '#8fb8e0' : '#c8c8c8'} font-size="10.5">{c.name}<title>{colTip(b.t, c)}</title></text>
                <text x={b.x + b.w - 8} y={cy} fill="#707070" font-size="9" text-anchor="end">
                  <title>{colTip(b.t, c)}</title>
                  {#if colTag(c)}<tspan fill={c.pk ? '#e0c07a' : '#8fb8e0'} font-weight="600">{colTag(c)} </tspan>{/if}{c.type}
                </text>
              {/each}
            </g>
          {/each}
        </svg>
      </div>

      <div class="foot">
        <span class="muted">Parsed {sources.length} SQL file{sources.length === 1 ? '' : 's'} · {tables.length} table{tables.length === 1 ? '' : 's'} · click a table to expand its columns · hover for source files</span>
        {#if warnings.length > 0}
          <span class="warn" title={warnings.join('\n')}>⚠ {warnings.length} unresolved reference{warnings.length === 1 ? '' : 's'}</span>
        {/if}
      </div>
      {#if showTints}
        <div class="dir-legend">
          {#each [...dirLegend] as [dir, tint] (dir)}
            <span class="dir-chip"><i class="dot" style="background: {tint}"></i>{dir}</span>
          {/each}
        </div>
      {/if}
    {/if}
  {/snippet}
</Modal>

<style>
  .ctrls { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; flex-wrap: wrap; }
  .btn {
    padding: 4px 12px; font-size: 12px; border-radius: 3px; cursor: pointer;
    border: 1px solid var(--p-333333); background: var(--p-1a1a1a); color: var(--p-cccccc);
  }
  .btn.active { background: var(--p-1a2530); color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .btn.primary { background: var(--p-1a2530); color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .btn:disabled { opacity: 0.6; cursor: default; }
  .export { margin-left: auto; display: inline-flex; align-items: center; gap: 8px; }
  .export-msg { font-size: 11px; color: var(--p-99ccff); }
  .menu { position: relative; }
  .pop {
    position: absolute; top: calc(100% + 4px); right: 0; z-index: 20;
    display: flex; flex-direction: column; gap: 4px; padding: 6px;
    background: var(--p-161616); border: 1px solid var(--p-333333); border-radius: 5px;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.5); min-width: 140px;
  }
  .pop.wide { min-width: 220px; gap: 8px; padding: 10px; }
  .pop button:not(.btn) {
    background: transparent; border: none; color: var(--p-dddddd); text-align: left;
    padding: 5px 8px; font-size: 12px; border-radius: 3px; cursor: pointer;
  }
  .pop button:not(.btn):hover { background: var(--p-252525); }
  .pub-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; font-size: 12px; color: var(--p-aaaaaa); }
  .pub-row select {
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-dddddd);
    border-radius: 4px; padding: 3px 6px; font-size: 12px; min-width: 120px;
  }
  .legend { display: inline-flex; align-items: center; gap: 4px; font-size: 11px; color: var(--p-888888); }
  .sw { display: inline-block; width: 16px; height: 0; border-top: 2px solid #6a86a8; margin-left: 8px; }
  .sw.dashed { border-top-style: dashed; border-top-color: #8a8a8a; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .empty { text-align: center; padding: 30px 16px; }
  .empty .head { color: var(--p-bbbbbb); font-size: 14px; margin: 0 0 6px; }
  .empty .sub { color: var(--p-777777); font-size: 12px; line-height: 1.5; max-width: 460px; margin: 0 auto; }
  .empty code { font-family: var(--font-mono, monospace); color: var(--p-99ccff); font-size: 11px; }
  .diagram { overflow: auto; max-height: 62vh; border: 1px solid var(--p-1f1f1f); border-radius: 6px; background: var(--p-0c0c0c); }
  .tbl { cursor: pointer; }
  .tbl:focus { outline: none; }
  .tbl:hover rect:first-child { stroke: #4a4a4a; }
  .foot { margin-top: 8px; display: flex; align-items: center; gap: 12px; }
  .warn { font-size: 11px; color: var(--p-d4a54d); cursor: help; }
  .dir-legend { margin-top: 6px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .dir-chip { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; color: var(--p-999999); font-family: var(--font-mono, monospace); }
  .dir-chip .dot { width: 8px; height: 8px; border-radius: 2px; display: inline-block; }
</style>
