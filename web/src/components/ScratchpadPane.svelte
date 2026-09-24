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
  import { alertDialog, confirmDialog, promptDialog } from '../lib/dialog';
  import { onMount, onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import { shouldSubmit, getSubmitShortcut } from '../lib/submitShortcut';
  import { collapseOnOverflow } from '../lib/overflowCollapse';
  import { loadRestackMode, saveRestackMode, loadAutoTidy, saveAutoTidy } from '../lib/canvasSettings';
  import { loadTypeColors, type TypeColorsConfig } from '../lib/typeColors';
  import { loadDueDateColors, type DueDateColorsConfig } from '../lib/dueDateColors';
  import type { RestackMode } from '../lib/api';
  import { loadUserBool, saveUserBool } from '../lib/userSettings';
  import type {
    ScratchpadItem,
    ClassificationOverride,
    Scratchpad,
    CanvasFilter,
    Project,
  } from '../lib/types';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import FilterPanel from './FilterPanel.svelte';
  import DedupModal from './DedupModal.svelte';
  import Modal from './Modal.svelte';
  import ScratchpadGrid from './ScratchpadGrid.svelte';
  import ListView from './ListView.svelte';
  import CalendarView from './CalendarView.svelte';
  import KanbanView from './KanbanView.svelte';
  import ScratchpadItemEdit from './ScratchpadItemEdit.svelte';
  import SketchEditor from './SketchEditor.svelte';
  import CompositeEditor from './CompositeEditor.svelte';
  import FileAnchorModal from './FileAnchorModal.svelte';
  import Icon from './Icon.svelte';

  type Props = {
    items: ScratchpadItem[];
    scratchpads: Scratchpad[];
    activeScratchpadId: string;
    projectId: string;
    // hiddenCount is computed in App and passed in so the pad-bar can show
    // a small "(N hidden)" badge as a discoverability hint.
    hiddenCount?: number;
    onChange: () => void;
    // Forwarded to ScratchpadGrid — flashes the matching card briefly when
    // App routes a code-canvas anchor click here.
    flashItemId?: string | null;
    // Per-item file-anchor map. Cards render an anchor badge if their id
    // is present and has at least one kind=file entry. Click → opens the
    // first file anchor in the code canvas.
    itemAnchors?: Record<string, import('../lib/types').CodeAnchor[]>;
    onCardAnchorClick: (anchor: import('../lib/types').CodeAnchor) => void;
    onCardAnchorHover: (anchor: import('../lib/types').CodeAnchor | null) => void;
    // App handles cross-scratchpad navigation when the user clicks
    // "Open match" on a dedup banner. The pane itself just bubbles up.
    onOpenSimilar: (similarToId: string) => void;
    /** Start a decompose on a document that is already here — the other half of
     *  the drop gesture: drag a spec in, then act on it where it landed. */
    onDecompose?: (item: ScratchpadItem) => void;
    // Set of scratchpad item ids whose every derived item is done.
    // ScratchpadGrid uses this to fade + strikethrough cards whose
    // downstream work is complete. Computed in App from padTodos /
    // padBugs / padKb / padUseCases.
    doneSourceIds?: Set<string>;
    // Bumped by App when the settings modal closes, so canvas-affecting
    // preferences (type colors) reload without a refresh.
    settingsTick?: number;
    // Per-card derived-item status chip (source item id -> kind+status+subject).
    derivedStatus?: Map<string, { kind: string; status: string; subject?: string }>;
    // Canvas pulse: source item id -> latest activity-log note summary.
    latestNoteBySource?: Map<string, string>;
    // Paid: show the 'Find duplicates' affordance only when licensed.
    dedupEnabled?: boolean;
    // Paid (UC-4): show the List/Calendar view toggle only when licensed.
    canvasViewsEnabled?: boolean;
    // Source-item-id -> derived todo's due_date (ISO), for the Calendar
    // view. Built in App from the active pad's todos.
    dueDateBySource?: Map<string, string>;
    // Source-item-id -> derived work item's tags (canvas rework C3). Forwarded
    // to the grid so cards show the living work record's tags.
    tagsBySource?: Map<string, string[]>;
    // Open a classified item's derived detail modal (todo/bug/use-case) —
    // App resolves the derived row and opens the matching modal, so the
    // canvas and the Lists drawer share one modal per type.
    onOpenDerived?: (item: ScratchpadItem) => void;
  };
  let {
    items,
    scratchpads,
    activeScratchpadId,
    projectId,
    hiddenCount = 0,
    onChange,
    flashItemId = null,
    itemAnchors = {},
    onCardAnchorClick,
    onCardAnchorHover,
    onOpenSimilar,
    onDecompose,
    doneSourceIds,
    settingsTick = 0,
    derivedStatus,
    dedupEnabled = false,
    canvasViewsEnabled = false,
    dueDateBySource = new Map(),
    latestNoteBySource = new Map(),
    tagsBySource = new Map(),
    onOpenDerived,
  }: Props = $props();

  // UC-4: alternative renderings of the same pad. 'canvas' is the
  // spatial grid (default); 'list' and 'calendar' are paid lenses on
  // filteredItems, scoped to this scratchpad. Falls back to canvas if
  // the license is lost.
  type ViewMode = 'canvas' | 'list' | 'calendar' | 'kanban';
  let viewMode = $state<ViewMode>('canvas');
  const effectiveView = $derived(canvasViewsEnabled ? viewMode : 'canvas');

  // Open an item the same way the canvas card does — sketches/composites
  // route to their visual editors, everything else to the metadata
  // modal. Shared by the canvas grid and the List/Calendar views.
  function openItem(item: ScratchpadItem) {
    if (item.content_type === 'sketch') { openExistingSketch(item); return; }
    if (item.content_type === 'composite') { openExistingComposite(item); return; }
    // Classified todo/bug/use-case items open their derived detail modal —
    // the SAME modal the Lists drawer uses — so it's one modal per type
    // regardless of how you got here. KB / skip / unclassified have no
    // derived detail modal, so they use the source-item editor.
    const cat = item.classification_override || item.proposed_category;
    if (onOpenDerived && item.derived_item_id && (cat === 'todo' || cat === 'bug' || cat === 'use_case')) {
      onOpenDerived(item);
      return;
    }
    editingItem = item;
  }

  // File-drop modal state. Non-null path = modal open.
  let droppedFile = $state<string | null>(null);
  let fileDropInProgress = $state(false);
  // OS-file drops route through the rich-canvas upload pipeline.
  // We track an in-flight count so the UI can show a spinner without
  // racing against multiple-file drops.
  let uploadsInFlight = $state(0);
  let uploadError = $state<string | null>(null);

  const FILE_DROP_TYPE = 'application/x-codedistill-file';

  // Two drag-source flavors converge on this pane:
  //   1. In-app file-tree drag (`application/x-codedistill-file`) → opens
  //      the FileAnchorModal so the user can attach the file as a code
  //      anchor to a new item.
  //   2. OS-file drag (`Files` MIME — supplied by the browser when the
  //      user drags an image off the desktop) → goes straight into the
  //      blob upload pipeline as an image / file scratchpad item.
  function hasFilePayload(e: DragEvent): boolean {
    return e.dataTransfer?.types.includes(FILE_DROP_TYPE) ?? false;
  }
  function hasOSFiles(e: DragEvent): boolean {
    return e.dataTransfer?.types.includes('Files') ?? false;
  }
  // A browser link drag carries text/uri-list — our signal for "route this URL
  // into the composer" (UC-24). We key on the type here because getData() is
  // empty during dragover; the actual URL is read on drop.
  function hasURLPayload(e: DragEvent): boolean {
    return e.dataTransfer?.types.includes('text/uri-list') ?? false;
  }
  // First bare http(s) URL in a dropped uri-list (or plain-text) payload, or ''.
  // A uri-list may include '#'-prefixed comment lines; those are skipped.
  function extractDroppedURL(e: DragEvent): string {
    const raw = e.dataTransfer?.getData('text/uri-list') || e.dataTransfer?.getData('text/plain') || '';
    for (const line of raw.split(/\r?\n/)) {
      const s = line.trim();
      if (/^https?:\/\/\S+$/.test(s)) return s;
    }
    return '';
  }

  function onPaneDragOver(e: DragEvent) {
    if (!hasFilePayload(e) && !hasOSFiles(e) && !hasURLPayload(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
    // Show the file-drop overlay only for file drags; a URL just lands in the composer.
    if (hasFilePayload(e) || hasOSFiles(e)) fileDropInProgress = true;
  }

  function onPaneDragLeave(e: DragEvent) {
    // Only clear if leaving the pane entirely — dragleave fires for children too.
    if ((e.currentTarget as Element).contains(e.relatedTarget as Node)) return;
    fileDropInProgress = false;
  }

  function onPaneDrop(e: DragEvent) {
    fileDropInProgress = false;
    // UC-24: a dropped URL routes INTO the composer (instead of being silently
    // stored as a bare link), so the Summarize / Send choice appears. Runs
    // before the file checks — a URL drag carries no Files payload.
    if (hasURLPayload(e)) {
      const url = extractDroppedURL(e);
      if (url) {
        e.preventDefault();
        draft = url;
        composerKind = 'note';
        return;
      }
    }
    // OS file drop wins — that's the rich-canvas upload path. The
    // browser populates dataTransfer.files for OS drops; presence of
    // any file there means "treat as upload."
    if (hasOSFiles(e) && e.dataTransfer && e.dataTransfer.files.length > 0) {
      e.preventDefault();
      void uploadFiles(Array.from(e.dataTransfer.files));
      return;
    }
    if (!hasFilePayload(e)) return;
    e.preventDefault();
    const raw = e.dataTransfer?.getData(FILE_DROP_TYPE);
    if (!raw) return;
    try {
      const payload = JSON.parse(raw) as { project_id: string; path: string };
      // Cross-project drops are rejected — anchors are repo-scoped and the
      // destination project's repo_root may be different or unset.
      if (payload.project_id !== projectId) {
        void alertDialog('File drops must target the same project that owns the file.');
        return;
      }
      droppedFile = payload.path;
    } catch (err) {
      console.warn('bad file-drop payload', err);
    }
  }

  // Shared upload pipeline for drag-drop / paste / button. Uploads
  // serially so the per-card grid_row assignment doesn't race (each
  // upload's response feeds into the next item's NextAvailableGridRow).
  async function uploadFiles(files: File[]) {
    if (!activeScratchpadId || files.length === 0) return;
    uploadError = null;
    for (const f of files) {
      uploadsInFlight++;
      try {
        await api.uploadBlob(activeScratchpadId, f);
      } catch (err) {
        uploadError = `Upload failed for ${f.name}: ${err}`;
      } finally {
        uploadsInFlight--;
      }
    }
    onChange();
  }

  // Clipboard paste — extract image / file payloads from a paste
  // event. Skipped when the user is pasting into a text input so we
  // don't hijack normal text editing.
  function onDocPaste(e: ClipboardEvent) {
    const target = e.target as Element | null;
    if (target instanceof HTMLElement) {
      const tag = target.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || target.isContentEditable) {
        return;
      }
    }
    const items = e.clipboardData?.items;
    if (!items) return;
    const files: File[] = [];
    for (const it of items) {
      if (it.kind === 'file') {
        const f = it.getAsFile();
        if (f) files.push(f);
      }
    }
    if (files.length === 0) return;
    e.preventDefault();
    void uploadFiles(files);
  }

  // Sketch editor state. open=true with item=null = new sketch;
  // open=true with item=<existing sketch> = edit. Set by the
  // Sketch header button (new) or by clicking a sketch card on
  // the grid (existing).
  let sketchEditorOpen = $state(false);
  let sketchEditorItem = $state<ScratchpadItem | null>(null);
  function openNewSketch() {
    sketchEditorItem = null;
    sketchEditorOpen = true;
  }
  function openExistingSketch(it: ScratchpadItem) {
    sketchEditorItem = it;
    sketchEditorOpen = true;
  }
  function closeSketchEditor() {
    sketchEditorOpen = false;
    sketchEditorItem = null;
  }

  // Slice 6 — multi-select state for grouping. Ctrl/Cmd+click in
  // the grid toggles selection; the Group header button appears
  // when ≥1 items are selected.
  let selectedItemIds = $state(new Set<string>());
  function onSelectionChange(next: Set<string>) {
    selectedItemIds = next;
  }
  function clearSelection() {
    selectedItemIds = new Set();
  }

  async function createGroupFromSelection() {
    if (selectedItemIds.size === 0 || !activeScratchpadId) return;
    const name = await promptDialog('Name this group:', {
      title: 'Create group',
      placeholder: 'e.g. "OAuth handoff"',
      confirmLabel: 'Create',
    });
    if (name === null) return; // cancelled
    const trimmed = name.trim();
    try {
      // Create the group item itself. content is empty (its
      // identity is name + membership). Grid position is the
      // top-left of the selection so the frame lands sensibly.
      const selectedItems = items.filter((i) => selectedItemIds.has(i.id));
      const minCol = Math.min(...selectedItems.map((i) => i.grid_col));
      const minRow = Math.min(...selectedItems.map((i) => i.grid_row));
      const group = await api.createItem(activeScratchpadId, {
        name: trimmed,
        content: '',
        content_type: 'group',
      });
      // Position the group at the top-left of the selection so the
      // frame overlay sits where it should. Width/height are
      // derived on render from children, but a sensible default
      // helps if the group ever exists without children.
      await api.updateItem(group.id, {
        grid_col: minCol,
        grid_row: minRow,
        grid_w: 12,
        grid_h: 4,
      });
      // Assign membership AND colocate (UC-38): a multi-select can span cards
      // scattered across the canvas, which would leave the group frame (a bbox
      // over its members) ballooning across unrelated items. Pack them into a
      // tidy column at the selection's top-left — same tight-frame behavior the
      // dedup auto-group already uses. This is a one-shot tidy at creation; we
      // never re-pack, so dragging a card around inside the group afterward
      // sticks (members stay manually placeable). Order by current position so
      // the stack preserves the user's rough top-to-bottom intent.
      const ordered = [...selectedItems].sort(
        (a, b) => a.grid_row - b.grid_row || a.grid_col - b.grid_col,
      );
      let row = minRow;
      for (const it of ordered) {
        await api.updateItem(it.id, { group_id: group.id, grid_col: minCol, grid_row: row });
        row += it.grid_h || 4;
      }
      clearSelection();
      onChange();
    } catch (e) {
      void alertDialog(`Group create failed: ${e}`);
    }
  }

  async function saveGroupNote(group: ScratchpadItem, note: string) {
    try {
      await api.updateItem(group.id, { annotations: note });
      onChange();
    } catch (e) {
      void alertDialog(`Save note failed: ${e}`);
    }
  }

  async function renameGroup(group: ScratchpadItem, name: string) {
    const next = name.trim();
    if (next === (group.name ?? '')) return; // no-op
    try {
      await api.updateItem(group.id, { name: next });
      onChange();
    } catch (e) {
      void alertDialog(`Rename failed: ${e}`);
    }
  }

  async function toggleGroupCollapse(group: ScratchpadItem) {
    try {
      await api.updateItem(group.id, { collapsed: !group.collapsed });
      onChange();
    } catch (e) {
      void alertDialog(`Toggle failed: ${e}`);
    }
  }

  async function deleteGroup(group: ScratchpadItem) {
    if (!await confirmDialog(`Delete group "${group.name || 'Group'}"? Items inside will be ungrouped but kept.`, { title: 'Delete group', confirmLabel: 'Delete', danger: true })) {
      return;
    }
    try {
      // ON DELETE SET NULL on group_id (migration 0027) — children
      // are orphaned by the FK, not cascaded.
      await api.deleteItem(group.id);
      onChange();
    } catch (e) {
      void alertDialog(`Delete group failed: ${e}`);
    }
  }

  // Composite-doc editor — mirrors sketch editor state pattern.
  // Triggered by the "Doc" header button (new) or by clicking a
  // composite card on the grid (existing).
  let compositeEditorOpen = $state(false);
  let compositeEditorItem = $state<ScratchpadItem | null>(null);
  function openNewComposite() {
    compositeEditorItem = null;
    compositeEditorOpen = true;
  }
  function openExistingComposite(it: ScratchpadItem) {
    compositeEditorItem = it;
    compositeEditorOpen = true;
  }
  function closeCompositeEditor() {
    compositeEditorOpen = false;
    compositeEditorItem = null;
  }

  // Hidden file input + button trigger. Reusable across "Upload"
  // header button and any other call sites later.
  let fileInput: HTMLInputElement | null = $state(null);
  function openUploadPicker() {
    if (fileInput) fileInput.click();
  }
  function onUploadInputChange(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;
    void uploadFiles(Array.from(input.files));
    // Reset so re-selecting the same file fires onchange.
    input.value = '';
  }

  // Document-level paste listener — gated to non-input targets in
  // onDocPaste itself. Mounted/unmounted with the component so
  // unmounted scratchpads don't leak handlers.
  onMount(() => document.addEventListener('paste', onDocPaste));
  onDestroy(() => document.removeEventListener('paste', onDocPaste));

  let draft = $state('');
  let override = $state<ClassificationOverride>('');
  // Composer modality (#21): one entry point for new items. The
  // [Note · Sketch · Doc] segment picks the kind — Note is the text composer
  // (today's behavior, incl. classification); Sketch/Doc swap in a launcher for
  // their rich modal editor. Replaces the old orphan toolbar buttons. This is
  // frontend-only: items created are identical to before, so it's reversible by
  // reverting one commit.
  let composerKind = $state<'note' | 'sketch' | 'doc'>('note');
  let busy = $state(false);
  let typeColors = $state<TypeColorsConfig | undefined>(undefined);
  let dueDateColors = $state<DueDateColorsConfig | undefined>(undefined);
  let lastSettingsTick = -1;
  $effect(() => {
    if (settingsTick !== lastSettingsTick) {
      lastSettingsTick = settingsTick;
      void loadTypeColors().then((c) => (typeColors = c));
      void loadDueDateColors().then((c) => (dueDateColors = c));
    }
  });

  let pendingDelete = $state<ScratchpadItem | null>(null);

  // Archive (backlog item #19). The Delete button's behavior is a project
  // setting: hard-delete, archive, or prompt which. Archived items leave the
  // canvas but stay searchable + restorable.
  let deleteAction = $state<'delete' | 'archive' | 'prompt'>('prompt');
  let archivePrompt = $state<ScratchpadItem | null>(null);
  let showArchived = $state(false);
  let archivedItems = $state<ScratchpadItem[]>([]);
  let archivedLoadFailed = $state(false);
  $effect(() => {
    if (!projectId) return;
    api
      .getProjectSetting(projectId, 'archive.delete_action')
      .then((r) => {
        if (r?.value === 'delete' || r?.value === 'archive' || r?.value === 'prompt') deleteAction = r?.value;
      })
      .catch(() => {});
  });
  function requestDelete(item: ScratchpadItem) {
    if (deleteAction === 'archive') {
      doArchive(item);
    } else if (deleteAction === 'prompt') {
      archivePrompt = item;
    } else {
      pendingDelete = item;
    }
  }
  async function doArchive(item: ScratchpadItem) {
    archivePrompt = null;
    try {
      await api.archiveItem(item.id);
      onChange();
      if (showArchived) loadArchived();
    } catch (e) {
      void alertDialog(`Archive failed: ${e}`);
    }
  }
  async function doUnarchive(item: ScratchpadItem) {
    try {
      await api.unarchiveItem(item.id);
      onChange();
      loadArchived();
    } catch (e) {
      void alertDialog(`Unarchive failed: ${e}`);
    }
  }
  async function loadArchived() {
    try {
      const all = await api.listItems(activeScratchpadId, true);
      archivedItems = all.filter((i) => i.archived_at);
      archivedLoadFailed = false;
    } catch {
      // Don't render a load failure as "Nothing archived" (audit M26).
      archivedItems = [];
      archivedLoadFailed = true;
    }
  }
  function toggleArchived() {
    showArchived = !showArchived;
    if (showArchived) loadArchived();
  }
  // Move-to-scratchpad modal target. Null = closed.
  let movingItem = $state<ScratchpadItem | null>(null);
  let moveBusy = $state(false);
  let moveErr = $state('');
  // Cross-project move (UC-1): the modal lets you first pick a target
  // project, then one of its scratchpads. Projects load lazily on open.
  let moveProjects = $state<Project[]>([]);
  let moveTargetProjectId = $state('');
  let moveTargetScratchpads = $state<Scratchpad[]>([]);
  let moveLoadingPads = $state(false);

  async function startMove(item: ScratchpadItem) {
    movingItem = item;
    moveErr = '';
    moveTargetProjectId = projectId;
    moveTargetScratchpads = scratchpads;
    if (moveProjects.length === 0) {
      try {
        moveProjects = await api.listProjects();
      } catch {
        // Non-fatal: fall back to same-project move (no picker shown).
      }
    }
  }

  async function onMoveProjectChange(pid: string) {
    moveTargetProjectId = pid;
    moveErr = '';
    if (pid === projectId) {
      moveTargetScratchpads = scratchpads;
      return;
    }
    moveLoadingPads = true;
    try {
      moveTargetScratchpads = await api.listScratchpads(pid);
    } catch (e) {
      moveErr = String(e);
      moveTargetScratchpads = [];
    } finally {
      moveLoadingPads = false;
    }
  }

  // "Hide done" toggle — when on, items whose every derived
  // todo/bug/kb/use_case is in terminal status (doneSourceIds,
  // computed in App from the per-pad derived lists) drop off the
  // canvas. Different from the per-item manual `hidden` flag: this
  // is a single visibility filter for "all the stuff I've already
  // worked through," persisted per-user so the choice survives
  // reloads. Default off (preserves current behavior for anyone
  // who never touches the toggle).
  const HIDE_DONE_KEY = 'ui.scratchpad.hide_done';
  let hideDone = $state(false);
  onMount(async () => {
    hideDone = await loadUserBool(HIDE_DONE_KEY, false);
  });
  function toggleHideDone() {
    hideDone = !hideDone;
    saveUserBool(HIDE_DONE_KEY, hideDone);
  }

  // Canvas only renders visible items. The hidden-items badge in the
  // pad-bar shows a count for discoverability; unhide UX still TBD.
  //
  // Two-pass filter:
  //   Pass 1 — per-item gating:
  //     - !i.hidden         (per-item manual hide flag, the Hide button)
  //     - !done             (hideDone toggle, non-group items only)
  //   Pass 2 — group GC:
  //     - When hideDone is on AND a group's children were all filtered
  //       out by pass 1, drop the group too. Otherwise the canvas
  //       leaves an empty frame outline where the group used to live.
  let visibleItems = $derived.by(() => {
    const passOne = items.filter((i) => {
      if (i.hidden) return false;
      if (hideDone && i.content_type !== 'group' && doneSourceIds?.has(i.id)) return false;
      return true;
    });
    if (!hideDone) return passOne;
    // Which groups still have at least one surviving child?
    const groupsWithSurvivors = new Set<string>();
    for (const i of passOne) {
      if (i.group_id) groupsWithSurvivors.add(i.group_id);
    }
    return passOne.filter((i) =>
      i.content_type !== 'group' || groupsWithSurvivors.has(i.id),
    );
  });
  // Count of items the hideDone toggle is currently suppressing —
  // surfaced in the pad-bar so it's obvious WHY the canvas got
  // smaller after toggling.
  let hiddenByDoneCount = $derived(
    hideDone
      ? items.filter((i) => !i.hidden && i.content_type !== 'group' && doneSourceIds?.has(i.id)).length
      : 0,
  );

  // Category filter chips above the grid. Single-select. Persists across
  // scratchpad switches (the pane stays mounted) but resets on reload.
  // Mapped to scratchpad_item fields:
  //   - all: no filter
  //   - todo / bug / kb / use_case: classification_override OR proposed_category
  //   - unclassified: no proposed_category and no override (still funneling)
  //   - pending_review: classification_state === 'pending-review'
  type Filter = CanvasFilter;
  let filter = $state<Filter>('all');
  let filterPanelOpen = $state(false);
  let dedupModalOpen = $state(false);
  // True when any view filter narrows the canvas — drives the tiny
  // active-dot on the funnel button.
  let filterActive = $derived(filter !== 'all' || hideDone);

  function effectiveCategory(it: ScratchpadItem): string {
    return it.classification_override || it.proposed_category || '';
  }
  let filteredItems = $derived.by(() => {
    if (filter === 'all') return visibleItems;
    if (filter === 'pending_review') {
      return visibleItems.filter((i) => i.classification_state === 'pending-review');
    }
    if (filter === 'unclassified') {
      return visibleItems.filter((i) => effectiveCategory(i) === '');
    }
    return visibleItems.filter((i) => effectiveCategory(i) === filter);
  });
  function countFor(f: Filter): number {
    // Group frames are containers, not items — exclude them from every count so
    // the chips stay consistent (All == sum of category chips). They still
    // render on the canvas; only the tallies skip them.
    const base = visibleItems.filter((i) => i.content_type !== 'group');
    if (f === 'all') return base.length;
    if (f === 'pending_review') {
      return base.filter((i) => i.classification_state === 'pending-review').length;
    }
    if (f === 'unclassified') {
      return base.filter((i) => effectiveCategory(i) === '').length;
    }
    return base.filter((i) => effectiveCategory(i) === f).length;
  }

  let editingItem = $state<ScratchpadItem | null>(null);

  let activePad = $derived(
    scratchpads.find((s) => s.id === activeScratchpadId) ?? null
  );

  // A bare URL in the composer can be fetched + summarized instead of stored raw.
  let summarizing = $state(false);
  const bareUrl = $derived(/^https?:\/\/\S+$/.test(draft.trim()));
  async function summarize() {
    const url = draft.trim();
    if (!bareUrl || summarizing) return;
    summarizing = true;
    try {
      await api.summarizeUrl(activeScratchpadId, url);
      draft = '';
      override = '';
      onChange();
    } catch (e) {
      console.error(e);
      void alertDialog(`Summarize failed: ${e}`);
    } finally {
      summarizing = false;
    }
  }

  async function submit() {
    const trimmed = draft.trim();
    if (!trimmed || busy) return;
    busy = true;
    try {
      await api.createItem(activeScratchpadId, {
        content: trimmed,
        classification_override: override || undefined,
      });
      draft = '';
      override = '';
      onChange();
    } catch (e) {
      console.error(e);
      void alertDialog(`Create failed: ${e}`);
    } finally {
      busy = false;
    }
  }

  async function doDelete() {
    const it = pendingDelete;
    pendingDelete = null;
    if (!it) return;
    try {
      await api.deleteItem(it.id);
      onChange();
    } catch (e) {
      void alertDialog(`Delete failed: ${e}`);
    }
  }

  async function doAccept(item: ScratchpadItem) {
    try {
      await api.acceptItem(item.id);
      onChange();
    } catch (e) {
      void alertDialog(`Accept failed: ${e}`);
    }
  }

  // UC-46: set/clear a calendar item's due date by resolving its derived item
  // (todo/bug/use-case) and PATCHing due_date. iso null clears; KB has no due date.
  async function setItemDueDate(item: ScratchpadItem, iso: string | null) {
    const id = item.derived_item_id;
    const kind = derivedStatus?.get(item.id)?.kind;
    if (!id || !kind) return;
    const due = iso ?? '';
    try {
      if (kind === 'todo') await api.updateTodo(id, { due_date: due });
      else if (kind === 'bug') await api.updateBug(id, { due_date: due });
      else if (kind === 'use case') await api.updateUseCase(id, { due_date: due });
      else return; // kb — no due date
      onChange();
    } catch (e) {
      void alertDialog(`Set due date failed: ${e}`);
    }
  }
  async function doReclassify(item: ScratchpadItem, category: 'todo' | 'bug' | 'kb' | 'use_case') {
    try {
      await api.reclassifyItem(item.id, category);
      onChange();
    } catch (e) {
      void alertDialog(`Reclassify failed: ${e}`);
    }
  }
  async function doDismissSimilar(item: ScratchpadItem) {
    try {
      await api.dismissSimilarity(item.id);
      onChange();
    } catch (e) {
      void alertDialog(`Dismiss failed: ${e}`);
    }
  }
  async function doGroupSimilar(item: ScratchpadItem) {
    try {
      await api.groupSimilar(item.id);
      onChange();
    } catch (e) {
      void alertDialog(`Group failed: ${e}`);
    }
  }

  async function doMove(dstScratchpadId: string) {
    if (!movingItem || moveBusy) return;
    if (dstScratchpadId === movingItem.scratchpad_id) {
      movingItem = null;
      return;
    }
    moveBusy = true;
    moveErr = '';
    try {
      await api.moveItem(movingItem.id, dstScratchpadId);
      movingItem = null;
      onChange();
    } catch (e) {
      moveErr = String(e);
    } finally {
      moveBusy = false;
    }
  }

  async function hideItem(it: ScratchpadItem) {
    try {
      await api.updateItem(it.id, { hidden: true });
      onChange();
    } catch (e) {
      void alertDialog(`Hide failed: ${e}`);
    }
  }

  // Re-enqueue a capture stuck at unprocessed/failed. The classifier picks it up
  // shortly; the poll refreshes the card when its state changes.
  async function reprocessItem(it: ScratchpadItem) {
    try {
      await api.reprocessItem(it.id);
      onChange();
    } catch (e) {
      void alertDialog(`Reprocess failed: ${e}`);
    }
  }

  function excerpt(s: string, n = 60): string {
    s = s.trim().replace(/\s+/g, ' ');
    return s.length > n ? s.slice(0, n) + '…' : s;
  }

  function handleKey(e: KeyboardEvent) {
    if (shouldSubmit(e)) {
      e.preventDefault();
      submit();
    }
  }

  async function commitLayout(
    updates: Record<string, { col: number; row: number; w: number; h: number }>,
  ) {
    try {
      await Promise.all(
        Object.entries(updates).map(([id, l]) =>
          api.updateItem(id, {
            grid_col: l.col,
            grid_row: l.row,
            grid_w: l.w,
            grid_h: l.h,
          }),
        ),
      );
      onChange();
    } catch (e) {
      void alertDialog(`Layout save failed: ${e}`);
      onChange();
    }
  }

  let exporting = $state(false);
  async function exportActivePad() {
    if (!activePad || exporting) return;
    exporting = true;
    try {
      await api.exportScratchpad(activePad.id);
    } catch (e) {
      void alertDialog(String(e));
    } finally {
      exporting = false;
    }
  }

  // Compact the grid via server-side shelf-pack. Atomic on the backend
  // so the 3s poll never sees half-applied positions; we fire onChange
  // afterwards to pull the new layout into the local items array.
  let restacking = $state(false);
  let restackMenuOpen = $state(false);
  // Toolbar label collapse, driven by the bar's own overflow (lib/overflowCollapse).
  let padBarTight = $state(false);
  let restackWrapEl = $state<HTMLElement | null>(null);
  // Close the menu on any click outside its wrapper or on Escape —
  // previously it only closed via the caret or picking an option.
  function onWindowPointer(e: MouseEvent) {
    if (restackMenuOpen && restackWrapEl && !restackWrapEl.contains(e.target as Node)) {
      restackMenuOpen = false;
    }
  }
  function onWindowKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && restackMenuOpen) restackMenuOpen = false;
  }
  const RESTACK_MODES: { mode: RestackMode; label: string; hint: string }[] = [
    { mode: 'tidy', label: 'Tidy', hint: 'Keep card sizes; slide each into the lowest gap that fits' },
    { mode: 'fit', label: 'Fit to content', hint: 'Resize text cards to their content, then tidy (overwrites manual sizes on text cards)' },
    { mode: 'cols2', label: '2 columns', hint: 'Force half-width cards, then tidy' },
    { mode: 'cols3', label: '3 columns', hint: 'Force third-width cards, then tidy' },
  ];
  async function restackActivePad(mode: RestackMode) {
    restackMenuOpen = false;
    if (!activePad || restacking) return;
    restacking = true;
    try {
      saveRestackMode(activePad.id, mode);
      await api.restackScratchpad(activePad.id, mode);
      onChange();
    } catch (e) {
      void alertDialog(String(e));
    } finally {
      restacking = false;
    }
  }
  // Plain click on the button repeats the pad's last-used mode; the
  // dropdown arrow picks a different one.
  function restackDefault() {
    if (!activePad) return;
    void restackActivePad(loadRestackMode(activePad.id));
  }

  // UC-12: per-pad auto-tidy. When on, item mutations (anything that
  // changes the layout signature — creates, deletes, hides, grouping,
  // agent auto-grouping) trigger a debounced Tidy. User drags/resizes
  // change the signature too, but Tidy never moves a card that already
  // sits in a gap-free spot, and the server no-ops entirely when the
  // layout is stable — so the loop terminates after one pass.
  let autoTidy = $state(false);
  let lastPadForTidy = '';
  let tidyTimer: ReturnType<typeof setTimeout> | null = null;
  let lastLayoutSig = '';
  $effect(() => {
    if (activePad && activePad.id !== lastPadForTidy) {
      lastPadForTidy = activePad.id;
      autoTidy = loadAutoTidy(activePad.id);
      lastLayoutSig = '';
    }
  });
  function toggleAutoTidy() {
    if (!activePad) return;
    autoTidy = !autoTidy;
    saveAutoTidy(activePad.id, autoTidy);
    if (autoTidy) scheduleAutoTidy();
  }
  function layoutSig(): string {
    return items
      .map((i) => `${i.id}:${i.grid_col},${i.grid_row},${i.grid_w},${i.grid_h},${i.hidden ? 1 : 0}`)
      .sort()
      .join('|');
  }
  function scheduleAutoTidy() {
    if (tidyTimer) clearTimeout(tidyTimer);
    tidyTimer = setTimeout(async () => {
      tidyTimer = null;
      if (!autoTidy || !activePad || restacking) return;
      try {
        const res = await api.restackScratchpad(activePad.id, 'tidy');
        if (res.items_restacked > 0) onChange();
      } catch (e) {
        console.warn('auto-tidy failed:', e);
      }
    }, 1500);
  }
  $effect(() => {
    const sig = layoutSig();
    if (!autoTidy || !activePad) {
      lastLayoutSig = sig;
      return;
    }
    if (sig !== lastLayoutSig) {
      lastLayoutSig = sig;
      scheduleAutoTidy();
    }
  });
