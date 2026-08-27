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
  import type { ScratchpadItem } from '../lib/types';
  import Modal from './Modal.svelte';

  // Modal that hosts the Excalidraw iframe (built separately under
  // web/excalidraw-iframe/, vendored to web/public/excalidraw/).
  // The iframe and this component communicate via postMessage —
  // protocol documented in excalidraw-iframe/src/main.tsx.
  //
  // Two modes:
  //   - existing item: loads scene from item.content (JSON), saves
  //     via api.updateItem.
  //   - new sketch: opens with a blank canvas, saves via
  //     api.createItem with content_type='sketch'.

  type Props = {
    open: boolean;
    scratchpadId: string;
    // The item being edited. null = new sketch.
    item: ScratchpadItem | null;
    onClose: () => void;
    onSaved: () => void;
  };
  let { open, scratchpadId, item, onClose, onSaved }: Props = $props();

  let iframe = $state<HTMLIFrameElement | null>(null);
  let iframeReady = $state(false);
  let saving = $state(false);
  let err = $state('');

  // Parse the item's stored scene JSON. Returns null on empty /
  // malformed content (which becomes a fresh canvas in the editor).
  function loadedScene(): unknown {
    if (!item || !item.content) return null;
    try {
      return JSON.parse(item.content);
    } catch {
      return null;
    }
  }

  // Message handler — listens for 'ready' (so we know we can send
  // the init), and 'save' (the response to request-save with the
  // current scene + preview).
  function onMessage(e: MessageEvent) {
    if (!iframe || e.source !== iframe.contentWindow) return;
    const msg = e.data;
    if (!msg || typeof msg !== 'object') return;
    if (msg.type === 'ready') {
      iframeReady = true;
      iframe.contentWindow?.postMessage({ type: 'init', scene: loadedScene() }, '*');
    } else if (msg.type === 'save') {
      void persistSave(msg.scene, msg.previewDataURL ?? '');
    }
  }

  async function persistSave(scene: unknown, previewDataURL: string) {
    saving = true;
    err = '';
    try {
      const json = JSON.stringify(scene);

      // Step 1: ensure the item exists with the scene JSON. For an
      // existing sketch we patch; for a new one we create + capture
      // the resulting id so step 3 can target it.
      let targetID: string;
      if (item) {
        await api.updateItem(item.id, { content: json });
        targetID = item.id;
      } else {
        const created = await api.createItem(scratchpadId, {
          content: json,
          content_type: 'sketch',
        });
        targetID = created.id;
      }

      // Step 2: upload the preview PNG (if Excalidraw produced one)
      // via the standalone /blobs endpoint. Best-effort — preview
      // failure shouldn't fail the save; the card just falls back
      // to the placeholder renderer.
      if (previewDataURL) {
        try {
          const file = api.dataURLtoFile(previewDataURL, 'sketch-preview.png');
          if (file) {
            const blob = await api.uploadBlobOnly(file, scratchpadId);
            // Step 3: patch the item with the preview's blob_sha so
            // ScratchpadGrid renders the thumbnail instead of the
            // placeholder.
            await api.updateItem(targetID, {
              blob_sha: blob.sha,
              mime_type: blob.mime_type,
              byte_size: blob.size,
              width: blob.width ?? 0,
              height: blob.height ?? 0,
            });
          }
        } catch (e) {
          // Surface but don't block — the sketch itself saved.
          console.warn('sketch preview upload failed:', e);
        }
      }

      onSaved();
      onClose();
    } catch (e) {
      err = `Save failed: ${e}`;
    } finally {
      saving = false;
    }
  }

  // Trigger save: ask the iframe for the current scene; it replies
  // with 'save' which routes to persistSave.
  function triggerSave() {
    if (!iframe?.contentWindow) return;
    iframe.contentWindow.postMessage({ type: 'request-save' }, '*');
  }

  // Reset state on open / item change so a previously-loaded scene
  // doesn't bleed into the next session.
  let lastKey = '';
  $effect(() => {
    const key = open ? (item?.id ?? '__new__') : '';
    if (key !== lastKey) {
      lastKey = key;
      iframeReady = false;
      err = '';
    }
  });

  $effect(() => {
    if (open) {
      window.addEventListener('message', onMessage);
      return () => window.removeEventListener('message', onMessage);
    }
  });
</script>

<Modal open={open} title={item ? `Edit sketch — ${item.name || '(untitled)'}` : 'New sketch'} onClose={onClose} width="95vw">
  {#snippet children()}
    <div class="frame-wrap">
      {#if err}
        <div class="err">{err}</div>
      {/if}
      <iframe
        bind:this={iframe}
        title="Excalidraw editor"
        src="/excalidraw/"
        sandbox="allow-scripts allow-same-origin"
      ></iframe>
    </div>
  {/snippet}
  {#snippet footer()}
    <span class="muted">{iframeReady ? 'Ready' : 'Loading editor…'}</span>
    <div class="grow"></div>
    <button class="btn ghost" onclick={onClose} disabled={saving}>Cancel</button>
    <button class="btn primary" onclick={triggerSave} disabled={saving || !iframeReady}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .frame-wrap {
    width: 100%;
    height: 70vh;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--p-0a0a0a);
  }
  iframe {
    flex: 1;
    width: 100%;
    border: none;
    background: var(--p-0a0a0a);
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
