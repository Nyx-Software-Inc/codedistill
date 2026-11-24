<!--
  The server, for someone who administers it without using the product: the
  person who installed it, holding Admin (or Owner) alone. Such an account
  takes no license seat and opens no project, so this is all it sees: the
  people on the server, their roles, and the installation's settings.
  (docs/design/workflows-and-roles.md, "Seats")
-->
<script lang="ts">
  import * as api from './lib/api';
  import { LOCAL_USER } from './lib/userSettings';
  import AdminConsole from './components/AdminConsole.svelte';
  import Settings from './components/Settings.svelte';
  import DialogHost from './components/DialogHost.svelte';
  import { can } from './lib/session.svelte';

  type Props = { me: api.Me };
  let { me }: Props = $props();

  let tab = $state<'people' | 'settings'>(can('manage_members') ? 'people' : 'settings');

  async function signOut() {
    try { await api.logout(); } catch { /* signed out either way */ }
    location.reload();
  }
</script>

<main class="admin-only">
  <header>
    <span class="brand">CodeDistill</span>
    <span class="what">Server administration</span>
    <span class="spacer"></span>
    <span class="who" title={me.email ?? ''}>{me.display_name}</span>
    <button class="out" onclick={signOut}>Sign out</button>
  </header>
  <p class="note">
    Your account administers this server and does not use the product, so it takes no license seat.
    To work in projects too, an owner can give you the Member role.
  </p>
  <nav class="tabs">
    {#if can('manage_members')}
      <button class:active={tab === 'people'} onclick={() => (tab = 'people')}>People and roles</button>
    {/if}
    <button class:active={tab === 'settings'} onclick={() => (tab = 'settings')}>Settings</button>
  </nav>
  <section class="body">
    {#if tab === 'people'}
      <AdminConsole />
    {:else}
      <Settings userId={LOCAL_USER} initial="models" />
    {/if}
  </section>
</main>
<DialogHost />

<style>
  .admin-only { height: 100%; display: flex; flex-direction: column; background: var(--p-0d0d0d); color: var(--text); overflow: auto; }
  header { display: flex; align-items: center; gap: 10px; height: 38px; padding: 0 12px; border-bottom: 1px solid var(--p-2a2a2a); }
  .brand { color: var(--p-99ccff); font-weight: 600; }
  .what { color: var(--p-888888); font-size: 0.85rem; }
  .spacer { flex: 1; }
  .who { font-size: 0.85rem; }
  .out { background: none; border: 1px solid var(--p-333333); color: var(--text); border-radius: 4px; padding: 2px 10px; cursor: pointer; font: inherit; font-size: 0.8rem; }
  .note { margin: 12px 16px 0; font-size: 0.8rem; color: var(--p-888888); max-width: 52rem; }
  .tabs { display: flex; gap: 4px; padding: 12px 16px 0; }
  .tabs button { background: none; border: 1px solid var(--p-2a2a2a); color: var(--p-aaaaaa); border-radius: 4px; padding: 4px 12px; cursor: pointer; font: inherit; font-size: 0.85rem; }
  .tabs button.active { color: var(--text); border-color: var(--p-555555); background: var(--p-1a1a1a); }
  .body { padding: 16px; }
</style>
