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
  import type { Snippet } from 'svelte';
  import { registerModal, unregisterModal, isTopModal } from '../lib/modalStack';

  type Props = {
    open: boolean;
    title?: string;
    onClose: () => void;
    children: Snippet;
    footer?: Snippet;
    width?: string;
    // Stacking layer. Modals default to 300 (above the 200 slide-out
    // panels); the dialog host passes 400 so alert/confirm/prompt always
    // sit above any open modal.
    z?: number;
  };
  let { open, title, onClose, children, footer, width = '720px', z = 300 }: Props = $props();

  // Register this modal in the shared stack while open, so Escape closes only
  // the TOPMOST one, not every stacked layer at once (audit M23).
  let modalId = 0;
  $effect(() => {
    if (open) {
      modalId = registerModal(z);
      return () => unregisterModal(modalId);
    }
  });
  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape' && isTopModal(modalId)) onClose();
  }

  // Drag the dialog by its header — a persistent offset from the centered
  // position (the layout stays flex-centered; we just translate). Reset each
  // time the modal opens so a re-open lands centered.
  let dx = $state(0);
  let dy = $state(0);
  let drag = $state<{ sx: number; sy: number; ox: number; oy: number } | null>(null);
  let dialogEl = $state<HTMLElement | undefined>(undefined);
  $effect(() => {
    if (open) { dx = 0; dy = 0; }
  });
  function startDrag(e: PointerEvent) {
    // Ignore drags starting on the close button.
    if ((e.target as HTMLElement)?.closest('.close')) return;
    e.preventDefault();
    drag = { sx: e.clientX, sy: e.clientY, ox: dx, oy: dy };
    window.addEventListener('pointermove', onDragMove);
    window.addEventListener('pointerup', endDrag, { once: true });
  }
  function onDragMove(e: PointerEvent) {
    if (!drag) return;
    let nx = drag.ox + (e.clientX - drag.sx);
    let ny = drag.oy + (e.clientY - drag.sy);
    // Clamp so the dialog can NEVER be dragged out of the viewport — its
    // buttons must always stay clickable. (This is the bug that trapped users
    // in an "un-dismissable" dialog: OK/X worked, they were just off-screen.)
    if (dialogEl) {
      const r = dialogEl.getBoundingClientRect();
      const m = 8; // keep at least this margin from every edge
      const wouldLeft = r.left + (nx - dx);
      const wouldTop = r.top + (ny - dy);
      const maxLeft = Math.max(m, window.innerWidth - r.width - m);
      const maxTop = Math.max(m, window.innerHeight - r.height - m);
      nx += Math.min(Math.max(wouldLeft, m), maxLeft) - wouldLeft;
      ny += Math.min(Math.max(wouldTop, m), maxTop) - wouldTop;
    }
    dx = nx;
    dy = ny;
  }
  function endDrag() {
    drag = null;
    window.removeEventListener('pointermove', onDragMove);
  }
</script>

<svelte:window onkeydown={onKey} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="backdrop"
    style="z-index: {z}"
    role="presentation"
    onclick={onClose}
  >
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
      class="dialog"
      role="dialog"
      aria-modal="true"
      tabindex="-1"
      bind:this={dialogEl}
      style="max-width: {width}; transform: translate({dx}px, {dy}px)"
      onclick={(e) => e.stopPropagation()}
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <header class="drag-handle" class:dragging={drag !== null} onpointerdown={startDrag} title="Drag to move">
        <h2>{title ?? ''}</h2>
        <button class="close" onclick={onClose} aria-label="Close">×</button>
      </header>
      <div class="body">
        {@render children()}
      </div>
      {#if footer}
        <div class="footer">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    /* Modals always win over slide-out panels (Matches, Lists,
       Search, Ask, Dashboard — all at z-index 200). Without
       this, opening an item modal from inside an open panel
       leaves the modal partially covered with no way to
       interact with the hidden parts. */
    z-index: 300;
    padding: 24px;
  }
  .dialog {
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-3a3a3a);
    border-radius: 6px;
    width: 100%;
    max-height: calc(100vh - 48px);
    display: flex;
    flex-direction: column;
    min-height: 0;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
  }
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--p-333333);
    flex-shrink: 0;
  }
  .drag-handle { cursor: grab; touch-action: none; user-select: none; }
  .drag-handle.dragging { cursor: grabbing; }
  .drag-handle .close { cursor: pointer; }
  h2 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.5px;
    color: var(--p-eeeeee);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .close {
    border: none;
    background: transparent;
    color: var(--p-888888);
    font-size: 20px;
    line-height: 1;
    padding: 0 6px;
    cursor: pointer;
  }
  .close:hover { color: var(--p-ffffff); }
  .body {
    padding: 14px;
    overflow: auto;
    min-height: 0;
    flex: 1;
  }
  .footer {
    padding: 10px 14px;
    border-top: 1px solid var(--p-333333);
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    flex-shrink: 0;
  }
</style>
