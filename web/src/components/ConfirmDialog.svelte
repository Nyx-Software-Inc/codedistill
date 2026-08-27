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
  import Modal from './Modal.svelte';

  type Props = {
    open: boolean;
    title?: string;
    message: string;
    confirmLabel?: string;
    danger?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  };
  let {
    open,
    title = 'Confirm',
    message,
    confirmLabel = 'Delete',
    danger = true,
    onConfirm,
    onCancel,
  }: Props = $props();

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Enter') {
      e.preventDefault();
      onConfirm();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<Modal {open} {title} onClose={onCancel} width="440px">
  <p class="msg">{message}</p>
  <div class="actions">
    <button class="cancel" onclick={onCancel}>Cancel</button>
    <button class:danger onclick={onConfirm}>{confirmLabel}</button>
  </div>
</Modal>

<style>
  .msg {
    margin: 0 0 14px 0;
    font-size: 13px;
    color: var(--p-dddddd);
    line-height: 1.5;
  }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
  .cancel {
    background: transparent;
  }
  .danger {
    background: var(--p-5a1515);
    color: var(--p-ffdddd);
    border-color: var(--p-732525);
  }
  .danger:hover {
    background: var(--p-732525);
  }
</style>
