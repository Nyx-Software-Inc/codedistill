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
  // Self-maintaining architecture diagram (glass-box Phase 5, slice 5.3a). The
  // LLM drafts the conceptual structure; the human ratifies (proposed = dashed,
  // ratified = solid). Auto-laid-out by kind into columns; drag-layout + the
  // as-built drift overlay come in later slices.
  import * as api from '../lib/api';
  import type { ArchitectureNode, ArchitectureEdge, ArchKind, ArchitectureDelta } from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = { open: boolean; projectId: string; projectName?: string; onClose: () => void; onPublished?: () => void };
  let { open, projectId, projectName = '', onClose, onPublished }: Props = $props();

  let nodes = $state<ArchitectureNode[]>([]);
  let edges = $state<ArchitectureEdge[]>([]);
  let delta = $state<ArchitectureDelta | null>(null);
  let loading = $state(false);
  let drafting = $state(false);
  let err = $state('');
  let selected = $state('');
  let loadedFor = $state('');

  const COL_W = 180, ROW_H = 78, NODE_W = 140, NODE_H = 48, MARGIN = 30;
  const KIND_ORDER: ArchKind[] = ['ui', 'service', 'component', 'store', 'external'];
  const KIND_COLOR: Record<string, string> = {
    ui: '#99ccff', service: '#66cc66', component: '#aaaaaa', store: '#d4a54d', external: '#e07a7a',
  };

  // Live drag state. Persisted positions (pos_x/pos_y) win over auto-layout; a
  // drag-in-progress wins over both.
  let drag = $state<{ id: string; sx: number; sy: number; ox: number; oy: number; x: number; y: number; moved: boolean } | null>(null);

  type Placed = ArchitectureNode & { x: number; y: number; cx: number; cy: number };

  // Directory containers (async-draft slice 2): nodes cluster by their area's
  // top-level directory — the hierarchy already true in the code, not an
  // invented one. Groups of ≥2 render as collapsible containers (same mental
  // model as canvas groups); singletons and area-less nodes stay free.
  type Container = {
    key: string; x: number; y: number; w: number; h: number; cx: number; cy: number;
    collapsed: boolean; members: Placed[]; memberIds: string[];
    total: number; unenriched: number; missing: number;
  };
  const GAP = 14, C_PAD = 12, C_HEAD = 24, FLOW_W = 900;
  let collapsedGroups = $state<Set<string>>(new Set());
  function toggleGroup(key: string) {
    const next = new Set(collapsedGroups);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    collapsedGroups = next;
  }

  const layout = $derived.by<{ containers: Container[]; free: Placed[]; memberGroup: Map<string, string> }>(() => {
    const byKey = new Map<string, ArchitectureNode[]>();
    for (const n of nodes) {
      const area = (n.area ?? '').trim();
      if (!area) continue;
      const k = area.split('/')[0];
      const arr = byKey.get(k);
      if (arr) arr.push(n);
      else byKey.set(k, [n]);
    }
    for (const [k, mem] of [...byKey]) if (mem.length < 2) byKey.delete(k);
    const memberGroup = new Map<string, string>();
    for (const [k, mem] of byKey) for (const n of mem) memberGroup.set(n.id, k);

    // Simple flow layout: biggest containers first, wrap at FLOW_W.
    let fx = MARGIN, fy = MARGIN, rowH = 0;
    const place = (w: number, h: number) => {
      if (fx > MARGIN && fx + w > FLOW_W) { fx = MARGIN; fy += rowH + GAP; rowH = 0; }
      const at = { x: fx, y: fy };
      fx += w + GAP;
      rowH = Math.max(rowH, h);
      return at;
    };
    const kindRank = (n: ArchitectureNode) => Math.max(0, KIND_ORDER.indexOf(n.kind));

    const containers: Container[] = [];
    for (const [key, raw] of [...byKey.entries()].sort((a, b) => b[1].length - a[1].length)) {
      const members = [...raw].sort((a, b) => kindRank(a) - kindRank(b) || a.name.localeCompare(b.name));
      const collapsed = collapsedGroups.has(key);
      const unenriched = members.filter((m) => m.provenance === 'proposed' && !m.description && m.area).length;
      const missing = members.filter((m) => statusOf(m.id) === 'missing').length;
      const memberIds = members.map((m) => m.id);
      if (collapsed) {
        const w = NODE_W + 20, h = NODE_H + 8;
        const { x, y } = place(w, h);
        containers.push({ key, x, y, w, h, cx: x + w / 2, cy: y + h / 2, collapsed, members: [], memberIds, total: members.length, unenriched, missing });
      } else {
        const cols = Math.min(3, members.length);
        const rows = Math.ceil(members.length / cols);
        const w = C_PAD * 2 + cols * NODE_W + (cols - 1) * GAP;
        const h = C_HEAD + C_PAD * 2 + rows * NODE_H + (rows - 1) * GAP;
        const { x, y } = place(w, h);
        const placedMembers = members.map((m, i) => {
          const mx = x + C_PAD + (i % cols) * (NODE_W + GAP);
          const my = y + C_HEAD + C_PAD + Math.floor(i / cols) * (NODE_H + GAP);
          return { ...m, x: mx, y: my, cx: mx + NODE_W / 2, cy: my + NODE_H / 2 };
        });
        containers.push({ key, x, y, w, h, cx: x + w / 2, cy: y + h / 2, collapsed, members: placedMembers, memberIds, total: members.length, unenriched, missing });
      }
    }
    const free: Placed[] = [];
    for (const n of nodes) {
      if (memberGroup.has(n.id)) continue;
      let { x, y } = place(NODE_W, NODE_H);
      if (n.pos_x || n.pos_y) { x = n.pos_x; y = n.pos_y; }
      if (drag && drag.id === n.id) { x = drag.x; y = drag.y; }
      free.push({ ...n, x, y, cx: x + NODE_W / 2, cy: y + NODE_H / 2 });
    }
    return { containers, free, memberGroup };
  });

  // Every visible node (free + expanded members) — selection/detail works on these.
  const placed = $derived.by<Placed[]>(() => [
    ...layout.free,
    ...layout.containers.flatMap((c) => c.members),
  ]);

  // Edge endpoint resolution: a member of a COLLAPSED container anchors at the
  // container, and parallel edges between the same visible pair bundle into one
  // line with a count — so collapsing keeps the picture true, just coarser.
  type Bundle = { k: string; ax: number; ay: number; bx: number; by: number; label?: string; proposed: boolean; dangling: boolean; count: number };
  const renderEdges = $derived.by<Bundle[]>(() => {
    const anchor = new Map<string, { aid: string; cx: number; cy: number }>();
    for (const p of placed) anchor.set(p.id, { aid: 'n:' + p.id, cx: p.cx, cy: p.cy });
    for (const c of layout.containers) {
      if (!c.collapsed) continue;
      for (const id of c.memberIds) anchor.set(id, { aid: 'g:' + c.key, cx: c.cx, cy: c.cy });
    }
    const out = new Map<string, Bundle>();
    for (const e of edges) {
      const a = anchor.get(e.from_node);
      const b = anchor.get(e.to_node);
      if (!a || !b || a.aid === b.aid) continue;
      const k = a.aid + '->' + b.aid;
      const dangling = statusOf(e.from_node) === 'missing' || statusOf(e.to_node) === 'missing';
      const cur = out.get(k);
      if (cur) {
        cur.count++;
        cur.proposed = cur.proposed || e.provenance === 'proposed';
        cur.dangling = cur.dangling || dangling;
        if (cur.label && e.label && cur.label !== e.label) cur.label = undefined;
      } else {
        out.set(k, { k, ax: a.cx, ay: a.cy, bx: b.cx, by: b.cy, label: e.label, proposed: e.provenance === 'proposed', dangling, count: 1 });
      }
    }
    return [...out.values()];
  });

  const width = $derived(Math.max(
    MARGIN * 2 + NODE_W,
    ...layout.containers.map((c) => c.x + c.w + MARGIN),
    ...layout.free.map((p) => p.x + NODE_W + MARGIN),
  ));
  const height = $derived(Math.max(
    MARGIN * 2 + NODE_H,
    ...layout.containers.map((c) => c.y + c.h + MARGIN),
    ...layout.free.map((p) => p.y + NODE_H + MARGIN),
  ));

  function startDrag(e: PointerEvent, n: Placed) {
    e.preventDefault();
    selected = n.id;
    drag = { id: n.id, sx: e.clientX, sy: e.clientY, ox: n.x, oy: n.y, x: n.x, y: n.y, moved: false };
    window.addEventListener('pointermove', onDragMove);
    window.addEventListener('pointerup', onDragEnd, { once: true });
  }
  function onDragMove(e: PointerEvent) {
    if (!drag) return;
    // SVG renders 1:1 (no CSS scale), so a screen-pixel delta is an SVG delta.
    const dx = e.clientX - drag.sx;
    const dy = e.clientY - drag.sy;
    drag = { ...drag, x: Math.max(0, drag.ox + dx), y: Math.max(0, drag.oy + dy), moved: drag.moved || Math.abs(dx) + Math.abs(dy) > 3 };
  }
  async function onDragEnd() {
    window.removeEventListener('pointermove', onDragMove);
    const d = drag;
    drag = null;
    if (!d || !d.moved) return; // a click, not a drag
    const n = nodes.find((x) => x.id === d.id);
    if (n) {
      n.pos_x = d.x;
      n.pos_y = d.y;
    }
    try {
      await api.updateArchNode(d.id, { pos_x: d.x, pos_y: d.y });
    } catch (e) {
      err = String(e);
    }
  }
  const selectedNode = $derived(placed.find((p) => p.id === selected) ?? null);
  const proposedCount = $derived(nodes.filter((n) => n.provenance === 'proposed').length);

  $effect(() => {
    if (open && projectId && loadedFor !== projectId) {
      loadedFor = projectId;
      void load();
    }
    if (!open) loadedFor = '';
  });

  function apply(r: api.ArchitectureResponse) {
    nodes = r.nodes;
    edges = r.edges;
    delta = r.delta ?? null;
    job = r.draft_job ?? null;
    unenriched = r.unenriched_nodes ?? 0;
    // First sight of a project's diagram picks the collapse default: big
    // diagrams (>24 nodes) open with containers collapsed — a readable
    // top-level view — small ones fully expanded. User toggles stick after.
    if (groupDefaultsFor !== projectId) {
      groupDefaultsFor = projectId;
      const keys = new Map<string, number>();
      for (const n of r.nodes) {
        const area = (n.area ?? '').trim();
        if (!area) continue;
        const k = area.split('/')[0];
        keys.set(k, (keys.get(k) ?? 0) + 1);
      }
      collapsedGroups = r.nodes.length > 24
        ? new Set([...keys.entries()].filter(([, c]) => c >= 2).map(([k]) => k))
        : new Set();
    }
  }
  let groupDefaultsFor = $state('');

  // Async draft: the job runs server-side (closing this panel/browser doesn't
  // touch it) — while it's running we poll so nodes visibly enrich in place.
  let job = $state<api.ArchDraftJob | null>(null);
  let unenriched = $state(0);
  $effect(() => {
    if (!open || !job?.running) return;
    const t = setInterval(() => void quietReload(), 2500);
    return () => clearInterval(t);
  });
  async function quietReload() {
    try {
      apply(await api.getArchitecture(projectId));
    } catch {
      /* transient poll failure — next tick retries */
    }
  }
  async function cancelDraft() {
    try {
      await api.cancelArchDraft(projectId);
      await quietReload();
    } catch (e) { err = String(e); }
  }
  const statusOf = (id: string) => delta?.node_status?.[id] ?? '';
  // Missing (claims code that isn't there) paints red regardless of kind.
  const strokeOf = (n: ArchitectureNode) =>
    statusOf(n.id) === 'missing' ? '#e07a7a' : (KIND_COLOR[n.kind] ?? '#888');
  const missingCount = $derived(
    Object.values(delta?.node_status ?? {}).filter((s) => s === 'missing').length,
  );
  async function load() {
    loading = true;
    err = '';
    try {
      apply(await api.getArchitecture(projectId));
    } catch (e) { err = String(e); } finally { loading = false; }
  }
  async function draft(mode: 'auto' | 'redraft' = 'auto', scope?: string) {
    if (drafting || job?.running) return;
    if (mode === 'redraft' && nodes.length > 0 &&
        !await confirmDialog('Re-draft from scratch? Proposed nodes (including characterized ones) are replaced; ratified nodes survive.', { title: 'Re-draft architecture', confirmLabel: 'Re-draft', danger: true })) {
      return;
    }
    drafting = true;
    err = '';
    try {
      apply(await api.draftArchitecture(projectId, mode, scope));
    } catch (e) { err = `Draft failed: ${e}`; } finally { drafting = false; }
  }
  function fmtEta(sec?: number): string {
    if (!sec || sec <= 0) return '';
    if (sec < 90) return '≈1 min';
    return `≈${Math.round(sec / 60)} min`;
  }
  async function ratifyAll() {
    try { apply(await api.ratifyArchitecture(projectId)); } catch (e) { err = String(e); }
  }
  async function ratifyOne(n: ArchitectureNode) {
    try { await api.updateArchNode(n.id, { provenance: 'ratified' }); await load(); } catch (e) { err = String(e); }
  }
  async function removeNode(n: ArchitectureNode) {
    try { await api.deleteArchNode(n.id); selected = ''; await load(); } catch (e) { err = String(e); }
  }

  // --- Export / Publish (architecture panel actions) ---
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

  // Serialize the live <svg> to a standalone, self-contained string. The diagram
  // uses inline fill/stroke attributes (not CSS classes), so it rasterizes
  // faithfully; we just pin xmlns + a font so it renders outside the app.
  function serializeSVG(): string {
    if (!svgEl) return '';
    const clone = svgEl.cloneNode(true) as SVGSVGElement;
    clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
    clone.setAttribute('font-family', 'system-ui, sans-serif');
    return new XMLSerializer().serializeToString(clone);
  }

  // Rasterize the SVG to a PNG File at 2× for crispness, on a dark background.
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
      const file = await svgToPngFile('architecture.png');
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
  // Open-format sharing: mermaid renders natively on GitHub/GitLab/Notion —
  // paste into a README or PR and the diagram lives there.
  async function copyMermaid() {
    copyMenu = false;
    try {
      await navigator.clipboard.writeText(mermaidDoc());
      flash('Copied mermaid — paste into GitHub/Notion');
    } catch {
      flash('Clipboard blocked');
    }
  }
  async function copyStructurizr() {
    copyMenu = false;
    try {
      await navigator.clipboard.writeText(structurizrDoc());
      flash('Copied Structurizr DSL (C4)');
    } catch {
      flash('Clipboard blocked');
    }
  }

  // Structurizr DSL export — the C4 architecture-as-code standard. Each
  // component maps to a C4 container in one software system: kind + provenance
  // travel as tags, the real directory travels as the technology slot, and the
  // as-built honesty survives (a "missing" node exports with a missing-code
  // tag). Save as workspace.dsl → Structurizr / structurizr-cli (which also
  // converts onward to PlantUML, D2, Ilograph…).
  function structurizrDoc(): string {
    const sid = (id: string) => 'n' + id.replace(/[^a-zA-Z0-9]/g, '');
    const esc = (s: string) => (s || '').replace(/"/g, "'").replace(/\n/g, ' ');
    const ids = new Set(nodes.map((n) => n.id));
    const L: string[] = [];
    L.push(`workspace "${esc(projectName || 'CodeDistill project')}" "Exported from CodeDistill — drawn from the real repository; edges are structural, not invented." {`);
    L.push('  model {');
    L.push(`    sys = softwareSystem "${esc(projectName || 'System')}" {`);
    for (const n of nodes) {
      const tags: string[] = [n.kind, n.provenance];
      if (statusOf(n.id) === 'missing') tags.push('missing-code');
      L.push(`      ${sid(n.id)} = container "${esc(n.name)}" "${esc(n.description ?? '')}" "${esc(n.area ?? '')}" {`);
      L.push(`        tags ${tags.map((t) => `"${t}"`).join(' ')}`);
      L.push('      }');
    }
    L.push('    }');
    for (const e of edges) {
      if (ids.has(e.from_node) && ids.has(e.to_node)) {
        L.push(`    ${sid(e.from_node)} -> ${sid(e.to_node)} "${esc(e.label || 'uses')}"`);
      }
    }
    L.push('  }');
    L.push('  views {');
    L.push('    container sys "Containers" {');
    L.push('      include *');
    L.push('      autolayout lr');
    L.push('    }');
    L.push('    styles {');
    for (const [kind, color] of Object.entries(KIND_COLOR)) {
      L.push(`      element "${kind}" { stroke "${color}" color "${color}" }`);
    }
    L.push('      element "proposed" { border dashed }');
    L.push('      element "missing-code" { stroke "#e07a7a" color "#e07a7a" }');
    L.push('    }');
    L.push('  }');
    L.push('}');
    return L.join('\n') + '\n';
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

  // Build a portable mermaid graph from the nodes + edges (renders on GitHub,
  // mermaid.live, etc.; shows as a code block in-app until a renderer is added).
  function mermaidDoc(): string {
    const mid = (id: string) => 'n' + id.replace(/[^a-zA-Z0-9]/g, '');
    const esc = (s: string) => (s || '').replace(/"/g, "'");
    const lines = ['```mermaid', 'graph TD'];
    // Export carries the FULL diagram regardless of on-screen collapse state.
    const ids = new Set(nodes.map((n) => n.id));
    for (const n of nodes) lines.push(`  ${mid(n.id)}["${esc(n.name)}"]`);
    for (const e of edges) {
      if (ids.has(e.from_node) && ids.has(e.to_node)) {
        lines.push(`  ${mid(e.from_node)} -->${e.label ? `|${esc(e.label)}|` : ''} ${mid(e.to_node)}`);
      }
    }
    lines.push('```');
    return `# Architecture — ${projectName}\n\n${lines.join('\n')}\n`;
  }

  async function doPublish() {
    if (!pubPadId) return;
    exporting = true;
    try {
      if (pubFormat === 'image') {
        const file = await svgToPngFile(`architecture-${projectName || 'project'}.png`);
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

<Modal open={open} title={`Architecture — ${projectName}`} {onClose} width="940px">
  {#snippet children()}
    <div class="ctrls">
      {#if job?.running}
        <span class="draft-progress" title="Runs on the server — you can close this window and come back">
          ✨ Characterizing{job.scope ? ` ${job.scope}/` : ''}… {job.done}/{job.total}
          {#if fmtEta(job.eta_seconds)}<span class="eta">{fmtEta(job.eta_seconds)}</span>{/if}
        </span>
        <button class="btn" onclick={cancelDraft}>Cancel</button>
      {:else}
        {#if nodes.length === 0}
          <button class="btn primary" disabled={drafting} onclick={() => draft()}>
            {drafting ? 'Drafting…' : '✨ Draft with AI'}
          </button>
        {:else}
          {#if unenriched > 0}
            <button class="btn primary" disabled={drafting} onclick={() => draft()}
              title="Continue characterizing the remaining skeleton nodes (survives restarts)">
              ▶ Resume characterization ({unenriched} left)
            </button>
          {/if}
          <button class="btn" disabled={drafting} onclick={() => draft('redraft')}
            title="Rebuild the diagram from the current code structure">
            {drafting ? 'Drafting…' : '↻ Re-draft'}
          </button>
        {/if}
      {/if}
      {#if proposedCount > 0}
        <button class="btn" onclick={ratifyAll}>Ratify all ({proposedCount})</button>
      {/if}
      <span class="legend">
        <i class="sw solid"></i>ratified
        <i class="sw dashed"></i>proposed
        {#if delta}<i class="sw red"></i>path not found{/if}
      </span>
      {#if err}<span class="err">{err}</span>{/if}

      {#if nodes.length > 0}
        <div class="export">
          {#if exportMsg}<span class="export-msg">{exportMsg}</span>{/if}
          <div class="menu">
            <button class="btn" onclick={() => { copyMenu = !copyMenu; publishOpen = false; }}>⎘ Copy ▾</button>
            {#if copyMenu}
              <div class="pop">
                <button onclick={copyPng}>Copy as PNG</button>
                <button onclick={copySvg}>Copy as SVG</button>
                <button onclick={copyMermaid} title="Renders natively on GitHub, GitLab, Notion…">Copy as Mermaid</button>
                <button onclick={copyStructurizr} title="C4 architecture-as-code — open in Structurizr, convert with structurizr-cli">Copy as Structurizr DSL</button>
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
                    <option value="mermaid">Doc (mermaid)</option>
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

    {#if loading && nodes.length === 0}
      <p class="muted">Loading…</p>
    {:else if nodes.length === 0}
      <div class="empty">
        <p class="head">No architecture diagram yet.</p>
        <p class="sub">
          Let the AI draft the conceptual structure from your project brain, use
          cases, and code map — then ratify what's right. Proposed nodes render
          dashed until you accept them.
        </p>
      </div>
    {:else}
      <div class="diagram">
        <svg bind:this={svgEl} viewBox="0 0 {width} {height}" {width} {height}>
          <defs>
            <marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
              <path d="M0,0 L7,3 L0,6 Z" fill="#666"></path>
            </marker>
          </defs>
          {#each renderEdges as e (e.k)}
            <line
              x1={e.ax} y1={e.ay} x2={e.bx} y2={e.by}
              stroke={e.dangling ? '#e07a7a' : '#555'} stroke-width={e.count > 1 ? Math.min(3, 1 + e.count * 0.4) : 1}
              stroke-dasharray={e.proposed ? '4 3' : 'none'}
              marker-end="url(#arrow)"
            ></line>
            {#if e.count > 1}
              <text x={(e.ax + e.bx) / 2} y={(e.ay + e.by) / 2 - 3} fill="#888" font-size="9" text-anchor="middle">×{e.count}</text>
            {:else if e.label}
              <text x={(e.ax + e.bx) / 2} y={(e.ay + e.by) / 2 - 3} fill="#888" font-size="9" text-anchor="middle">{e.label}</text>
            {/if}
          {/each}
          {#each layout.containers as c (c.key)}
            {#if c.collapsed}
              <!-- Collapsed container: one super-node standing in for its members. -->
              <g class="node" role="button" tabindex="0"
                onclick={() => toggleGroup(c.key)}
                onkeydown={(ev) => { if (ev.key === 'Enter') toggleGroup(c.key); }}>
                <rect x={c.x} y={c.y} width={c.w} height={c.h} rx="8"
                  fill="#10161c" stroke={c.missing > 0 ? '#e07a7a' : '#3d5a78'} stroke-width="1.5"></rect>
                <text x={c.x + 10} y={c.y + 20} fill="#cfe6ff" font-size="12" font-weight="600">▸ {c.key}/</text>
                <text x={c.x + 10} y={c.y + 38} fill="#7d97b0" font-size="9">
                  {c.total} components{c.unenriched > 0 ? ` · ${c.unenriched} pending` : ''}{c.missing > 0 ? ` · ⚠${c.missing}` : ''}
                </text>
              </g>
            {:else}
              <!-- Expanded container frame; members render as normal nodes inside. -->
              <rect x={c.x} y={c.y} width={c.w} height={c.h} rx="8"
                fill="rgba(80,130,180,0.05)" stroke="#2c3e50" stroke-width="1" stroke-dasharray="2 3"></rect>
              <g class="node" role="button" tabindex="0"
                onclick={() => toggleGroup(c.key)}
                onkeydown={(ev) => { if (ev.key === 'Enter') toggleGroup(c.key); }}>
                <text x={c.x + 10} y={c.y + 16} fill="#9fb8d0" font-size="11" font-weight="600">▾ {c.key}/</text>
              </g>
              <text x={c.x + c.w - 10} y={c.y + 16} fill="#5f7f9f" font-size="9" text-anchor="end">{c.total}</text>
              {#if c.unenriched > 0 && !job?.running}
                <!-- Scope-to-subtree (slice 3): characterize this container first. -->
                <g class="node" role="button" tabindex="0"
                  onclick={(ev) => { ev.stopPropagation(); void draft('auto', c.key); }}
                  onkeydown={(ev) => { if (ev.key === 'Enter') void draft('auto', c.key); }}>
                  <text x={c.x + c.w - 24} y={c.y + 16} fill="#99ccff" font-size="10" text-anchor="end">✨ {c.unenriched}</text>
                  <title>Characterize this directory's {c.unenriched} pending component{c.unenriched === 1 ? '' : 's'} first</title>
                </g>
              {/if}
            {/if}
          {/each}
          {#each placed as n (n.id)}
            {@const inContainer = layout.memberGroup.has(n.id)}
            <g class="node" role="button" tabindex="0"
              onpointerdown={(ev) => { if (inContainer) selected = n.id; else startDrag(ev, n); }}
              onkeydown={(ev) => { if (ev.key === 'Enter') selected = n.id; }}>
              <rect
                x={n.x} y={n.y} width={NODE_W} height={NODE_H} rx="6"
                fill="#141414" stroke={strokeOf(n)}
                stroke-width={selected === n.id ? 2.5 : statusOf(n.id) === 'missing' ? 2 : 1}
                stroke-dasharray={n.provenance === 'proposed' ? '5 3' : 'none'}
              ></rect>
              <text x={n.x + NODE_W / 2} y={n.y + 20} fill="#ddd" font-size="12" text-anchor="middle">{n.name}</text>
              <text x={n.x + NODE_W / 2} y={n.y + 36} fill={strokeOf(n)} font-size="9" text-anchor="middle">
                {statusOf(n.id) === 'missing' ? '⚠ path not found' : n.kind}
              </text>
            </g>
          {/each}
        </svg>
      </div>

      {#if delta && (missingCount > 0 || delta.uncovered_areas.length > 0)}
        <div class="delta">
          {#if missingCount > 0}
            <span class="delta-bad" title="The node's code-path (its “area”) doesn't match any folder in the repo — wrong/renamed path, or the code isn't in one folder. Edit the node's area to a real path, or clear it.">⚠ {missingCount} component{missingCount > 1 ? 's' : ''} point at a code path that doesn't exist</span>
          {/if}
          {#if delta.uncovered_areas.length > 0}
            <div class="uncovered">
              <span class="uncovered-lbl">Code areas with no component:</span>
              {#each delta.uncovered_areas as a (a)}<code class="area-tag">{a}</code>{/each}
            </div>
          {/if}
        </div>
      {/if}

      {#if selectedNode}
        <div class="detail">
          <div class="detail-head">
            <strong>{selectedNode.name}</strong>
            <span class="k" style:color={KIND_COLOR[selectedNode.kind]}>{selectedNode.kind}</span>
            {#if selectedNode.provenance === 'proposed'}<span class="prop">proposed</span>{/if}
          </div>
          {#if selectedNode.description}<p class="desc">{selectedNode.description}</p>{/if}
          {#if selectedNode.area}<p class="area">↳ {selectedNode.area}</p>{/if}
          <div class="detail-actions">
            {#if selectedNode.provenance === 'proposed'}
              <button class="btn" onclick={() => ratifyOne(selectedNode)}>Ratify</button>
            {/if}
            <button class="btn danger" onclick={() => removeNode(selectedNode)}>Delete</button>
          </div>
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
  .btn.primary { background: var(--p-1a2530); color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .btn.danger:hover { color: var(--p-e07a7a); border-color: var(--p-4a2020); }
  .btn:disabled { opacity: 0.6; cursor: default; }
  /* Export / Publish controls, pushed to the right of the toolbar. */
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
  .draft-progress { font-size: 12px; color: var(--p-99ccff); display: inline-flex; align-items: center; gap: 4px; }
  .draft-progress .eta { color: var(--p-7d97b0); font-size: 11px; }
  .sw { display: inline-block; width: 14px; height: 0; border-top: 2px solid var(--p-888888); margin-left: 8px; }
  .sw.dashed { border-top-style: dashed; }
  .sw.red { border-top-color: var(--p-e07a7a); }
  .delta { margin-top: 8px; display: flex; flex-direction: column; gap: 4px; }
  .delta-bad { font-size: 12px; color: var(--p-e07a7a); }
  .uncovered { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; font-size: 11px; color: var(--p-888888); }
  .uncovered-lbl { color: var(--p-888888); }
  .area-tag {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    color: var(--p-d4a54d);
    background: var(--p-141414);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 0 4px;
  }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .empty { text-align: center; padding: 30px 16px; }
  .empty .head { color: var(--p-bbbbbb); font-size: 14px; margin: 0 0 6px; }
  .empty .sub { color: var(--p-777777); font-size: 12px; line-height: 1.5; max-width: 440px; margin: 0 auto; }
  .diagram { overflow: auto; max-height: 60vh; border: 1px solid var(--p-1f1f1f); border-radius: 6px; background: var(--p-0c0c0c); }
  .node { cursor: grab; }
  .node:active { cursor: grabbing; }
  .node:focus { outline: none; }
  .detail {
    margin-top: 10px; padding: 8px 10px; border-radius: 6px;
    background: var(--p-141414); border: 1px solid var(--p-262626);
  }
  .detail-head { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--p-eeeeee); }
  .detail-head .k { font-size: 10px; text-transform: uppercase; letter-spacing: 0.4px; }
  .prop { font-size: 9px; color: var(--p-d4a54d); border: 1px solid var(--p-333333); border-radius: 3px; padding: 0 4px; }
  .desc { margin: 6px 0 2px; font-size: 12px; color: var(--p-bbbbbb); line-height: 1.5; }
  .area { margin: 0; font-size: 11px; color: var(--p-99ccff); font-family: var(--font-mono, monospace); }
  .detail-actions { margin-top: 8px; display: flex; gap: 6px; }
</style>
