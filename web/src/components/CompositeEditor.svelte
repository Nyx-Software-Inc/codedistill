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
  import { onMount } from 'svelte';
  import { Carta, MarkdownEditor } from 'carta-md';
  import 'carta-md/default.css';
  import * as api from '../lib/api';
  import type { ScratchpadItem } from '../lib/types';
  import Modal from './Modal.svelte';
  import Throughline from './Throughline.svelte';

  // Composite-doc editor — markdown with inline image refs,
  // Notion-style in spirit but source-mode in implementation.
  // Carta provides the markdown editor + split-pane preview; we
  // wire a custom paste handler that uploads pasted images via the
  // standalone /blobs endpoint and inserts a markdown ref at the
  // cursor.
  //
  // Image refs use `![alt](/api/v1/blobs/<sha>)` so the existing
  // server-side blob serving + GC sweeper (with the Slice 4.5
  // ListCompositeImageShas widening) handle them automatically.

  type Props = {
    open: boolean;
    scratchpadId: string;
    // null = new composite doc, populated = edit existing
    item: ScratchpadItem | null;
    onClose: () => void;
    onSaved: () => void;
    // Jump from a throughline file to the code canvas (optional).
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { open, scratchpadId, item, onClose, onSaved, onOpenFile }: Props = $props();

  // Composite docs are source items that can carry code anchors, so they get
  // the same intent→commit→code Throughline as the derived detail modals —
  // otherwise the lineage + code metrics would be invisible for a doc-type
  // use case (the editor below only knows Markdown).
  let activeTab = $state<'doc' | 'throughline'>('doc');
  function firstLine(s: string): string {
    return (s || '').split('\n').map((l) => l.trim()).find((l) => l) ?? '';
  }

  const carta = new Carta({
    // The default Carta sanitizer is OK for now. Future patches
    // can plug in a strict allowlist if composite docs ever
    // accept untrusted content.
    sanitizer: false,
  });

  let value = $state('');
  let saving = $state(false);
  let err = $state('');
  let uploadingCount = $state(0);
  let editorEl = $state<HTMLDivElement | null>(null);

  // Reset content + reattach paste listener whenever the modal
  // opens on a new item.
  let lastKey = '';
  $effect(() => {
    const key = open ? (item?.id ?? '__new__') : '';
    if (key !== lastKey) {
      lastKey = key;
      value = item?.content ?? '';
      err = '';
      activeTab = 'doc';
    }
  });

  // Find the editor's underlying <textarea> and attach an
  // image-paste handler. Carta doesn't expose a paste hook on the
  // component, so we reach into the DOM after mount. The textarea
  // is created on first render of MarkdownEditor; querySelector
  // inside an effect that runs when the modal opens.
  $effect(() => {
    if (!open || !editorEl) return;
    const ta = editorEl.querySelector('textarea');
    if (!ta) return;
    ta.addEventListener('paste', onPaste);
    return () => ta.removeEventListener('paste', onPaste);
  });

  async function onPaste(e: ClipboardEvent) {
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
    // Prevent the default paste — we're handling these bytes ourselves.
    e.preventDefault();

    const ta = e.currentTarget as HTMLTextAreaElement;
    for (const f of files) {
      uploadingCount++;
      try {
        const blob = await api.uploadBlobOnly(f, scratchpadId);
        const ref = `![${f.name}](/api/v1/blobs/${blob.sha})`;
        insertAtCursor(ta, ref);
      } catch (e) {
        err = `Image paste failed: ${e}`;
      } finally {
        uploadingCount--;
      }
    }
  }

  // Insert text at the textarea's current cursor + update the
  // bound Svelte state so the preview re-renders.
  function insertAtCursor(ta: HTMLTextAreaElement, text: string) {
    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const before = ta.value.slice(0, start);
    const after = ta.value.slice(end);
    const next = `${before}${text}${after}`;
    ta.value = next;
    value = next;
    // Restore cursor to immediately after the inserted text.
    const pos = start + text.length;
    ta.setSelectionRange(pos, pos);
    // Fire an input event so Carta picks up the change.
    ta.dispatchEvent(new Event('input', { bubbles: true }));
  }

  async function save() {
    if (saving) return;
    saving = true;
    err = '';
    try {
      if (item) {
        await api.updateItem(item.id, { content: value });
      } else {
        await api.createItem(scratchpadId, {
          content: value,
          content_type: 'composite',
        });
      }
      onSaved();
      onClose();
    } catch (e) {
      err = `Save failed: ${e}`;
    } finally {
      saving = false;
    }
  }
</script>

<Modal open={open} title={item ? `Edit doc — ${item.name || '(untitled)'}` : 'New doc'} onClose={onClose} width="95vw">
  {#snippet children()}
    {#if err}
      <div class="err">{err}</div>
    {/if}
    {#if item}
      <div class="modal-tabs">
        <button class="modal-tab" class:active={activeTab === 'doc'} onclick={() => (activeTab = 'doc')}>Document</button>
        <button class="modal-tab" class:active={activeTab === 'throughline'} onclick={() => (activeTab = 'throughline')}>Throughline</button>
      </div>
    {/if}
    <!-- Keep the editor mounted (hidden) so Carta state + the paste handler survive tab switches. -->
    <div class="editor-wrap" class:hidden={activeTab === 'throughline'} bind:this={editorEl}>
      <MarkdownEditor {carta} bind:value mode="split" />
    </div>
    {#if activeTab === 'throughline' && item}
      <div class="throughline-wrap">
        <Throughline
          ownerType="scratchpad_item"
          ownerId={item.id}
          intentTitle={item.name || firstLine(value)}
          {onOpenFile}
        />
      </div>
    {/if}
    {#if activeTab === 'doc'}
      <div class="hint">
        Paste images to upload + embed inline. Markdown is rendered live in the right pane.
      </div>
    {/if}
  {/snippet}
  {#snippet footer()}
    {#if uploadingCount > 0}
      <span class="muted">Uploading {uploadingCount} image{uploadingCount === 1 ? '' : 's'}…</span>
    {/if}
    <div class="grow"></div>
    <button class="btn ghost" onclick={onClose} disabled={saving}>Cancel</button>
    <button class="btn primary" onclick={save} disabled={saving || uploadingCount > 0}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .modal-tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--p-262626);
    margin-bottom: 8px;
  }
  .modal-tab {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--p-888888);
    font-size: 13px;
    padding: 6px 12px;
    cursor: pointer;
  }
  .modal-tab:hover { color: var(--p-cccccc); }
  .modal-tab.active { color: var(--p-eeeeee); border-bottom-color: var(--p-66ccff); }
  .editor-wrap {
    width: 100%;
    height: 65vh;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--p-0a0a0a);
  }
  .editor-wrap.hidden { display: none; }
  .throughline-wrap {
    width: 100%;
    max-height: 65vh;
    overflow-y: auto;
    padding: 2px;
  }
  /* Carta hard-codes height:600px on .carta-input/.carta-renderer and the flex
     chain above them never carries the modal's height down, so a long paste
     grew the editor past the modal and overlaid the UI below (bug 110). Make
     the whole chain fill the 65vh wrap and scroll INTERNALLY, and clip at the
     editor so nothing can ever escape the modal again. */
  .editor-wrap :global(.carta-editor) {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  .editor-wrap :global(.carta-wrapper) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .editor-wrap :global(.carta-container) {
    flex: 1;
    min-height: 0;
  }
  /* Override the fixed 600px — flex stretch sizes them to the container, and
     each pane scrolls its own overflow instead of pushing past the modal. */
  .editor-wrap :global(.carta-input),
  .editor-wrap :global(.carta-renderer) {
    height: auto;
    min-height: 0;
    overflow-y: auto;
  }
  .hint {
    color: var(--p-777777);
    font-size: 11px;
    font-style: italic;
    margin-top: 6px;
  }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 6px 10px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
    margin-bottom: 6px;
  }
  .muted {
    color: var(--p-777777);
    font-size: 11px;
    font-style: italic;
  }
  .btn {
    padding: 5px 14px;
    font-size: 12px;
    border-radius: 3px;
    cursor: pointer;
    border: 1px solid var(--p-333333);
    background: var(--p-1a1a1a);
    color: var(--p-cccccc);
  }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn.ghost:hover:not(:disabled) { background: var(--p-262626); color: var(--p-ffffff); }
  .btn.primary {
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .grow { flex: 1; }
</style>
