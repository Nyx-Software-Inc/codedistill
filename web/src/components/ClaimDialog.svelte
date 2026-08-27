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
  // Claim dialog (backlog use case 6e4fe9088a484d36). Replaces the old
  // window.prompt: a real in-app modal pre-filled with the current user's
  // identity — the license holder in single-user mode, the signed-in user in
  // multi-user — so claiming is usually one click. The field stays editable so
  // you can claim on behalf of an agent id or a teammate.
  import * as api from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = {
    open: boolean;
    busy?: boolean;
    onClaim: (name: string) => void;
    onClose: () => void;
  };
  let { open, busy = false, onClaim, onClose }: Props = $props();

  let name = $state('');
  let touched = $state(false);
  let inputEl: HTMLInputElement | undefined = $state();

  // On open, pre-fill from /me once (don't clobber anything the user typed).
  $effect(() => {
    if (!open) {
      touched = false;
      name = '';
      return;
    }
    if (!touched && name === '') {
      api.getMe().then((m) => { if (!touched && name === '') name = m.display_name; }).catch(() => {});
    }
  });
  // Focus the field when the modal appears.
  $effect(() => { if (open && inputEl) inputEl.focus(); });

  function submit() {
    const n = name.trim();
    if (n) onClaim(n);
  }
</script>

<Modal {open} title="Claim this item" width="380px" {onClose}>
  {#snippet children()}
    <div class="claim">
      <label for="claim-name">Claim as</label>
      <input
        id="claim-name"
        bind:this={inputEl}
        bind:value={name}
        oninput={() => (touched = true)}
        placeholder="your name or agent id"
        spellcheck="false"
        disabled={busy}
        onkeydown={(e) => { if (e.key === 'Enter') submit(); }}
      />
      <p class="hint">Pre-filled from your profile. Edit it to claim for an agent or teammate.</p>
    </div>
  {/snippet}
  {#snippet footer()}
    <button class="ghost" onclick={onClose} disabled={busy}>Cancel</button>
    <button class="primary" onclick={submit} disabled={busy || !name.trim()}>{busy ? 'Claiming…' : 'Claim'}</button>
  {/snippet}
</Modal>

<style>
  .claim { display: flex; flex-direction: column; gap: 8px; }
  label { font-size: 12px; color: var(--p-aaaaaa); }
  input {
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-eeeeee);
    border-radius: 5px; padding: 8px 10px; font-size: 13px;
  }
  input:focus { outline: none; border-color: var(--p-2d5578); }
  .hint { font-size: 11px; color: var(--p-777777); margin: 0; }
  .ghost, .primary { font-size: 12px; padding: 6px 14px; border-radius: 5px; cursor: pointer; }
  .ghost { background: transparent; border: 1px solid var(--p-333333); color: var(--p-999999); }
  .primary { background: var(--p-2d5578); border: 1px solid var(--p-2d5578); color: var(--p-cceeff); }
  .primary:disabled { opacity: 0.5; cursor: default; }
</style>
