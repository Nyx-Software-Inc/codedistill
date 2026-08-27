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
  import { alertDialog } from '../lib/dialog';
  import type { ScratchpadItem, CodeAnchor } from '../lib/types';
  import { copyToClipboard, itemToMarkdown, plainContent } from '../lib/markdown';
  import { draftingCriteria } from '../lib/draftingStore';
  import { withNumberPrefix } from '../lib/itemNumberLabel';
  import { dueDateClass, type DueDateColorsConfig } from '../lib/dueDateColors';
  import Icon from './Icon.svelte';

  type Layout = { col: number; row: number; w: number; h: number };
  type Props = {
    items: ScratchpadItem[];
    onLayoutChange: (updates: Record<string, Layout>) => void;
    onDelete: (item: ScratchpadItem) => void;
    onHide: (item: ScratchpadItem) => void;
    onEdit: (item: ScratchpadItem) => void;
    onMoveItem: (item: ScratchpadItem) => void;
    // Re-enqueue a stuck capture (unprocessed/failed) for classification.
    onReprocess: (item: ScratchpadItem) => void;
    // Triggered from the in-card pending-review banner. The agent's
    // proposed_category is offered as the headline Accept option; the
    // dropdown lets the user reclassify to any of the other categories.
    onAccept: (item: ScratchpadItem) => void | Promise<void>;
    onReclassify: (item: ScratchpadItem, category: 'todo' | 'bug' | 'kb' | 'use_case') => void;
    // Dedup banner: when the agent flagged a possible duplicate,
    // onOpenSimilar navigates the user to that other item; onDismiss-
    // Similar clears the candidate-match fields ("not a duplicate").
    onOpenSimilar: (similarToId: string) => void;
    onDismissSimilar: (item: ScratchpadItem) => void;
    // onGroupSimilar accepts the suggestion and groups the pair.
    onGroupSimilar: (item: ScratchpadItem) => void;
    // External "please flash this card" — set when the user clicks a code-
    // canvas anchor that resolves to a scratchpad item. App owns the value;
    // this component flashes the matching card and scrolls it into view.
    flashItemId?: string | null;
    // Per-item anchor map. Cards render a small anchor badge in the header
    // when their id has at least one kind=file entry. Click → emits
    // onAnchorBadge with the first file anchor.
    itemAnchors?: Record<string, CodeAnchor[]>;
    onAnchorBadge: (anchor: CodeAnchor) => void;
    // Hover preview — emits the badge anchor when the mouse enters its
    // button, null when it leaves. App forwards it to CodeViewer for the
    // reverse-hover tint.
    onAnchorBadgeHover: (anchor: CodeAnchor | null) => void;
    // Set of source item ids whose every derived item (todo/bug/kb/
    // use_case) is in a terminal state. Cards in this set get faded
    // and strikethrough so the canvas reflects downstream completion.
    // Computed in App.svelte from the per-pad derived lists.
    doneSourceIds?: Set<string>;
    // Per-card derived-item status (source item id -> kind + status).
    // Cards show a small status chip when the derived item has moved
    // past its initial open state — in-progress, terminal, or dropped —
    // so the canvas reflects lifecycle without opening the drawer.
    derivedStatus?: Map<string, { kind: string; status: string; subject?: string; number?: number }>;
    // Canvas rework slice B: source item id -> latest activity-log note summary,
    // shown as the card's pulse line (supersedes the frozen annotations blob).
    latestNoteBySource?: Map<string, string>;
    // Slice 6 — multi-select state owned by App so the Group
    // button (in ScratchpadPane) can read it. Empty set = no
    // selection; the Group button hides.
    selectedItemIds?: Set<string>;
    onSelectionChange?: (next: Set<string>) => void;
    // Toggle a group's collapsed state. Routes to api.updateItem
    // via the parent.
    onToggleGroupCollapse?: (group: ScratchpadItem) => void;
    // Delete a group (orphans its children — backend handles it).
    onDeleteGroup?: (group: ScratchpadItem) => void;
    // Save a note onto a group frame (writes the group item's
    // annotations field — same column agents write running notes to).
    onSaveGroupNote?: (group: ScratchpadItem, note: string) => void;
    // UC-49: rename a group (writes the group item's name field).
    onRenameGroup?: (group: ScratchpadItem, name: string) => void;
    // UC-18: per-type card background tinting. Undefined or
    // enabled=false → default backgrounds.
    typeColors?: { enabled: boolean; colors: Record<string, string> };
    // UC-48: due-date header coloring. dueDateBySource maps a source item id
    // -> its derived todo's due_date (ISO); dueDateColors gates the feature.
    dueDateBySource?: Map<string, string>;
    dueDateColors?: DueDateColorsConfig;
    // Canvas rework C3: source item id -> its derived work item's tags. When a
    // card has a derived work item, we show ITS tags (editable on the modal)
    // instead of the frozen source #hashtags.
    tagsBySource?: Map<string, string[]>;
  };
  let {
    items, onLayoutChange, onDelete, onHide, onEdit, onMoveItem, onReprocess,
    onAccept, onReclassify, onOpenSimilar, onDismissSimilar, onGroupSimilar,
    flashItemId = null, itemAnchors = {}, onAnchorBadge, onAnchorBadgeHover,
    doneSourceIds,
    derivedStatus,
    latestNoteBySource,
    selectedItemIds = new Set<string>(),
    onSelectionChange = () => {},
    onToggleGroupCollapse = () => {},
    onDeleteGroup = () => {},
    onSaveGroupNote = () => {},
    onRenameGroup = () => {},
    typeColors,
    dueDateBySource,
    dueDateColors,
    tagsBySource,
  }: Props = $props();

  // Group derivation: build {group, children, bbox} per group id.
  // The bbox spans the min/max grid cells of children — used to
  // position the overlay frame. Groups with zero children render
  // a placeholder (shouldn't happen in practice because the API
  // auto-deletes empty groups, but defensive for race conditions).
  type GroupRenderModel = {
    group: ScratchpadItem;
    children: ScratchpadItem[];
    bbox: { col: number; row: number; w: number; h: number };
  };
  let groupModels = $derived.by<GroupRenderModel[]>(() => {
    const byParent = new Map<string, ScratchpadItem[]>();
    for (const it of items) {
      if (!it.group_id || it.content_type === 'group') continue;
      const arr = byParent.get(it.group_id) ?? [];
      arr.push(it);
      byParent.set(it.group_id, arr);
    }
    const out: GroupRenderModel[] = [];
    for (const g of items) {
      if (g.content_type !== 'group') continue;
      const children = byParent.get(g.id) ?? [];
      let minCol = g.grid_col;
      let minRow = g.grid_row;
      let maxCol = g.grid_col + g.grid_w;
      let maxRow = g.grid_row + g.grid_h;
      if (children.length > 0) {
        minCol = Math.min(...children.map((c) => c.grid_col));
        minRow = Math.min(...children.map((c) => c.grid_row));
        maxCol = Math.max(...children.map((c) => c.grid_col + c.grid_w));
        maxRow = Math.max(...children.map((c) => c.grid_row + c.grid_h));
      }
      out.push({
        group: g,
        children,
        bbox: { col: minCol, row: minRow, w: maxCol - minCol, h: maxRow - minRow },
      });
    }
    return out;
  });
  // Set of group ids that are currently collapsed — items with a
  // group_id in this set are hidden from the canvas (the group
  // frame shrinks to just its header).
  let collapsedGroupIds = $derived(
    new Set(groupModels.filter((m) => m.group.collapsed).map((m) => m.group.id)),
  );

  // Maps any derived-item status (across all four kinds) to a chip
  // tone. 'open' statuses render no chip — a fresh item's state is
  // implied by the category arrow already on the bar.
  function statusTone(status: string): 'busy' | 'done' | 'dropped' | 'open' {
    if (status === 'in_progress' || status === 'in-progress' || status === 'investigating') return 'busy';
    if (['complete', 'fixed', 'verified', 'closed', 'completed', 'deprecated'].includes(status)) return 'done';
    if (['abandoned', 'rejected', 'not_a_bug', 'wont_fix', 'duplicate'].includes(status)) return 'dropped';
    return 'open';
  }

  // Group note popover (UC-16). One open at a time; Esc closes
  // without saving, Ctrl/Cmd+Enter saves.
  let noteEditId = $state<string | null>(null);
  let noteDraft = $state('');
  function openGroupNote(group: ScratchpadItem) {
    noteEditId = group.id;
    noteDraft = group.annotations ?? '';
  }
  function saveGroupNote(group: ScratchpadItem) {
    onSaveGroupNote(group, noteDraft.trim());
    noteEditId = null;
  }
  function onNoteKey(e: KeyboardEvent, group: ScratchpadItem) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      noteEditId = null;
    } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      saveGroupNote(group);
    }
  }

  // UC-49: double-click the group title to rename it inline. Enter/blur
  // commits, Esc cancels.
  let renameId = $state<string | null>(null);
  let renameDraft = $state('');
  function startRename(group: ScratchpadItem) {
    renameId = group.id;
    renameDraft = group.name ?? '';
  }
  function saveRename(group: ScratchpadItem) {
    if (renameId !== group.id) return; // already closed (e.g. Esc then blur)
    renameId = null;
    onRenameGroup(group, renameDraft);
  }
  function onRenameKey(e: KeyboardEvent, group: ScratchpadItem) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      renameId = null;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      saveRename(group);
    }
  }

  // Canvas rework slice A: the card's title reflects the derived work item's
  // subject (the living thing), falling back to the source's own name/label for
  // items that haven't derived a work item yet (unclassified/pending/skipped).
  function cardTitle(item: ScratchpadItem): string {
    const d = derivedStatus?.get(item.id);
    const base = d?.subject || item.name || '';
    return withNumberPrefix(base, d?.kind, d?.number); // UC-65 number prefix (user pref)
  }

  // Canvas rework C3: prefer the derived work item's tags (editable on the
  // detail modal); fall back to the source's own #hashtags for cards that
  // haven't derived a work item yet.
  function cardTags(item: ScratchpadItem): string[] {
    return tagsBySource?.get(item.id) ?? item.tags ?? [];
  }

  // UC-18: card background tint by effective type. ~30% blend with
  // the theme card background keeps text readable under any picked
  // color, in both themes.
  function cardTint(item: ScratchpadItem): string {
    if (!typeColors?.enabled || item.content_type === 'group') return '';
    const cat = item.classification_override || item.proposed_category || 'unclassified';
    const c = typeColors.colors[cat] ?? typeColors.colors['unclassified'];
    if (!c) return '';
    return `background: color-mix(in srgb, ${c} 30%, var(--p-222222));`;
  }

  // UC-48: minute-ticking clock so headers re-color as deadlines approach
  // without needing an interaction to force a re-render.
  let nowMs = $state(Date.now());
  $effect(() => {
    const id = setInterval(() => (nowMs = Date.now()), 60_000);
    return () => clearInterval(id);
  });

  // dueBarClass returns the header-bar state class ('' | 'due-warn' |
  // 'due-urgent') for an item's due date (its derived todo's), or '' when the
  // feature is off, it's a group, or there's no due date.
  // Terminal-state items get no deadline heat: once the work is complete/
  // abandoned/fixed/rejected/etc. the due date is moot — coloring it red/yellow
  // is noise (and would clash with the done-fade). doneSourceIds is the same
  // "every derived item is in a terminal state" signal used for that fade.
  function dueBarClass(item: ScratchpadItem): string {
    if (!dueDateColors?.enabled || item.content_type === 'group') return '';
    if (doneSourceIds?.has(item.id)) return '';
    return dueDateClass(dueDateBySource?.get(item.id), nowMs);
  }

  // UC-35: immediate feedback on Accept. Track in-flight accepts per item so
  // the button disables + shows "Accepting…" the instant it's clicked, even
  // when the classifier is busy with other items — so it never feels like a
  // mis-click. Per-item (a Set) so accepting one doesn't freeze the rest.
  let accepting = $state<Set<string>>(new Set());
  async function accept(item: ScratchpadItem) {
    if (accepting.has(item.id)) return;
    accepting = new Set(accepting).add(item.id);
    try {
      await onAccept(item);
    } finally {
      const next = new Set(accepting);
      next.delete(item.id);
      accepting = next;
    }
  }

  function toggleSelection(id: string) {
    const next = new Set(selectedItemIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onSelectionChange(next);
  }

  function similarTitle(id: string): string {
    const found = items.find((i) => i.id === id);
    if (!found) return '(item in another scratchpad)';
    if (found.name) return found.name;
    return found.content.slice(0, 60);
  }

  // Pick a glyph for a file content_type item. Falls back to a
  // generic page icon for anything not in the small known set —
  // intentionally keeping the matrix tiny rather than mapping every
  // MIME variant. Operators who want richer iconography can iterate
  // here later.
  function fileIcon(mime?: string, name?: string): string {
    const m = (mime || '').toLowerCase();
    if (m === 'application/pdf') return '📕';
    if (m === 'application/zip' || m === 'application/x-tar' || m === 'application/gzip') return '🗜️';
    if (m === 'application/json') return '🗎';
    if (m.startsWith('text/csv')) return '📊';
    if (m.startsWith('text/markdown') || (name && /\.md$/i.test(name))) return '📝';
    if (m.startsWith('text/')) return '📄';
    return '📎';
  }

  // hasOG: a link item has enough OpenGraph data to render a rich
  // preview card. We require ANY of title/description/image — a
  // fetch that returned nothing (og_fetched_at set, others empty)
  // falls back to the plain content rendering, which is just the
  // bare URL.
  function hasOG(it: ScratchpadItem): boolean {
    return !!(it.og_title || it.og_description || it.og_image_sha);
  }

  // shortHost extracts the host for the small caption line under
  // the OG card. Strips leading www. and trailing port; never
  // throws, never blank-renders.
  function shortHost(raw: string): string {
    if (!raw) return '';
    try {
      const u = new URL(raw);
      return u.hostname.replace(/^www\./, '');
    } catch {
      return raw;
    }
  }

  // compositePreview strips markdown to a plain-text snippet for
  // the card. Image refs become "[image]" so the user sees content
  // density without rendering inline blobs in a tiny grid cell.
  // Full rendering happens in the CompositeEditor's preview pane.
  function compositePreview(content: string): string {
    if (!content) return '';
    return content
      .replace(/!\[[^\]]*\]\([^)]+\)/g, '[image]')   // images
      .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')         // links → text
      .replace(/^#{1,6}\s+/gm, '')                    // heading hashes
      .replace(/[*_`]/g, '')                          // emphasis chars
      .replace(/\n+/g, ' ')                            // collapse newlines
      .trim()
      .slice(0, 200);
  }

  // Human-readable byte size. Uses 1024-step units (KiB/MiB) since
  // file sizes are usually thought of in binary multiples by the
  // people staring at them.
  function formatBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    const units = ['KiB', 'MiB', 'GiB', 'TiB'];
    let v = n / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
  }

  // Pending-review banner state — open dropdown id (one at a time).
  let openReclassifyFor = $state<string | null>(null);
  function pickCat(c: string): 'todo' | 'bug' | 'kb' | 'use_case' | '' {
    return c === 'todo' || c === 'bug' || c === 'kb' || c === 'use_case' ? c : '';
  }
  function categoryLabel(c: string): string {
    return c === 'use_case' ? 'Use Case' : c.charAt(0).toUpperCase() + c.slice(1);
  }
  function otherCategories(current: string): Array<'todo' | 'bug' | 'kb' | 'use_case'> {
    const all: Array<'todo' | 'bug' | 'kb' | 'use_case'> = ['todo', 'bug', 'kb', 'use_case'];
    return all.filter((c) => c !== current);
  }

  function firstFileAnchor(itemId: string): CodeAnchor | null {
    const list = itemAnchors[itemId];
    if (!list) return null;
    for (const a of list) {
      if (a.kind === 'file' && a.path) return a;
    }
    return null;
  }
  function anchorBadgeTooltip(a: CodeAnchor): string {
    const range = a.line_start && a.line_end && a.line_end > a.line_start
      ? `:${a.line_start}-${a.line_end}`
      : a.line_start ? `:${a.line_start}` : '';
    const rev = a.revision ? ` @${a.revision.slice(0, 7)}` : '';
    // Lead with provenance so the user can tell at a glance
    // whether to trust the link — addresses the "is this an LLM
    // hallucination?" question that's hard to answer otherwise.
    const tag = provenanceLabel(a.provenance);
    return `[${tag}] Open ${a.path}${range}${rev} in code canvas`;
  }

  // Short human label per provenance — used in tooltips so the
  // user knows whether they set the anchor, the agent suggested
  // it, the URL-detect pass spotted it, or a file-drop produced it.
  function provenanceLabel(p: string): string {
    switch (p) {
      case 'user-set':       return 'yours';
      case 'url-detected':   return 'auto from URL';
      case 'file-dropped':   return 'file-dropped';
      case 'agent-suggested': return 'LLM-suggested';
      default: return p;
    }
  }
  // CSS class suffix per provenance so the badge color reflects
  // trust level. user-set = strong accent; url-detected = neutral
  // accent; file-dropped = neutral; agent-suggested = muted/warning
  // so the user knows to verify before relying on it.
  function provenanceClass(p: string): string {
    switch (p) {
      case 'user-set':       return 'prov-user';
      case 'url-detected':   return 'prov-url';
      case 'file-dropped':   return 'prov-file';
      case 'agent-suggested': return 'prov-agent';
      default: return '';
    }
  }

  // Latch — when flashItemId changes, we paint the flash class for ~1.6s.
  // Latch identity (lastFlash) prevents re-flashing if the same id is
  // passed twice; App should clear & re-set to re-trigger.
  let flashing = $state<string | null>(null);
  let lastFlash = $state<string | null>(null);
  $effect(() => {
    if (flashItemId && flashItemId !== lastFlash) {
      lastFlash = flashItemId;
      flashing = flashItemId;
      queueMicrotask(() => {
        document.getElementById(`item-card-${flashItemId}`)?.scrollIntoView({
          behavior: 'smooth',
          block: 'nearest',
        });
      });
      const id = flashItemId;
      setTimeout(() => { if (flashing === id) flashing = null; }, 1600);
    }
  });

  // Per-item "Copied" tooltip. Map from item id → mode label for the
  // ephemeral feedback after a successful copy. Cleared on a timer.
  let copiedFor = $state<Record<string, string>>({});

  // Item-brief hover popup (bug #75). Cursor-anchored — anchoring to the card edge
  // landed the tooltip on top of the right neighbor when cards tile gap-0.
  // Renders position: fixed so the .item's overflow: hidden doesn't clip it.
  let reasoningHover = $state<{
    id: string;
    text: string;
    top: number;
    left: number;
  } | null>(null);

  // The summary tooltip appears only after the cursor rests on a card for this
  // long — so sweeping across the canvas doesn't flash a trail of popups. The
  // backlog item asked for 3s; that's long for a tooltip (most are ~500ms), so
  // it's a single knob to tune after testing.
  const REASONING_HOVER_DELAY = 3000; // ms
  let reasoningTimer: ReturnType<typeof setTimeout> | undefined;
  let pendingPointer: { top: number; left: number } | null = null;

  function pointerToTooltip(e: MouseEvent): { top: number; left: number } {
    return { top: e.clientY + 14, left: e.clientX + 14 };
  }

  // A brief of what the item is ABOUT — its own content (plain text, truncated),
  // not the LLM's classification reasoning. The card's type badge already says
  // what KIND it is; the hover should summarize the content (bug #75: the old
  // "The content describes… / follows the use-case format" reasoning was useless).
  function itemBrief(item: ScratchpadItem): string {
    const s = plainContent(item).trim();
    return s.length > 300 ? s.slice(0, 300).trimEnd() + '…' : s;
  }

  function showReasoning(e: MouseEvent, item: ScratchpadItem) {
    const brief = itemBrief(item);
    if (!brief || reasoningHover?.id === item.id) return;
    clearTimeout(reasoningTimer);
    pendingPointer = pointerToTooltip(e);
    const id = item.id;
    const text = brief;
    reasoningTimer = setTimeout(() => {
      // The item may have been deleted during the dwell delay — don't show a
      // tooltip for a card that no longer exists.
      if (!items.some((i) => i.id === id)) return;
      reasoningHover = { id, text, ...(pendingPointer ?? pointerToTooltip(e)) };
    }, REASONING_HOVER_DELAY);
  }

  function moveReasoning(e: MouseEvent, item: ScratchpadItem) {
    const pos = pointerToTooltip(e);
    if (reasoningHover?.id === item.id) {
      reasoningHover = { ...reasoningHover, ...pos };
    } else {
      // Keep the pending position fresh so the tooltip lands under the cursor
      // when the delay elapses, not where the cursor first entered.
      pendingPointer = pos;
    }
  }

  function hideReasoning(itemId: string) {
    clearTimeout(reasoningTimer);
    pendingPointer = null;
    if (reasoningHover?.id === itemId) {
      reasoningHover = null;
    }
  }

  // The card under a tooltip can vanish without a mouseleave (delete, archive,
  // hide, pad switch) — a removed element never fires leave events, so the
  // popup would stick to the screen (Rich's field find). Clear it the moment
  // its item stops existing; writes only when clearing, so no effect loop.
  $effect(() => {
    if (reasoningHover && !items.some((i) => i.id === reasoningHover!.id)) {
      clearTimeout(reasoningTimer);
      reasoningHover = null;
    }
  });

  async function copyItem(e: MouseEvent, item: ScratchpadItem) {
    e.stopPropagation();
    const withAttrs = e.shiftKey;
    const text = withAttrs ? itemToMarkdown(item) : plainContent(item);
    const ok = await copyToClipboard(text);
    if (!ok) {
      void alertDialog('Copy failed — clipboard access denied or unavailable.');
      return;
    }
    const label = withAttrs ? 'Copied as Markdown' : 'Copied';
    copiedFor = { ...copiedFor, [item.id]: label };
    setTimeout(() => {
      copiedFor = Object.fromEntries(
        Object.entries(copiedFor).filter(([k]) => k !== item.id),
      );
    }, 1200);
  }

  const COLS = 24;
  const ROW_H = 28;
  const MIN_W = 3;
  const MIN_H = 2;

  let container: HTMLDivElement;

  // Drag / resize state. Pixel offsets applied as transforms during the
  // gesture; on release we snap to cells, resolve collisions, and emit.
  type Gesture = {
    kind: 'drag' | 'resize' | 'group-drag';
    itemId: string;
    startX: number;
    startY: number;
    dx: number;
    dy: number;
    cellW: number;
  };
  let gesture = $state<Gesture | null>(null);

  // Collision-push resolve: items overlapping `moved` get shoved straight down.
  // Cascades until the layout is overlap-free. Terminates because every shove
  // monotonically increases row; worst case is O(n²) per pass.
  function resolveCollisions(
    layouts: Record<string, Layout>,
    movedIds: string[],
  ): Record<string, Layout> {
    const queue = [...movedIds];
    let guard = items.length * items.length;
    while (queue.length > 0 && guard-- > 0) {
      const curId = queue.shift()!;
      const cur = layouts[curId];
      if (!cur) continue;
      for (const other of items) {
        if (other.id === curId) continue;
        const o = layouts[other.id];
        if (!o) continue;
        if (overlaps(cur, o)) {
          layouts[other.id] = { ...o, row: cur.row + cur.h };
          queue.push(other.id);
        }
      }
    }
    return layouts;
  }

  function overlaps(a: Layout, b: Layout): boolean {
    return (
      a.col < b.col + b.w &&
      a.col + a.w > b.col &&
      a.row < b.row + b.h &&
      a.row + a.h > b.row
    );
  }

  // The group's rendered collision rectangle — matches the frame's on-screen
  // footprint: min 6 cols wide, and collapsed groups shrink to just the header
  // row. This is wider/taller than the raw member bbox, so a card can't nestle
  // into the frame's empty padding (which still reads as "inside the group").
  // An expanded group's header rides in the row ABOVE the members (its own
  // row), so that row is part of the footprint too — except at row 0, where
  // the header falls back to overlaying the frame's top edge.
  function frameRect(gm: GroupRenderModel): Layout {
    const headAbove = !gm.group.collapsed && gm.bbox.row > 0;
    return {
      col: gm.bbox.col,
      row: headAbove ? gm.bbox.row - 1 : gm.bbox.row,
      w: Math.max(gm.bbox.w, 6),
      h: (gm.group.collapsed ? 1 : Math.max(gm.bbox.h, 1)) + (headAbove ? 1 : 0),
    };
  }

  // UC-49 Slice B (criterion #3): push a dragged card below any group frame it
  // overlaps, EXCEPT its own group (members stay freely placeable inside their
  // container). Groups are immovable obstacles here — the card yields, not the
  // group. Loops so a card shoved clear of one group also clears a second one.
  function avoidGroups(layouts: Record<string, Layout>, dragged: ScratchpadItem) {
    let guard = groupModels.length + 4;
    let moved = true;
    while (moved && guard-- > 0) {
      moved = false;
      for (const gm of groupModels) {
        if (dragged.group_id === gm.group.id) continue;
        const cur = layouts[dragged.id];
        if (!cur) continue;
        const rect = frameRect(gm);
        if (overlaps(cur, rect)) {
          layouts[dragged.id] = { ...cur, row: rect.row + rect.h };
          moved = true;
        }
      }
    }
  }

  function currentLayouts(): Record<string, Layout> {
    const out: Record<string, Layout> = {};
    for (const it of items) {
      out[it.id] = {
        col: it.grid_col,
        row: it.grid_row,
        w: it.grid_w,
        h: it.grid_h,
      };
    }
    return out;
  }

  function cellWidth(): number {
    if (!container) return 40;
    return container.getBoundingClientRect().width / COLS;
  }

  function startDrag(e: PointerEvent, item: ScratchpadItem) {
    if (!(e.target instanceof HTMLElement)) return;
    // Ignore gestures that start on interactive children (buttons etc).
    if (e.target.closest('[data-grid-nodrag]')) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    gesture = {
      kind: 'drag',
      itemId: item.id,
      startX: e.clientX,
      startY: e.clientY,
      dx: 0,
      dy: 0,
      cellW: cellWidth(),
    };
  }

  function startResize(e: PointerEvent, item: ScratchpadItem) {
    e.preventDefault();
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    gesture = {
      kind: 'resize',
      itemId: item.id,
      startX: e.clientX,
      startY: e.clientY,
      dx: 0,
      dy: 0,
      cellW: cellWidth(),
    };
  }

  // UC-49: a group is a true container. Dragging its header moves the group
  // frame AND every member together, preserving their relative positions.
  function startGroupDrag(e: PointerEvent, group: ScratchpadItem) {
    if (!(e.target instanceof HTMLElement)) return;
    // Buttons in the header (collapse/note/delete) opt out of the drag.
    if (e.target.closest('[data-grid-nodrag]')) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    gesture = {
      kind: 'group-drag',
      itemId: group.id,
      startX: e.clientX,
      startY: e.clientY,
      dx: 0,
      dy: 0,
      cellW: cellWidth(),
    };
  }

  function onMove(e: PointerEvent) {
    if (!gesture) return;
    gesture = {
      ...gesture,
      dx: e.clientX - gesture.startX,
      dy: e.clientY - gesture.startY,
    };
  }

  function onUp() {
    if (!gesture) return;
    const g = gesture;
    gesture = null;

    const dCol = Math.round(g.dx / g.cellW);
    const dRow = Math.round(g.dy / ROW_H);
    const layouts = currentLayouts();

    // UC-49: group drag translates the group frame + all members as one unit.
    if (g.kind === 'group-drag') {
      const memberIds = items.filter((i) => i.group_id === g.itemId).map((i) => i.id);
      const movedIds = [g.itemId, ...memberIds].filter((id) => layouts[id]);
      // Clamp the delta against every moved cell so the group stays on the grid
      // AND keeps its internal layout — one shared delta, not per-item clamping.
      let cCol = dCol;
      let cRow = dRow;
      const grp = items.find((i) => i.id === g.itemId);
      // An expanded group's header tab needs the row above its members —
      // clamp so the topmost member never goes above row 1.
      const minMemberFloor = grp && !grp.collapsed && memberIds.length > 0 ? 1 : 0;
      for (const id of movedIds) {
        const l = layouts[id];
        cCol = clamp(cCol, -l.col, COLS - l.w - l.col);
        cRow = Math.max(cRow, (id === g.itemId ? 0 : minMemberFloor) - l.row);
      }
      if (cCol === 0 && cRow === 0) return;
      for (const id of movedIds) {
        const l = layouts[id];
        layouts[id] = { ...l, col: l.col + cCol, row: l.row + cRow };
      }
      resolveCollisions(layouts, movedIds);
      // The header tab sits in the row above the members; resolveCollisions
      // only pushed cards out of member cells, so evict any foreign card left
      // under the tab to below the frame.
      if (grp && !grp.collapsed && memberIds.length > 0) {
        const rows = memberIds.map((id) => layouts[id].row);
        const cols = memberIds.map((id) => layouts[id].col);
        const rights = memberIds.map((id) => layouts[id].col + layouts[id].w);
        const bottoms = memberIds.map((id) => layouts[id].row + layouts[id].h);
        const headRect: Layout = {
          col: Math.min(...cols),
          row: Math.min(...rows) - 1,
          w: Math.max(Math.max(...rights) - Math.min(...cols), 6),
          h: 1,
        };
        if (headRect.row >= 0) {
          const evicted: string[] = [];
          for (const it of items) {
            if (it.group_id === g.itemId || it.id === g.itemId || it.content_type === 'group') continue;
            const l = layouts[it.id];
            if (l && overlaps(l, headRect)) {
              layouts[it.id] = { ...l, row: Math.max(...bottoms) };
              evicted.push(it.id);
            }
          }
          if (evicted.length > 0) resolveCollisions(layouts, [...movedIds, ...evicted]);
        }
      }
      emitChanged(layouts);
      return;
    }

    const item = items.find((i) => i.id === g.itemId);
    if (!item) return;
    const cur = layouts[item.id];
    if (!cur) return;

    let next: Layout;
    if (g.kind === 'drag') {
      const newCol = clamp(cur.col + dCol, 0, COLS - cur.w);
      const newRow = Math.max(0, cur.row + dRow);
      if (newCol === cur.col && newRow === cur.row) return;
      next = { ...cur, col: newCol, row: newRow };
    } else {
      const maxW = COLS - cur.col;
      const newW = clamp(cur.w + dCol, MIN_W, maxW);
      const newH = Math.max(MIN_H, cur.h + dRow);
      if (newW === cur.w && newH === cur.h) return;
      next = { ...cur, w: newW, h: newH };
    }

    layouts[item.id] = next;
    // A loose card dropped onto a group is evicted below the frame instead of
    // overlaying it (and its header). Only on drag — a resize grows the card's
    // own footprint and shouldn't teleport it.
    if (g.kind === 'drag') avoidGroups(layouts, item);
    resolveCollisions(layouts, [item.id]);
    emitChanged(layouts);
  }

  // Emit only items whose layout actually changed vs. the current items.
  function emitChanged(layouts: Record<string, Layout>) {
    const base = currentLayouts();
    const updates: Record<string, Layout> = {};
    for (const it of items) {
      const before = base[it.id];
      const after = layouts[it.id];
      if (!before || !after) continue;
      if (
        before.col !== after.col ||
        before.row !== after.row ||
        before.w !== after.w ||
        before.h !== after.h
      ) {
        updates[it.id] = after;
      }
    }
    if (Object.keys(updates).length > 0) {
      onLayoutChange(updates);
    }
  }

  function onCancel() {
    gesture = null;
  }

  function clamp(v: number, lo: number, hi: number) {
    return Math.max(lo, Math.min(hi, v));
  }

  function stateClass(state: string): string {
    switch (state) {
      case 'pending-review': return 'st-pending';
      case 'classified':     return 'st-classified';
      case 'skipped':        return 'st-skipped';
      case 'failed':         return 'st-failed';
      case 'processing':     return 'st-processing';
      default:               return 'st-unprocessed';
    }
  }

  // Compute the dragged item's transform in px for live feedback.
  function transformFor(id: string): string {
    if (!gesture) return '';
    // UC-49: while group-dragging, the frame (id === group) and every member
    // (group_id === group) slide together for live feedback.
    if (gesture.kind === 'group-drag') {
      if (id === gesture.itemId) return `translate(${gesture.dx}px, ${gesture.dy}px)`;
      const it = items.find((i) => i.id === id);
      if (it?.group_id === gesture.itemId) return `translate(${gesture.dx}px, ${gesture.dy}px)`;
      return '';
    }
    if (gesture.itemId !== id) return '';
    if (gesture.kind === 'drag') {
      return `translate(${gesture.dx}px, ${gesture.dy}px)`;
    }
    return '';
  }

  // For resize, draw extra width/height during the gesture. Snapped on release.
  function sizeDeltaFor(id: string): { w: number; h: number } | null {
    if (!gesture || gesture.itemId !== id || gesture.kind !== 'resize') return null;
    return { w: gesture.dx, h: gesture.dy };
  }

  // Total row span needed so the container grows to fit the layout.
  let totalRows = $derived(
    items.reduce((m, i) => Math.max(m, i.grid_row + i.grid_h), 8),
  );
</script>

<svelte:window
  onpointermove={onMove}
  onpointerup={onUp}
  onpointercancel={onCancel}
/>

<div
  class="grid"
  bind:this={container}
  style="
    grid-template-columns: repeat({COLS}, 1fr);
    grid-auto-rows: {ROW_H}px;
    grid-template-rows: repeat({totalRows}, {ROW_H}px);
  "
>
  <!-- Slice 6: group frames rendered first so they sit behind their children (z-index handled in CSS). -->
  {#each groupModels as gm (gm.group.id)}
    {@const collapsed = gm.group.collapsed}
    <div
      class="group-frame"
      class:collapsed
      style="
        grid-column: {gm.bbox.col + 1} / span {Math.max(gm.bbox.w, 6)};
        grid-row: {gm.bbox.row + 1} / span {collapsed ? 1 : Math.max(gm.bbox.h, 1)};
        transform: {transformFor(gm.group.id)};
        {gesture?.kind === 'group-drag' && gesture.itemId === gm.group.id ? 'z-index: 30;' : ''}
        {typeColors?.enabled ? 'background: color-mix(in srgb, #5a5a5a 18%, transparent);' : ''}
      "
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <header
        class="group-head"
        class:head-inline={gm.bbox.row === 0}
        onpointerdown={(e) => startGroupDrag(e, gm.group)}
        title="Drag to move the whole group"
      >
        <button class="group-toggle" data-grid-nodrag onclick={() => onToggleGroupCollapse(gm.group)} title={collapsed ? 'Expand group' : 'Collapse group'}>
          {collapsed ? '▸' : '▾'}
        </button>
        {#if renameId === gm.group.id}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="group-name-input"
            data-grid-nodrag
            bind:value={renameDraft}
            autofocus
            placeholder="Group name"
            onpointerdown={(e) => e.stopPropagation()}
            onkeydown={(e) => onRenameKey(e, gm.group)}
            onblur={() => saveRename(gm.group)}
          />
        {:else}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="group-name" ondblclick={() => startRename(gm.group)} title="Double-click to rename">{gm.group.name || 'Group'}</span>
        {/if}
        <span class="group-count">{gm.children.length} item{gm.children.length === 1 ? '' : 's'}</span>
        <button
          class="group-note"
          class:has-note={!!gm.group.annotations}
          data-grid-nodrag
          onclick={(e) => { e.stopPropagation(); openGroupNote(gm.group); }}
          title={gm.group.annotations ? gm.group.annotations : 'Add a note to this group'}
          aria-label="Group note"
        ><Icon name="pencil" size={11} /></button>
        <button class="group-delete" data-grid-nodrag onclick={() => onDeleteGroup(gm.group)} title="Delete group (items keep their content)">×</button>
      </header>
      {#if noteEditId === gm.group.id}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="group-note-pop" data-grid-nodrag onpointerdown={(e) => e.stopPropagation()}>
          <!-- svelte-ignore a11y_autofocus -->
          <textarea
            bind:value={noteDraft}
            rows="4"
            placeholder="Notes about this group…"
            autofocus
            onkeydown={(e) => onNoteKey(e, gm.group)}
          ></textarea>
          <div class="note-actions">
            <button class="note-save" onclick={() => saveGroupNote(gm.group)}>Save</button>
            <button class="note-cancel" onclick={() => (noteEditId = null)}>Cancel</button>
          </div>
        </div>
      {/if}
    </div>
  {/each}
  {#each items as item (item.id)}
    {#if item.content_type === 'group'}
      <!-- groups render as frames above; skip the card -->
    {:else if item.group_id && collapsedGroupIds.has(item.group_id)}
      <!-- collapsed group: hide children -->
    {:else}
    {@const delta = sizeDeltaFor(item.id)}
    {@const isSelected = selectedItemIds.has(item.id)}
    <article
      id="item-card-{item.id}"
      class="item {stateClass(item.classification_state)}"
      class:dragging={gesture?.itemId === item.id}
      class:flash={flashing === item.id}
      class:done={doneSourceIds?.has(item.id) ?? false}
      class:selected={isSelected}
      style="
        grid-column: {item.grid_col + 1} / span {item.grid_w};
        grid-row: {item.grid_row + 1} / span {item.grid_h};
        transform: {transformFor(item.id)};
        {delta ? `width: calc(100% + ${delta.w}px); height: calc(100% + ${delta.h}px);` : ''}
        {cardTint(item)}
      "
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <header
        class="bar {dueBarClass(item)}"
        onpointerdown={(e) => {
          // Ctrl/Cmd+click toggles selection without starting a drag.
          if (e.metaKey || e.ctrlKey) {
            e.preventDefault();
            e.stopPropagation();
            toggleSelection(item.id);
            return;
          }
          startDrag(e, item);
        }}
      >
        {#if cardTitle(item)}
          <span class="title" title={cardTitle(item)}>{cardTitle(item)}</span>
        {/if}
        <span class="state">{item.classification_state}</span>
        {#if item.proposed_category}
          <span class="cat">→ {item.proposed_category}</span>
        {/if}
        {#if item.classification_override}
          <span class="override" title="User override">★ {item.classification_override}</span>
        {/if}
        {#if item.derived_item_id && $draftingCriteria.has(item.derived_item_id)}
          <span class="drafting-badge" title="Drafting acceptance criteria with AI in the background…">✨ drafting</span>
        {/if}
        {#each [derivedStatus?.get(item.id)] as ds (item.id)}
          {#if ds && statusTone(ds.status) !== 'open'}
            <span
              class="dstatus tone-{statusTone(ds.status)}"
              title="{ds.kind} status: {ds.status.replace(/[_-]/g, ' ')}"
            >{ds.status.replace(/[_-]/g, ' ')}</span>
          {/if}
        {/each}
        {#if item.skipped_reason}
          <span class="reason">{item.skipped_reason}</span>
        {/if}
        <span class="spacer"></span>
        {#if copiedFor[item.id]}
          <span class="copied-flash">{copiedFor[item.id]}</span>
        {/if}
        {#each [firstFileAnchor(item.id)] as fa (fa?.id ?? '')}
          {#if fa}
            <button
              class="icon-btn anchor-badge {provenanceClass(fa.provenance)}"
              data-grid-nodrag
              title={anchorBadgeTooltip(fa)}
              aria-label={anchorBadgeTooltip(fa)}
              onclick={(e) => { e.stopPropagation(); onAnchorBadge(fa); }}
              onmouseenter={() => onAnchorBadgeHover(fa)}
              onmouseleave={() => onAnchorBadgeHover(null)}
              onfocus={() => onAnchorBadgeHover(fa)}
              onblur={() => onAnchorBadgeHover(null)}
            ><Icon name="anchor" size={12} />{#if fa.provenance === 'agent-suggested'}<span class="prov-suffix" aria-hidden="true">?</span>{/if}</button>
          {/if}
        {/each}
        {#if item.classification_state === 'unprocessed' || item.classification_state === 'failed'}
          <button
            class="icon-btn reprocess"
            data-grid-nodrag
            onclick={(e) => { e.stopPropagation(); onReprocess(item); }}
            title="Reprocess — re-run classification (this capture got stuck)"
            aria-label="Reprocess"
          >↻</button>
        {/if}
        <button
          class="icon-btn"
          data-grid-nodrag
          onclick={(e) => { e.stopPropagation(); onEdit(item); }}
          title="Edit"
          aria-label="Edit"
        ><Icon name="edit" size={13} /></button>
        <button
          class="icon-btn"
          data-grid-nodrag
          onclick={(e) => copyItem(e, item)}
          title="Copy content (Shift+click for Markdown with attributes)"
          aria-label="Copy"
        ><Icon name="copy" size={13} /></button>
        <button
          class="icon-btn"
          data-grid-nodrag
          onclick={(e) => { e.stopPropagation(); onMoveItem(item); }}
          title="Move to scratchpad…"
          aria-label="Move to scratchpad"
        ><Icon name="move" size={13} /></button>
        <button
          class="icon-btn"
          data-grid-nodrag
          onclick={(e) => { e.stopPropagation(); onHide(item); }}
          title="Hide from canvas"
          aria-label="Hide"
        >−</button>
        <button
          class="del"
          data-grid-nodrag
          onclick={(e) => { e.stopPropagation(); onDelete(item); }}
          title="Delete"
        >×</button>
      </header>
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="body"
        onmouseenter={(e) => showReasoning(e, item)}
        onmousemove={(e) => moveReasoning(e, item)}
        onmouseleave={() => hideReasoning(item.id)}
      >
        {#if item.classification_state === 'pending-review'}
          {@const proposed = pickCat(item.proposed_category ?? '')}
          <div class="review-banner" data-grid-nodrag>
            <div class="rb-head">
              <span class="rb-icon" aria-hidden="true">⚠</span>
              <span class="rb-text">
                Pending review{#if proposed} · suggested:
                  <strong>{categoryLabel(proposed)}</strong>{/if}
              </span>
            </div>
            <div class="rb-actions">
              {#if proposed}
                <button
                  type="button"
                  class="rb-accept"
                  disabled={accepting.has(item.id)}
                  onclick={(e) => { e.stopPropagation(); accept(item); }}
                  title="Accept the agent's classification"
                >{accepting.has(item.id) ? 'Accepting…' : `Accept ${categoryLabel(proposed)}`}</button>
                <button
                  type="button"
                  class="rb-other"
                  onclick={(e) => {
                    e.stopPropagation();
                    openReclassifyFor = openReclassifyFor === item.id ? null : item.id;
                  }}
                  aria-expanded={openReclassifyFor === item.id}
                  title="Re-classify to another category"
                >Other ▾</button>
              {:else}
                <!-- No proposed category yet — only re-classify is meaningful. -->
                <button
                  type="button"
                  class="rb-other"
                  onclick={(e) => {
                    e.stopPropagation();
                    openReclassifyFor = openReclassifyFor === item.id ? null : item.id;
                  }}
                  aria-expanded={openReclassifyFor === item.id}
                >Classify ▾</button>
              {/if}
              {#if openReclassifyFor === item.id}
                <div class="rb-menu" role="menu">
                  {#each otherCategories(proposed) as cat (cat)}
                    <button
                      type="button"
                      role="menuitem"
                      class="rb-menu-item"
                      onclick={(e) => {
                        e.stopPropagation();
                        openReclassifyFor = null;
                        onReclassify(item, cat);
                      }}
                    >{categoryLabel(cat)}</button>
                  {/each}
                </div>
              {/if}
            </div>
          </div>
        {/if}
        {#if item.similar_to_id}
          <div class="dedup-banner" data-grid-nodrag>
            <div class="db-head">
              <span aria-hidden="true">🔁</span>
              <span class="db-text">
                Possible duplicate of:
                <strong>{similarTitle(item.similar_to_id)}</strong>
                {#if item.similarity_score}
                  <span class="db-score">{Math.round(item.similarity_score * 100)}%</span>
                {/if}
              </span>
            </div>
            <div class="db-actions">
              <button
                type="button"
                class="db-open"
                onclick={(e) => { e.stopPropagation(); onOpenSimilar(item.similar_to_id!); }}
              >Open match</button>
              <button
                type="button"
                class="db-group"
                onclick={(e) => { e.stopPropagation(); onGroupSimilar(item); }}
                title="Group these two items together"
              >Group</button>
              <button
                type="button"
                class="db-dismiss"
                onclick={(e) => { e.stopPropagation(); onDismissSimilar(item); }}
                title="Dismiss this suggestion"
              >Not a duplicate</button>
            </div>
          </div>
        {/if}
        {#if item.content_type === 'image' && item.blob_sha}
          <a class="img-link" href="/api/v1/blobs/{item.blob_sha}" target="_blank" rel="noopener" title={item.file_name || 'image'}>
            <img
              class="img"
              src="/api/v1/blobs/{item.blob_sha}"
              alt={item.file_name || item.name || 'image'}
              loading="lazy"
              draggable="false"
            />
          </a>
        {:else if item.content_type === 'link' && hasOG(item)}
          <a class="og-card" href={item.content} target="_blank" rel="noopener" title={item.content} onclick={(e) => e.stopPropagation()}>
            {#if item.og_image_sha}
              <img
                class="og-thumb"
                src="/api/v1/blobs/{item.og_image_sha}"
                alt={item.og_title || 'preview'}
                loading="lazy"
                draggable="false"
              />
            {/if}
            <div class="og-body">
              {#if item.og_title}
                <div class="og-title">{item.og_title}</div>
              {/if}
              {#if item.og_description}
                <div class="og-desc">{item.og_description}</div>
              {/if}
              <div class="og-url">{shortHost(item.content)}</div>
            </div>
          </a>
        {:else if item.content_type === 'composite'}
          <div class="composite-card">
            <div class="composite-icon" aria-hidden="true">¶</div>
            <div class="composite-meta">
              <div class="composite-title">{item.name || 'Document'}</div>
              <div class="composite-preview">{compositePreview(item.content)}</div>
            </div>
          </div>
        {:else if item.content_type === 'sketch'}
          {#if item.blob_sha}
            <div class="sketch-thumb-wrap">
              <img
                class="sketch-thumb"
                src="/api/v1/blobs/{item.blob_sha}"
                alt={item.name || 'sketch'}
                loading="lazy"
                draggable="false"
              />
              <span class="sketch-thumb-label">{item.name || 'Sketch'}</span>
            </div>
          {:else}
            <div class="sketch-card">
              <div class="sketch-icon" aria-hidden="true">✎</div>
              <div class="sketch-meta">
                <div class="sketch-title">{item.name || 'Sketch'}</div>
                <div class="sketch-hint">Click to open editor</div>
              </div>
            </div>
          {/if}
        {:else if item.content_type === 'file' && item.blob_sha}
          <div class="file-card">
            <div class="file-icon" aria-hidden="true">{fileIcon(item.mime_type, item.file_name)}</div>
            <div class="file-meta">
              <div class="file-name" title={item.file_name}>{item.file_name || '(unnamed file)'}</div>
              <div class="file-size">{formatBytes(item.byte_size ?? 0)}{item.mime_type ? ' · ' + item.mime_type : ''}</div>
            </div>
            <a
              class="file-download"
              href="/api/v1/blobs/{item.blob_sha}"
              download={item.file_name || ''}
              title="Download {item.file_name || 'file'}"
              onclick={(e) => e.stopPropagation()}
            >Download</a>
          </div>
        {:else}
          <pre>{item.content}</pre>
        {/if}
        {#each [cardTags(item)] as tags}
          {#if tags.length > 0}
            <div class="tags">
              {#each tags as t (t)}
                <span class="tag-chip">#{t}</span>
              {/each}
            </div>
          {/if}
        {/each}
        {#if latestNoteBySource?.get(item.id)}
          <div class="annotations" title="Latest note">{latestNoteBySource.get(item.id)}</div>
        {:else if item.annotations}
          <div class="annotations">{item.annotations}</div>
        {/if}
      </div>
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span
        class="resize"
        title="Resize"
        onpointerdown={(e) => startResize(e, item)}
      ></span>
    </article>
    {/if}
  {/each}
  {#if items.length === 0}
    <div class="empty">No items yet. Paste or type above.</div>
  {/if}
</div>

{#if reasoningHover}
  <div
    class="reasoning-popup"
    style="top: {reasoningHover.top}px; left: {reasoningHover.left}px;"
    role="tooltip"
  >
    <span class="reasoning-arrow">↳</span>
    {reasoningHover.text}
  </div>
{/if}

<style>
  .grid {
    display: grid;
    width: 100%;
    gap: 0;
    padding: 8px;
    box-sizing: border-box;
    min-height: 100%;
    position: relative;
  }
  .item {
    border: 1px solid var(--p-333333);
    background: var(--p-222222);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
    user-select: none;
    transition: transform 0.08s ease-out;
  }
  .item.dragging {
    transition: none;
    z-index: 10;
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.6);
    border-color: var(--p-66ccff);
  }
  .item.st-pending { border-left: 3px solid var(--p-d4a54d); }
  .item.st-classified { border-left: 3px solid var(--p-5aa055); }
  .item.st-skipped { border-left: 3px solid var(--p-666666); opacity: 0.8; }
  .item.st-failed { border-left: 3px solid var(--p-b04545); }
  .item.st-processing { border-left: 3px solid var(--p-4080b8); }
  .item.flash {
    animation: card-flash 1.6s ease-out;
  }
  /* Card whose every derived item (todo/bug/kb/use_case) reached a
     terminal status. Faded + strikethrough on the title so the canvas
     visually reflects downstream completion without removing the card.
     The bar/state/cat metadata stays at full readability so the user
     can still scan the row. */
  .item.done {
    opacity: 0.55;
  }
  .dstatus {
    font-size: 9px;
    padding: 0 5px;
    border-radius: 3px;
    text-transform: uppercase;
    letter-spacing: 0.4px;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .dstatus.tone-busy { background: var(--p-4a3a10); color: var(--p-ffeedd); }
  .dstatus.tone-done { background: var(--p-2a4a2a); color: var(--p-99cc99); }
  .dstatus.tone-dropped { background: var(--p-3a2a2a); color: var(--p-cc8888); }
  .item.selected {
    outline: 2px solid var(--p-66ccff);
    outline-offset: -2px;
  }
  .group-frame {
    border: 1px dashed var(--p-4a6b8a);
    border-radius: 6px;
    background: rgba(80, 130, 180, 0.06);
    pointer-events: none;
    position: relative;
    z-index: 0;
  }
  .group-frame.collapsed {
    background: var(--p-131a22);
    border-style: solid;
  }
  .group-head {
    position: absolute;
    /* UC-49 follow-up: the header lives in its OWN row — a tab riding on top
       of the frame — so it never covers the first member's title bar. The
       frame's collision rect (frameRect) reserves this row so push-aside
       keeps foreign cards out from under it. */
    top: -22px;
    left: -1px;
    right: -1px;
    height: 22px;
    background: var(--p-1a2530);
    border: 1px solid var(--p-2d5578);
    border-bottom: none;
    border-radius: 5px 5px 0 0;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    font-size: 11px;
    color: var(--p-cceeff);
    pointer-events: auto;
    z-index: 1;
    cursor: move;
    touch-action: none;
  }
  /* Fallback: a group whose members touch row 0 has no row above for the tab —
     overlay the frame's top edge like the pre-tab look rather than clip. */
  .group-head.head-inline {
    top: -1px;
    border-bottom: 1px solid var(--p-2d5578);
  }
  .group-frame.collapsed .group-head {
    top: -1px;
    border-bottom: 1px solid var(--p-2d5578);
    border-radius: 5px;
    height: 100%;
  }
  .group-toggle, .group-delete {
    background: transparent;
    border: none;
    color: var(--p-99ccff);
    cursor: pointer;
    font-size: 12px;
    padding: 0 4px;
    line-height: 1;
  }
  .group-toggle:hover, .group-delete:hover { color: var(--p-ffffff); }
  .group-name { font-weight: 500; cursor: text; }
  .group-name-input {
    font: inherit;
    font-weight: 500;
    color: var(--p-ffffff);
    background: var(--p-0c1218);
    border: 1px solid var(--p-2d5578);
    border-radius: 3px;
    padding: 0 4px;
    min-width: 60px;
    max-width: 180px;
    height: 16px;
    outline: none;
  }
  .group-note {
    background: transparent;
    border: none;
    color: var(--p-666666);
    padding: 2px 4px;
    cursor: pointer;
    line-height: 1;
  }
  .group-note:hover { color: var(--p-dddddd); }
  .group-note.has-note { color: var(--p-99ccff); }
  .group-note-pop {
    pointer-events: auto; /* parent frame is pointer-events: none */
    position: absolute;
    top: 26px;
    right: 6px;
    z-index: 30;
    width: 260px;
    background: var(--p-161616);
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  }
  .group-note-pop textarea {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-2a2a2a);
    border-radius: 3px;
    padding: 6px;
    font-size: 12px;
    font-family: inherit;
    resize: vertical;
  }
  .note-actions { display: flex; gap: 6px; justify-content: flex-end; }
  .note-actions button {
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 3px;
    cursor: pointer;
  }
  .note-save {
    background: var(--p-1e3a52);
    border: 1px solid var(--p-2d5578);
    color: var(--p-99ccff);
  }
  .note-save:hover { background: var(--p-2d5578); }
  .note-cancel {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-999999);
  }
  .group-count { color: var(--p-888888); font-size: 10px; margin-left: auto; }
  .item.done .title {
    text-decoration: line-through;
  }
  @keyframes card-flash {
    0%   { box-shadow: 0 0 0 3px var(--p-66ccff), 0 0 24px rgba(108, 207, 255, 0.6); }
    30%  { box-shadow: 0 0 0 3px var(--p-66ccff), 0 0 24px rgba(108, 207, 255, 0.4); }
    100% { box-shadow: 0 0 0 0 transparent, 0 0 0 transparent; }
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 2px 8px;
    background: var(--p-1a1a1a);
    font-size: 11px;
    cursor: move;
    flex-shrink: 0;
    touch-action: none;
  }
  /* UC-48: due-date header tint. Yellow within 24-48h, red within 24h / overdue.
     color-mix keeps text readable over the base bar in both themes; uses the
     app's use_case-amber and bug-red accents. */
  .bar.due-warn { background: color-mix(in srgb, #d4a54d 34%, var(--p-1a1a1a)); }
  .bar.due-urgent { background: color-mix(in srgb, #b04545 40%, var(--p-1a1a1a)); }
  .spacer { flex: 1; }
  /* UC-9: a user-provided name is the card's heading — render it bold and
     slightly larger than the 12px body text for a clear visual hierarchy. */
  .title {
    color: var(--p-eeeeee);
    font-weight: 700;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 50%;
  }
  .state {
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-size: 10px;
    color: var(--p-aaaaaa);
  }
  .copied-flash {
    font-size: 10px;
    color: var(--p-66cc66);
    font-style: italic;
    padding: 0 4px;
    animation: fade-in 0.15s;
  }
  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }
  .cat { color: var(--p-99ccff); font-weight: 500; }
  .override { color: var(--p-ffcc66); }
  .drafting-badge {
    color: var(--p-cfe6ff); background: var(--p-16222e);
    border: 1px solid var(--p-2d5578); border-radius: 3px;
    padding: 0 5px; font-size: 10px; white-space: nowrap;
  }
  .reason { color: var(--p-888888); font-style: italic; }
  .del {
    border: none;
    background: transparent;
    color: var(--p-888888);
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
    cursor: pointer;
  }
  .del:hover { color: var(--p-ffffff); }
  .icon-btn {
    border: none;
    background: transparent;
    color: var(--p-888888);
    font-size: 14px;
    padding: 0 4px;
    line-height: 1;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .icon-btn:hover { color: var(--p-ffffff); }
  .icon-btn.anchor-badge { color: var(--p-66ccff); }
  .icon-btn.anchor-badge:hover { color: var(--p-ffffff); background: var(--p-2d5578); border-radius: 2px; }
  /* Provenance-keyed colors so the badge signals trust at a glance.
     - prov-user   (you set it):       strong cyan accent
     - prov-url    (auto from URL):    softer cyan (still trustworthy)
     - prov-file   (file-dropped):     same as url — system-derived
     - prov-agent  (LLM-suggested):    amber + "?" suffix — verify before trusting
  */
  .icon-btn.anchor-badge.prov-user  { color: var(--p-66ccff); }
  .icon-btn.anchor-badge.prov-url   { color: var(--p-99ccff); }
  .icon-btn.anchor-badge.prov-file  { color: var(--p-99ccff); }
  .icon-btn.anchor-badge.prov-agent { color: var(--p-e7b86c); }
  .icon-btn.anchor-badge.prov-agent:hover { color: var(--p-ffffff); background: var(--p-5a3e1a); border-radius: 2px; }
  .prov-suffix {
    font-size: 9px;
    margin-left: 1px;
    font-weight: 700;
    line-height: 1;
  }
  .body {
    flex: 1;
    overflow: auto;
    min-height: 0;
    user-select: text;
    /* User-selectable scratchpad-item font (Appearance settings). Prose text
       picks up the chosen family; code-snippet <pre> stay monospace. */
    font-family: var(--item-font-family, inherit);
    font-size: var(--item-font-size, 12px);
  }
  /* Note content renders in <pre> (UA-monospace by default). Keep that default,
     but honour a chosen item font when the user sets one. */
  .body pre {
    font-family: var(--item-font-family, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
    font-size: var(--item-font-size, 12px);
  }
  .review-banner {
    background: var(--p-2a2310);
    border-bottom: 1px solid var(--p-4a3a14);
    padding: 5px 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .rb-head {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--p-d4a54d);
    font-size: 11px;
  }
  .rb-icon {
    font-size: 12px;
    flex-shrink: 0;
  }
  .rb-text strong { color: var(--p-ffc56c); font-weight: 600; }
  .rb-actions {
    display: flex;
    gap: 4px;
    align-items: center;
    position: relative;
    flex-wrap: wrap;
  }
  .rb-accept,
  .rb-other {
    background: var(--p-4a3a14);
    border: 1px solid var(--p-6a5424);
    color: var(--p-ffe1a3);
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 3px;
    cursor: pointer;
    line-height: 1.2;
  }
  .rb-accept:hover { background: var(--p-6a5424); color: var(--p-ffffff); }
  /* UC-35: in-flight Accept — disabled + dimmed so the click reads as taken. */
  .rb-accept:disabled { opacity: 0.6; cursor: progress; }
  .rb-other {
    background: transparent;
    color: var(--p-d4a54d);
  }
  .rb-other:hover { background: var(--p-4a3a14); color: var(--p-ffffff); }
  .rb-menu {
    position: absolute;
    top: 100%;
    left: 0;
    margin-top: 2px;
    background: var(--p-161616);
    border: 1px solid var(--p-4a3a14);
    border-radius: 3px;
    z-index: 50;
    min-width: 120px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
    display: flex;
    flex-direction: column;
  }
  .rb-menu-item {
    background: transparent;
    border: none;
    color: var(--p-dddddd);
    text-align: left;
    padding: 6px 10px;
    font-size: 12px;
    cursor: pointer;
    border-radius: 0;
  }
  .rb-menu-item:hover { background: var(--p-2a2310); color: var(--p-ffc56c); }
  .dedup-banner {
    background: var(--p-2a1e3a);
    border-bottom: 1px solid var(--p-4a3a5a);
    padding: 5px 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .db-head {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--p-c084fc);
    font-size: 11px;
  }
  .db-text { flex: 1; overflow: hidden; }
  .db-text strong {
    color: var(--p-d8b4fe);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .db-score {
    color: var(--p-a78bfa);
    font-size: 10px;
    margin-left: 4px;
    font-variant-numeric: tabular-nums;
  }
  .db-actions {
    display: flex;
    gap: 4px;
    align-items: center;
    flex-wrap: wrap;
  }
  .db-open,
  .db-group,
  .db-dismiss {
    background: var(--p-4a3a5a);
    border: 1px solid var(--p-6a5a7a);
    color: var(--p-e9d5ff);
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 3px;
    cursor: pointer;
    line-height: 1.2;
  }
  .db-open:hover, .db-group:hover { background: var(--p-6a5a7a); color: var(--p-ffffff); }
  .db-dismiss {
    background: transparent;
    color: var(--p-c084fc);
  }
  .db-dismiss:hover { background: var(--p-4a3a5a); color: var(--p-ffffff); }
  pre {
    margin: 0;
    padding: 6px 10px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--p-d8d8d8);
  }
  .img-link {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 6px;
    min-height: 0;
    flex: 1;
    background: var(--p-050505);
  }
  .img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    display: block;
    user-select: none;
  }
  .file-card {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    min-height: 0;
  }
  .file-icon {
    font-size: 28px;
    line-height: 1;
    flex-shrink: 0;
  }
  .file-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .file-name {
    color: var(--p-d8d8d8);
    font-size: 12px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-size {
    color: var(--p-888888);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-download {
    flex-shrink: 0;
    padding: 4px 10px;
    font-size: 11px;
    color: var(--p-99ccff);
    background: var(--p-1a2530);
    border: 1px solid var(--p-2d5578);
    border-radius: 3px;
    text-decoration: none;
  }
  .file-download:hover { background: var(--p-2d5578); color: var(--p-cceeff); }
  .sketch-card {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 14px;
    padding: 12px;
    min-height: 0;
    flex: 1;
    background: linear-gradient(135deg, var(--p-131a22) 0%, var(--p-0a0d12) 100%);
  }
  .sketch-icon {
    font-size: 36px;
    color: var(--p-66ccff);
    line-height: 1;
  }
  .sketch-meta {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .sketch-title {
    color: var(--p-dddddd);
    font-size: 13px;
    font-weight: 500;
  }
  .sketch-hint {
    color: var(--p-777777);
    font-size: 10px;
    font-style: italic;
  }
  .sketch-thumb-wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 4px;
    background: var(--p-050505);
  }
  .sketch-thumb {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    display: block;
    user-select: none;
  }
  .sketch-thumb-label {
    position: absolute;
    bottom: 4px;
    left: 6px;
    padding: 2px 6px;
    background: rgba(0, 0, 0, 0.55);
    color: var(--p-cccccc);
    font-size: 10px;
    border-radius: 2px;
    pointer-events: none;
  }
  .composite-card {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 12px;
    min-height: 0;
    flex: 1;
    overflow: hidden;
  }
  .composite-icon {
    font-size: 28px;
    line-height: 1;
    color: var(--p-99ccff);
    flex-shrink: 0;
  }
  .composite-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow: hidden;
  }
  .composite-title {
    color: var(--p-dddddd);
    font-size: 12px;
    font-weight: 500;
    line-height: 1.3;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .composite-preview {
    color: var(--p-888888);
    font-size: 11px;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 4;
    line-clamp: 4;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .og-card {
    display: flex;
    gap: 10px;
    padding: 8px 10px;
    min-height: 0;
    text-decoration: none;
    color: inherit;
    align-items: flex-start;
    overflow: hidden;
  }
  .og-card:hover .og-title { color: var(--p-cceeff); }
  .og-thumb {
    width: 84px;
    height: 84px;
    object-fit: cover;
    border-radius: 3px;
    background: var(--p-050505);
    flex-shrink: 0;
    user-select: none;
  }
  .og-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow: hidden;
  }
  .og-title {
    color: var(--p-dddddd);
    font-size: 12px;
    font-weight: 500;
    line-height: 1.3;
    /* clamp to two lines */
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .og-desc {
    color: var(--p-999999);
    font-size: 11px;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .og-url {
    color: var(--p-66ccff);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    margin-top: auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reasoning-popup {
    position: fixed;
    z-index: 1000;
    max-width: 320px;
    padding: 6px 10px;
    background: var(--p-1e3a52);
    color: var(--p-cceeff);
    border: 1px solid var(--p-2d5578);
    border-radius: 4px;
    font-size: 11px;
    font-style: italic;
    line-height: 1.45;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
    pointer-events: none;
    word-break: break-word;
  }
  .reasoning-arrow {
    color: var(--p-66ccff);
    font-style: normal;
    margin-right: 4px;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
    padding: 2px 10px 4px;
  }
  .tag-chip {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 2px;
    font-family: ui-monospace, monospace;
  }
  .annotations {
    padding: 4px 10px 4px;
    font-size: 11px;
    color: var(--p-bbbbbb);
    border-top: 1px solid var(--p-2a2a2a);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .resize {
    position: absolute;
    bottom: 0;
    right: 0;
    width: 12px;
    height: 12px;
    cursor: nwse-resize;
    background: linear-gradient(
      135deg,
      transparent 0 50%,
      var(--p-666666) 50% 60%,
      transparent 60% 70%,
      var(--p-666666) 70% 80%,
      transparent 80% 100%
    );
    touch-action: none;
  }
  .resize:hover { background-color: rgba(102, 204, 255, 0.2); }
  .empty {
    grid-column: 1 / -1;
    color: var(--p-666666);
    text-align: center;
    padding: 40px 20px;
    font-size: 12px;
  }
</style>