</script>

<svelte:window onclick={onWindowPointer} onkeydown={onWindowKey} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="pane"
  class:drop-target={fileDropInProgress}
  ondragover={onPaneDragOver}
  ondragleave={onPaneDragLeave}
  ondrop={onPaneDrop}
>
  <!-- Pad CRUD lives in the header Picker. This bar holds the category
       filter chips (single-select) plus per-pad actions (export, hidden
       badge). Filter persists across scratchpad switches but resets on
       page reload. -->
  <div class="pad-bar" use:collapseOnOverflow={(t) => (padBarTight = t)} class:tight={padBarTight}>
    <span class="restack-wrap" bind:this={restackWrapEl}>
      <button
        class="pad-btn"
        onclick={restackDefault}
        disabled={!activePad || restacking || effectiveView !== 'canvas'}
        title={effectiveView !== 'canvas'
          ? 'Restack is only available in Canvas view'
          : restacking ? 'Restacking…' : 'Compact the grid (repeats your last layout choice — use ▾ to pick another)'}
        aria-label="Restack scratchpad"
      ><span class="btn-ico" style="--sig:#8b5cf6"><Icon name="restack" size={14} /></span><span class="btn-lbl">{restacking ? 'Restacking…' : 'Restack'}</span></button>
      <button
        class="pad-btn caret"
        onclick={() => (restackMenuOpen = !restackMenuOpen)}
        disabled={!activePad || restacking || effectiveView !== 'canvas'}
        title={effectiveView !== 'canvas' ? 'Restack is only available in Canvas view' : 'Choose restack layout'}
        aria-label="Choose restack layout"
        aria-expanded={restackMenuOpen}
      >▾</button>
      {#if restackMenuOpen}
        <div class="restack-menu" role="menu">
          {#each RESTACK_MODES as m (m.mode)}
            <button
              role="menuitem"
              class:current={activePad && loadRestackMode(activePad.id) === m.mode}
              title={m.hint}
              onclick={() => restackActivePad(m.mode)}
            >{m.label}</button>
          {/each}
          <div class="menu-divider"></div>
          <button
            role="menuitemcheckbox"
            aria-checked={autoTidy}
            title="Tidy this pad automatically when items are added, removed, hidden, or grouped. Never resizes cards."
            onclick={toggleAutoTidy}
          >{autoTidy ? '☑' : '☐'} Auto-tidy this pad</button>
        </div>
      {/if}
    </span>
    {#if canvasViewsEnabled}
      <span class="view-toggle" role="group" aria-label="View mode">
        <button
          class="view-btn"
          class:on={effectiveView === 'canvas'}
          onclick={() => (viewMode = 'canvas')}
          title="Spatial canvas"
          aria-pressed={effectiveView === 'canvas'}
        ><span class="btn-ico" style="--sig:#06b6d4"><Icon name="canvas" size={14} /></span><span class="btn-lbl">Canvas</span></button>
        <button
          class="view-btn"
          class:on={effectiveView === 'list'}
          onclick={() => (viewMode = 'list')}
          title="List view"
          aria-pressed={effectiveView === 'list'}
        ><span class="btn-ico" style="--sig:#6366f1"><Icon name="listview" size={14} /></span><span class="btn-lbl">List</span></button>
        <button
          class="view-btn"
          class:on={effectiveView === 'calendar'}
          onclick={() => (viewMode = 'calendar')}
          title="Calendar view (by todo due date)"
          aria-pressed={effectiveView === 'calendar'}
        ><span class="btn-ico" style="--sig:#f59e0b"><Icon name="calendar" size={14} /></span><span class="btn-lbl">Calendar</span></button>
        <button
          class="view-btn"
          class:on={effectiveView === 'kanban'}
          onclick={() => (viewMode = 'kanban')}
          title="Kanban view (by status, per item type)"
          aria-pressed={effectiveView === 'kanban'}
        ><span class="btn-ico" style="--sig:#14b8a6"><Icon name="kanban" size={14} /></span><span class="btn-lbl">Kanban</span></button>
      </span>
    {/if}
    {#if dedupEnabled}
      <button
        class="pad-btn"
        onclick={() => (dedupModalOpen = true)}
        disabled={!activePad}
        title="Find duplicate items and group them (this pad, project, or everywhere)"
        aria-label="Find duplicates"
      ><span class="btn-ico" style="--sig:#ec4899"><Icon name="duplicates" size={14} /></span><span class="btn-lbl">Find duplicates</span></button>
    {/if}
    <button
      class="pad-btn"
      class:on={showArchived}
      onclick={toggleArchived}
      disabled={!activeScratchpadId}
      title="Show archived items (off the canvas, still searchable)"
      aria-pressed={showArchived}
    ><span class="btn-ico" style="--sig:#a78bfa"><Icon name="archived" size={14} /></span><span class="btn-lbl">Archived</span></button>
    <button
      class="pad-btn"
      onclick={exportActivePad}
      disabled={!activePad || exporting}
      title={exporting ? 'Exporting…' : 'Export this scratchpad as a .zip bundle'}
      aria-label="Export scratchpad"
    ><span class="btn-ico" style="--sig:#3b82f6"><Icon name="export" size={14} /></span><span class="btn-lbl">Export</span></button>
    <button
      class="pad-btn"
      onclick={openUploadPicker}
      disabled={!activeScratchpadId || uploadsInFlight > 0}
      title={uploadsInFlight > 0 ? 'Uploading…' : 'Upload an image or file (also: drag-drop onto canvas, or Cmd/Ctrl+V a clipboard image)'}
      aria-label="Upload image or file"
    ><span class="btn-ico" style="--sig:#22c55e"><Icon name="upload" size={14} /></span><span class="btn-lbl">{uploadsInFlight > 0 ? 'Uploading…' : 'Upload'}</span></button>
    {#if selectedItemIds.size > 0}
      <button
        class="pad-btn group-cta"
        onclick={createGroupFromSelection}
        title="Group the selected items (Ctrl/Cmd+click to add or remove from selection)"
        aria-label="Group selected items"
      >§ Group ({selectedItemIds.size})</button>
      <button
        class="pad-btn"
        onclick={clearSelection}
        title="Clear selection"
        aria-label="Clear selection"
      >×</button>
    {/if}
    <input
      bind:this={fileInput}
      type="file"
      multiple
      style="display: none"
      onchange={onUploadInputChange}
    />
    {#if uploadError}
      <span class="upload-err" title={uploadError}>{uploadError}</span>
    {/if}
    <button
      class="pad-btn funnel"
      class:on={filterActive}
      onclick={() => (filterPanelOpen = !filterPanelOpen)}
      title={filterActive
        ? `Filters active: ${filter !== 'all' ? filter.replace('_', ' ') : ''}${filter !== 'all' && hideDone ? ' · ' : ''}${hideDone ? 'open only' : ''} — click to change`
        : 'Filter canvas cards'}
      aria-label="Canvas filters"
      aria-expanded={filterPanelOpen}
    >⛉{#if filterActive}<span class="funnel-dot" aria-hidden="true"></span>{/if}</button>
    {#if hiddenCount > 0}
      <span
        class="hidden-badge"
        title="{hiddenCount} hidden item{hiddenCount === 1 ? '' : 's'}"
      >({hiddenCount} hidden)</span>
    {/if}
  </div>

  <div class="editor">
    <div class="kindbar" role="radiogroup" aria-label="New item kind">
      <button
        type="button"
        class:on={composerKind === 'note'}
        onclick={() => (composerKind = 'note')}
        aria-pressed={composerKind === 'note'}
        title="Type a note — classified into Todo / Bug / KB"
      >≡ Note</button>
      <button
        type="button"
        class:on={composerKind === 'sketch'}
        onclick={() => (composerKind = 'sketch')}
        aria-pressed={composerKind === 'sketch'}
        title="Freehand sketch on an Excalidraw canvas"
      >✎ Sketch</button>
      <button
        type="button"
        class:on={composerKind === 'doc'}
        onclick={() => (composerKind = 'doc')}
        aria-pressed={composerKind === 'doc'}
        title="Composite doc — markdown with inline images"
      >¶ Doc</button>
    </div>

    {#if composerKind === 'note'}
    <textarea
      bind:value={draft}
      onkeydown={handleKey}
      placeholder="Dump anything — code, error, link, todo, note. Ctrl/Cmd+Enter to send."
      rows="4"
    ></textarea>
    <div class="controls">
      <div class="seg" role="radiogroup" aria-label="Classification override">
        <button
          type="button"
          class:on={override === ''}
          onclick={() => (override = '')}
          title="Let the agent decide"
          aria-pressed={override === ''}
        >Auto</button>
        <button
          type="button"
          class:on={override === 'todo'}
          onclick={() => (override = 'todo')}
          title="Force Todo"
          aria-pressed={override === 'todo'}
        >Todo</button>
        <button
          type="button"
          class:on={override === 'bug'}
          onclick={() => (override = 'bug')}
          title="Force Bug"
          aria-pressed={override === 'bug'}
        >Bug</button>
        <button
          type="button"
          class:on={override === 'kb'}
          onclick={() => (override = 'kb')}
          title="Force KB"
          aria-pressed={override === 'kb'}
        >KB</button>
        <button
          type="button"
          class:on={override === 'skip'}
          onclick={() => (override = 'skip')}
          title="Skip — don't classify"
          aria-pressed={override === 'skip'}
        >Skip</button>
      </div>
      {#if bareUrl}
        <button class="send summarize" onclick={summarize} disabled={summarizing || busy} title="Fetch the page and store an AI summary + the link">
          {summarizing ? 'Summarizing…' : '✦ Summarize'}
        </button>
      {/if}
      <button class="send" onclick={submit} disabled={busy || summarizing || !draft.trim()}>
        {busy ? 'Sending…' : getSubmitShortcut() === 'enter' ? 'Send (Enter)' : 'Send (Ctrl+Enter)'}
      </button>
    </div>
    {:else if composerKind === 'sketch'}
    <div class="kind-launch">
      <p class="kind-desc">A freehand sketch on an Excalidraw canvas — diagrams, wireframes, arrows. Opens its own editor.</p>
      <button
        class="send"
        onclick={() => { openNewSketch(); composerKind = 'note'; }}
        disabled={!activeScratchpadId}
      >✎ Open sketch canvas</button>
    </div>
    {:else}
    <div class="kind-launch">
      <p class="kind-desc">A composite doc — markdown with inline images. Good for richer notes and specs. Opens its own editor.</p>
      <button
        class="send"
        onclick={() => { openNewComposite(); composerKind = 'note'; }}
        disabled={!activeScratchpadId}
      >¶ Open doc editor</button>
    </div>
    {/if}
  </div>

  <div class="items">
    {#if effectiveView === 'list'}
      <ListView
        items={filteredItems}
        {derivedStatus}
        {dueDateBySource}
        onOpen={openItem}
        onToggleGroupCollapse={toggleGroupCollapse}
      />
    {:else if effectiveView === 'calendar'}
      <CalendarView
        items={filteredItems}
        {derivedStatus}
        {dueDateBySource}
        onOpen={openItem}
        onSetDueDate={setItemDueDate}
      />
    {:else if effectiveView === 'kanban'}
      <KanbanView
        items={filteredItems}
        {derivedStatus}
        onOpen={openItem}
        onChange={onChange}
      />
    {:else}
      <ScratchpadGrid
        items={filteredItems}
        onLayoutChange={commitLayout}
        onDelete={requestDelete}
        onHide={hideItem}
        onEdit={openItem}
        onReprocess={reprocessItem}
        onMoveItem={(item) => startMove(item)}
        onAccept={doAccept}
        onReclassify={doReclassify}
        onOpenSimilar={onOpenSimilar}
        {onDecompose}
        onDismissSimilar={doDismissSimilar}
        onGroupSimilar={doGroupSimilar}
        {flashItemId}
        {itemAnchors}
        {doneSourceIds}
        {derivedStatus}
        {latestNoteBySource}
        {typeColors}
        {dueDateBySource}
        {tagsBySource}
        {dueDateColors}
        onAnchorBadge={onCardAnchorClick}
        onAnchorBadgeHover={onCardAnchorHover}
        {selectedItemIds}
        {onSelectionChange}
        onToggleGroupCollapse={toggleGroupCollapse}
        onSaveGroupNote={saveGroupNote}
        onRenameGroup={renameGroup}
        onDeleteGroup={deleteGroup}
      />
    {/if}
  </div>

  {#if showArchived}
    <div class="archived-strip">
      <div class="archived-head">Archived ({archivedItems.length})</div>
      {#if archivedLoadFailed}
        <div class="archived-empty archived-err">Couldn't load archived items — try again.</div>
      {:else if archivedItems.length === 0}
        <div class="archived-empty">Nothing archived in this scratchpad.</div>
      {:else}
        <ul class="archived-list">
          {#each archivedItems as a (a.id)}
            <li class="archived-row">
              <span class="archived-text" title={a.content}>{excerpt(a.content)}</span>
              <button class="archived-restore" onclick={() => doUnarchive(a)} title="Un-archive (restore to canvas)">↩ Restore</button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<ScratchpadItemEdit
  item={editingItem}
  onClose={() => (editingItem = null)}
  onSaved={onChange}
  onOpenFile={(path, revision) => {
    editingItem = null;
    onCardAnchorClick({ kind: 'file', path, revision } as import('../lib/types').CodeAnchor);
  }}
/>

<SketchEditor
  open={sketchEditorOpen}
  scratchpadId={activeScratchpadId}
  item={sketchEditorItem}
  onClose={closeSketchEditor}
  onSaved={onChange}
/>

<CompositeEditor
  open={compositeEditorOpen}
  scratchpadId={activeScratchpadId}
  item={compositeEditorItem}
  onClose={closeCompositeEditor}
  onSaved={onChange}
  onOpenFile={(path, revision) => {
    closeCompositeEditor();
    onCardAnchorClick({ kind: 'file', path, revision } as import('../lib/types').CodeAnchor);
  }}
/>

<FileAnchorModal
  open={droppedFile !== null}
  {projectId}
  scratchpadId={activeScratchpadId}
  path={droppedFile}
  onClose={() => (droppedFile = null)}
  onSaved={onChange}
/>

<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete scratchpad item"
  message={pendingDelete ? `Delete “${excerpt(pendingDelete.content)}”? This cannot be undone.` : ''}
  onConfirm={doDelete}
  onCancel={() => (pendingDelete = null)}
/>

{#if archivePrompt}
  <div
    class="ap-backdrop"
    onclick={() => (archivePrompt = null)}
    onkeydown={(e) => e.key === 'Escape' && (archivePrompt = null)}
    role="presentation"
  >
    <div class="ap-box" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="dialog" aria-modal="true" tabindex="-1">
      <div class="ap-title">Delete or archive?</div>
      <div class="ap-msg">“{excerpt(archivePrompt.content)}”</div>
      <div class="ap-actions">
        <button class="ap-btn" onclick={() => (archivePrompt = null)}>Cancel</button>
        <button class="ap-btn archive" onclick={() => archivePrompt && doArchive(archivePrompt)}>Archive</button>
        <button class="ap-btn danger" onclick={() => { const it = archivePrompt; archivePrompt = null; pendingDelete = it; }}>Delete…</button>
      </div>
    </div>
  </div>
{/if}

<Modal
  open={movingItem !== null}
  title="Move to scratchpad"
  onClose={() => { if (!moveBusy) movingItem = null; }}
  width="420px"
>
  {#if movingItem}
    <div class="move-preview">
      <span class="move-label">Item:</span>
      <span class="move-text">“{excerpt(movingItem.content)}”</span>
    </div>
    {#if moveProjects.length > 1}
      <div class="move-project">
        <label class="move-label" for="move-proj">Project:</label>
        <select
          id="move-proj"
          value={moveTargetProjectId}
          disabled={moveBusy}
          onchange={(e) => onMoveProjectChange((e.currentTarget as HTMLSelectElement).value)}
        >
          {#each moveProjects as p (p.id)}
            <option value={p.id}>{p.name}{p.id === projectId ? ' (current)' : ''}</option>
          {/each}
        </select>
      </div>
    {/if}
    <div class="move-list">
      {#if moveLoadingPads}
        <div class="move-muted">Loading scratchpads…</div>
      {:else if moveTargetScratchpads.length === 0}
        <div class="move-muted">No scratchpads in this project.</div>
      {:else}
        {#each moveTargetScratchpads as sp (sp.id)}
          <button
            type="button"
            class="move-row"
            class:current={sp.id === movingItem.scratchpad_id}
            disabled={moveBusy || sp.id === movingItem.scratchpad_id}
            onclick={() => doMove(sp.id)}
          >
            <span class="move-name">{sp.name}</span>
            {#if sp.id === movingItem.scratchpad_id}
              <span class="move-tag">current</span>
            {/if}
          </button>
        {/each}
      {/if}
    </div>
    {#if moveTargetProjectId !== projectId}
      <div class="move-note">Moving across projects re-homes the item and its derived todo/bug. Items with code anchors can’t be moved — remove the anchors first.</div>
    {/if}
    {#if moveErr}<div class="move-err">{moveErr}</div>{/if}
  {/if}
</Modal>


<FilterPanel
  open={filterPanelOpen}
  {filter}
  counts={countFor}
  {hideDone}
  {hiddenByDoneCount}
  onFilterChange={(f) => (filter = f)}
  onToggleHideDone={toggleHideDone}
  onClose={() => (filterPanelOpen = false)}
/>

<DedupModal
  open={dedupModalOpen}
  scratchpadId={activeScratchpadId}
  {projectId}
  onClose={() => (dedupModalOpen = false)}
  onChanged={onChange}
/>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    position: relative;
  }
  .pane.drop-target::after {
    content: 'Drop file to anchor';
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(30, 58, 82, 0.25);
    border: 2px dashed var(--p-2d5578);
    color: var(--p-99ccff);
    font-size: 14px;
    font-family: ui-monospace, monospace;
    letter-spacing: 0.5px;
    pointer-events: none;
    z-index: 100;
  }
  .pad-bar {
    padding: 6px 10px;
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--p-101010);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .pad-btn {
    font-size: 11px;
    padding: 3px 8px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  /* The "Open only" toggle when active — same accent treatment
     as the panel toggles in the header so the on-state reads as
     "currently filtering" rather than "primary action". */
  .pad-btn.on {
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .pad-btn.on:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  /* UC-4 view-mode segmented control. flex:none is load-bearing: overflow:
     hidden (needed for the rounded-corner clip) zeroes a flex item's
     automatic minimum size, which made this the bar's ONLY shrinkable child —
     mid widths squeezed it (clipping Canvas/List/Calendar/Kanban) while the
     bar never actually overflowed, so the tight/icon collapse never fired. */
  .view-toggle {
    display: inline-flex;
    flex: none;
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    overflow: hidden;
  }
  .view-btn {
    font-size: 11px;
    padding: 3px 8px;
    border: none;
    border-radius: 0;
    background: var(--p-1a1a1a);
    color: var(--p-aaaaaa);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .view-btn + .view-btn { border-left: 1px solid var(--p-333333); }
  .view-btn:hover { background: var(--p-262626); color: var(--p-dddddd); }
  .view-btn.on {
    background: var(--p-1a2530);
    color: var(--p-99ccff);
  }
  /* Signature-hue icons on the canvas toolbar: colour the glyph, not the label. */
  .pad-btn .btn-ico, .view-btn .btn-ico { display: inline-flex; align-items: center; color: var(--sig, currentColor); }
  /* Collapse toolbar labels to icons when the BAR overflows (matches the
     header). Overflow-driven, not a viewport query: this bar lives in a pane
     narrowed by the drawer + code canvas, so viewport width says nothing
     about the space it actually has (Slavko's overlapping-buttons bug). */
  .pad-bar.tight .pad-btn .btn-lbl, .pad-bar.tight .view-btn .btn-lbl { display: none; }
  .pad-bar.tight .pad-btn, .pad-bar.tight .view-btn { padding: 4px 7px; }
  .editor {
    padding: 10px;
    border-bottom: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex-shrink: 0;
  }
  textarea {
    width: 100%;
    resize: vertical;
    min-height: 60px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 13px;
  }
  .controls {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .controls .send {
    margin-left: auto;
  }
  .controls .send.summarize {
    margin-left: auto;
    background: var(--p-2a2a2a);
    color: var(--p-cccccc);
  }
  .controls .send.summarize + .send {
    margin-left: 6px;
  }
  /* Composer kind selector (#21) — same segmented look as .seg, sized as the
     composer's primary modality switch. */
  .kindbar {
    display: inline-flex;
    align-self: flex-start;
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    overflow: hidden;
    background: var(--p-111111);
  }
  .kindbar button {
    background: transparent;
    border: none;
    border-right: 1px solid var(--p-2a2a2a);
    color: var(--p-888888);
    padding: 4px 14px;
    font-size: 12px;
    border-radius: 0;
    cursor: pointer;
  }
  .kindbar button:last-child { border-right: none; }
  .kindbar button:hover { color: var(--p-dddddd); background: var(--p-1a1a1a); }
  .kindbar button.on { background: var(--p-1e3a52); color: var(--p-99ccff); }
  /* Sketch/Doc launcher panel — replaces the textarea when those kinds are
     selected, since they edit in their own modal rather than this textbox. */
  .kind-launch {
    display: flex;
    flex-direction: column;
    gap: 10px;
    align-items: flex-start;
    padding: 14px 12px 4px;
  }
  .kind-desc {
    margin: 0;
    font-size: 12px;
    color: var(--p-999999);
    line-height: 1.5;
    max-width: 52ch;
  }
  .seg {
    display: inline-flex;
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    overflow: hidden;
    background: var(--p-111111);
  }
  .seg button {
    background: transparent;
    border: none;
    border-right: 1px solid var(--p-2a2a2a);
    color: var(--p-888888);
    padding: 3px 10px;
    font-size: 11px;
    border-radius: 0;
    letter-spacing: 0.3px;
    cursor: pointer;
  }
  .seg button:last-child { border-right: none; }
  .seg button:hover { color: var(--p-dddddd); background: var(--p-1a1a1a); }
  .seg button.on {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
  }
  .items {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }
  .group-cta {
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .group-cta:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .upload-err {
    color: var(--p-ff8888);
    font-size: 11px;
    padding: 2px 6px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hidden-badge {
    color: var(--p-888888);
    font-size: 10px;
    font-style: italic;
    margin-left: auto;
    padding: 0 4px;
    cursor: help;
  }
  .move-preview {
    display: flex;
    gap: 8px;
    align-items: baseline;
    margin-bottom: 12px;
    padding-bottom: 10px;
    border-bottom: 1px solid var(--p-262626);
  }
  .move-label {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    flex-shrink: 0;
  }
  .move-text {
    color: var(--p-dddddd);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .move-project {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-bottom: 10px;
  }
  .move-project select {
    flex: 1;
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 5px 8px;
    font-size: 12px;
    font-family: inherit;
  }
  .move-muted {
    color: var(--p-777777);
    font-size: 12px;
    font-style: italic;
    padding: 8px 4px;
  }
  .move-note {
    color: var(--p-99aabb);
    font-size: 11px;
    line-height: 1.4;
    margin-top: 10px;
    padding: 6px 8px;
    background: var(--p-141414);
    border-radius: 3px;
  }
  .move-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 320px;
    overflow-y: auto;
  }
  .move-row {
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: 1px solid var(--p-262626);
    color: var(--p-dddddd);
    padding: 8px 10px;
    cursor: pointer;
    border-radius: 3px;
    text-align: left;
    font-size: 13px;
  }
  .move-row:hover:not(:disabled) {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .move-row.current {
    color: var(--p-888888);
    background: var(--p-161616);
    cursor: default;
  }
  .move-name { flex: 1; }
  .move-tag {
    font-size: 10px;
    color: var(--p-666666);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .move-err {
    color: var(--p-ff8888);
    font-size: 12px;
    margin-top: 10px;
  }
  .restack-wrap {
    position: relative;
    display: inline-flex;
  }
  .restack-wrap .caret {
    padding-left: 4px;
    padding-right: 4px;
    margin-left: -1px;
  }
  .restack-menu {
    position: absolute;
    top: 100%;
    /* Anchor LEFT: since v0.10.10 removed the filter chips, Restack is
       the bar's first element — a right-anchored menu hangs past the
       pane's left edge and clips (Backlog regression, fixed v0.10.13). */
    left: 0;
    margin-top: 2px;
    background: var(--p-161616);
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    display: flex;
    flex-direction: column;
    min-width: 150px;
    z-index: 50;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }
  .restack-menu button {
    background: transparent;
    border: none;
    color: var(--p-cccccc);
    text-align: left;
    font-size: 12px;
    padding: 6px 10px;
    cursor: pointer;
  }
  .restack-menu button:hover { background: var(--p-222222); color: var(--p-ffffff); }
  .restack-menu button.current { color: var(--p-99ccff); }
  .restack-menu .menu-divider {
    border-top: 1px solid var(--p-2a2a2a);
    margin: 2px 0;
  }
  .pad-btn.funnel {
    position: relative;
    padding-left: 7px;
    padding-right: 7px;
  }
  .pad-btn.funnel.on { color: var(--p-99ccff); }
  .funnel-dot {
    position: absolute;
    top: 3px;
    right: 3px;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--p-99ccff);
  }

  /* Archive (item #19): delete/archive prompt + the archived strip. */
  .ap-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
  }
  .ap-box {
    background: var(--p-1e1e1e);
    border: 1px solid var(--p-3a3a3a);
    border-radius: 6px;
    padding: 16px;
    width: min(420px, 90vw);
  }
  .ap-title { font-size: 14px; color: var(--p-e0e0e0); margin-bottom: 6px; }
  .ap-msg { font-size: 12px; color: var(--p-cccccc); margin-bottom: 14px; }
  .ap-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .ap-btn {
    background: var(--p-2a2a2a); border: 1px solid var(--p-3a3a3a);
    border-radius: 4px; color: var(--p-e0e0e0); padding: 6px 12px; font-size: 12px; cursor: pointer;
  }
  .ap-btn.danger { color: var(--p-e07a7a); border-color: var(--p-e07a7a); }
  .ap-btn.archive { color: var(--p-99ccff); }

  .archived-strip {
    border-top: 1px solid var(--p-3a3a3a);
    padding: 8px 12px;
    max-height: 30vh;
    overflow-y: auto;
  }
  .archived-head {
    font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em;
    color: var(--p-777777); margin-bottom: 6px;
  }
  .archived-empty { font-size: 12px; color: var(--p-777777); }
  .archived-err { color: var(--p-ff8888); }
  .archived-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 3px; }
  .archived-row { display: flex; align-items: center; gap: 8px; }
  .archived-text {
    flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-size: 12px; color: var(--p-999999);
  }
  .archived-restore {
    flex: none; background: none; border: 1px solid var(--p-3a3a3a); border-radius: 4px;
    color: var(--p-cccccc); padding: 2px 8px; font-size: 11px; cursor: pointer;
  }
</style>
