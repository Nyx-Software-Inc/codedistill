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
  // Gmail-style profile menu (backlog use case 47e8a48972e0cb93). The avatar
  // replaces the old settings gear; clicking it opens a menu to edit your name,
  // see your license, open settings, reach Admin (multi-user owners/admins), and
  // sign out (multi-user). The window title separately shows the LICENSE holder
  // (anti-theft watermark) — that's handled in App.svelte, not here.
  import * as api from '../lib/api';

  type Props = {
    me: api.Me | null;
    license: api.LicenseInfo | null;
    onOpenSettings: () => void;
    onOpenAdmin: () => void;
    onOpenStats: () => void;
    onUpdated: () => void;
  };
  let { me, license, onOpenSettings, onOpenAdmin, onOpenStats, onUpdated }: Props = $props();

  let open = $state(false);
  let editing = $state(false);
  let nameDraft = $state('');
  let saving = $state(false);
  let err = $state('');

  // Redeem a purchase claim code (activation service slice 2). On success the
  // license file is installed server-side; features apply after a restart.
  let redeeming = $state(false);
  let redeemOpen = $state(false);
  let claimCode = $state('');
  let redeemMsg = $state('');
  let redeemErr = $state('');
  async function doRedeem() {
    const code = claimCode.trim();
    if (!code || redeeming) return;
    redeeming = true;
    redeemErr = '';
    redeemMsg = '';
    try {
      const r = await api.redeemLicense(code);
      redeemMsg = `Licensed to ${r.customer} (${r.edition}) — restart CodeDistill to apply.`;
      claimCode = '';
      redeemOpen = false;
      onUpdated();
    } catch (e) {
      redeemErr = String(e);
    } finally {
      redeeming = false;
    }
  }
  async function doRefresh() {
    if (redeeming) return;
    redeeming = true;
    redeemErr = '';
    redeemMsg = '';
    try {
      const r = await api.refreshLicense();
      redeemMsg = `License refreshed — valid to ${new Date(r.expires_at).toLocaleDateString()}. Restart to apply.`;
      onUpdated();
    } catch (e) {
      redeemErr = String(e);
    } finally {
      redeeming = false;
    }
  }

  const initials = $derived.by(() => {
    const n = (me?.display_name || '?').trim();
    const parts = n.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return n.slice(0, 2).toUpperCase();
  });

  function toggle() {
    open = !open;
    editing = false;
    err = '';
  }
  function startEdit() {
    nameDraft = me?.display_name ?? '';
    editing = true;
    err = '';
  }
  async function saveName() {
    const name = nameDraft.trim();
    if (!name) return;
    saving = true;
    err = '';
    try {
      await api.updateMe(name);
      editing = false;
      onUpdated();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Could not save';
    } finally {
      saving = false;
    }
  }
  async function signOut() {
    try {
      await api.logout();
    } catch {
      /* ignore */
    }
    window.location.reload();
  }
</script>

<!--
  Outside-click close. Use composedPath() (the event path, snapshotted at
  dispatch) rather than e.target.closest('.profile'): clicking "Edit name"
  swaps in the inline editor, detaching the clicked button BEFORE this bubbled
  window handler runs. A detached target's closest() returns null, which used
  to read as an outside-click and wrongly close the menu (edit UI never showed).
  composedPath survives the re-render, so an in-menu click is still recognized.
-->
<svelte:window onclick={(e) => {
  if (open && !e.composedPath().some((n) => (n as HTMLElement)?.classList?.contains?.('profile'))) open = false;
}} />

<div class="profile">
  <button class="avatar" onclick={toggle} title={me?.display_name ?? 'Profile'} aria-label="Profile menu" aria-expanded={open}>
    {initials}
  </button>

  {#if open}
    <div class="menu" role="menu">
      <div class="who">
        <div class="big">{me?.display_name ?? 'Local User'}</div>
        {#if me?.real_name && me.real_name !== me.display_name}<div class="sub">{me.real_name}</div>{/if}
        {#if me?.email}<div class="sub">{me.email}</div>{/if}
      </div>

      {#if editing}
        <div class="edit">
          <input bind:value={nameDraft} placeholder="Your name" spellcheck="false" disabled={saving}
            onkeydown={(e) => { if (e.key === 'Enter') saveName(); if (e.key === 'Escape') editing = false; }} />
          <div class="edit-row">
            <button class="primary" onclick={saveName} disabled={saving || !nameDraft.trim()}>Save</button>
            <button class="ghost" onclick={() => (editing = false)} disabled={saving}>Cancel</button>
          </div>
          {#if err}<div class="err">{err}</div>{/if}
        </div>
      {:else}
        <button class="item" role="menuitem" onclick={startEdit}>✎ Edit name</button>
      {/if}

      <div class="sep"></div>

      {#if license}
        <div class="license">
          {#if license.oss_build}
            Community edition
          {:else if license.customer}
            Licensed to {license.customer}{#if license.edition} · {license.edition}{/if}
          {:else}
            Unlicensed (free)
          {/if}
        </div>
      {/if}
      <!-- Which server this client is talking to. Matters most in PWA mode,
           where there's no URL bar — and disambiguates a desktop localhost
           from the team server. location.host is where requests actually go
           (behind a proxy that's the name you connect by, the right answer). -->
      <div class="license server-row" title="The server this window reads and writes">
        Server: {location.host}
      </div>

      {#if license?.oss_build}
        <!-- The CE binary physically lacks the paid packages (protect-by-
             absence), so a redeemed license would unlock nothing — point at
             the real upgrade path instead of offering a dead redeem. -->
        <div class="license">Purchased a license? Install the official build from codedistill.dev, then redeem your code there.</div>
      {:else if redeemOpen}
        <div class="edit">
          <input
            placeholder="cdk_… purchase claim code"
            bind:value={claimCode}
            onkeydown={(e) => e.key === 'Enter' && doRedeem()}
          />
          <div class="edit-row">
            <button class="item" onclick={doRedeem} disabled={redeeming}>{redeeming ? 'Redeeming…' : 'Redeem'}</button>
            <button class="item" onclick={() => { redeemOpen = false; redeemErr = ''; }}>Cancel</button>
          </div>
        </div>
      {:else}
        <button class="item" role="menuitem" onclick={() => (redeemOpen = true)}>🔑 Redeem purchase code…</button>
        {#if license && !license.oss_build && license.customer}
          <button class="item" role="menuitem" onclick={doRefresh} disabled={redeeming}>{redeeming ? 'Refreshing…' : '⟳ Refresh license'}</button>
        {/if}
      {/if}
      {#if redeemMsg}<div class="license redeem-ok">{redeemMsg}</div>{/if}
      {#if redeemErr}<div class="license redeem-err">{redeemErr}</div>{/if}

      <button class="item" role="menuitem" onclick={() => { open = false; onOpenStats(); }}>📊 Stats</button>
      <button class="item" role="menuitem" onclick={() => { open = false; onOpenSettings(); }}>⚙ Settings</button>
      {#if me?.multi_user && me?.is_admin}
        <button class="item" role="menuitem" onclick={() => { open = false; onOpenAdmin(); }}>👥 Admin</button>
      {/if}
      {#if me?.multi_user}
        <div class="sep"></div>
        <button class="item" role="menuitem" onclick={signOut}>↪ Sign out</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .profile { position: relative; display: inline-flex; }
  .avatar {
    width: 28px; height: 28px; border-radius: 50%; border: 1px solid var(--p-3a3a3a);
    background: var(--p-2d5578); color: var(--p-eaf4ff); font-size: 11px; font-weight: 600;
    cursor: pointer; display: inline-flex; align-items: center; justify-content: center;
    letter-spacing: 0.3px;
  }
  .avatar:hover { border-color: var(--p-99ccff); }
  .menu {
    position: absolute; top: calc(100% + 6px); right: 0; z-index: 300; min-width: 230px;
    background: var(--p-161616); border: 1px solid var(--p-333333); border-radius: 8px;
    box-shadow: 0 8px 26px rgba(0, 0, 0, 0.55); padding: 8px; display: flex; flex-direction: column; gap: 2px;
  }
  .who { padding: 6px 8px 8px; }
  .who .big { font-size: 13px; color: var(--p-f0f0f0); font-weight: 600; }
  .who .sub { font-size: 11px; color: var(--p-888888); margin-top: 1px; }
  .sep { height: 1px; background: var(--p-2a2a2a); margin: 4px 0; }
  .item {
    background: transparent; border: none; color: var(--p-dddddd); text-align: left;
    padding: 7px 8px; font-size: 12px; border-radius: 5px; cursor: pointer;
  }
  .item:hover { background: var(--p-252525); }
  .license { font-size: 11px; color: var(--p-888888); padding: 4px 8px; }
  .redeem-ok { color: var(--p-99cc99); white-space: normal; }
  .redeem-err { color: var(--p-ff8888); white-space: normal; }
  .edit { padding: 4px 8px; display: flex; flex-direction: column; gap: 6px; }
  .edit input {
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-eeeeee);
    border-radius: 4px; padding: 5px 8px; font-size: 12px;
  }
  .edit-row { display: flex; gap: 6px; }
  .edit-row button { font-size: 12px; padding: 4px 10px; border-radius: 4px; cursor: pointer; }
  .edit-row .primary { background: var(--p-2d5578); border: 1px solid var(--p-2d5578); color: var(--p-cceeff); }
  .edit-row .ghost { background: transparent; border: 1px solid var(--p-333333); color: var(--p-999999); }
  .err { font-size: 11px; color: var(--p-e07a7a); }
</style>
