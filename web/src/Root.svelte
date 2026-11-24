<!--
  Before the app: who is this, and may they use it?

  Asks the server once (GET /api/v1/me) and shows one of:
    - the sign-in screen, on a multi-user server with nobody signed in;
    - the access-token screen, on a server reached from another machine
      without sign-in (the token is asked for once, then kept in a cookie);
    - the administration view, for an account that administers the server
      but does not use the product (it takes no seat, and opens no project);
    - the app.
  The server enforces all of this; this only shows the right door.
  (docs/design/workflows-and-roles.md)
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import * as api from './lib/api';
  import { session } from './lib/session.svelte';
  import App from './App.svelte';
  import AdminOnly from './AdminOnly.svelte';

  let me = $state<api.Me | null>(null);
  let error = $state('');
  let token = $state('');
  let busy = $state(false);

  async function load() {
    try {
      me = await api.getMe();
      session.me = me;
      error = '';
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
  onMount(load);

  async function sendToken(e: Event) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.tokenSession(token.trim());
      location.reload();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  const adminOnly = $derived(!!me && me.multi_user && !me.needs && !(me.permissions ?? []).includes('use_product'));
</script>

{#if !me && !error}
  <div class="gate"><p class="muted">Loading…</p></div>
{:else if !me}
  <div class="gate">
    <div class="card">
      <h1>CodeDistill</h1>
      <p class="err">Could not reach the server: {error}</p>
      <button onclick={load}>Try again</button>
    </div>
  </div>
{:else if me.needs === 'sign_in'}
  <div class="gate">
    <div class="card">
      <h1>CodeDistill</h1>
      <p>Sign in to use this server.</p>
      <a class="primary" href={me.login_url ?? '/api/v1/auth/login'}>Sign in</a>
    </div>
  </div>
{:else if me.needs === 'refused' || me.needs === 'proxy'}
  <!-- A proxy signs people in: either it vouched for someone this server
       will not let in (deactivated, no seat, not allowed), or this request
       did not come through it. Nothing to fill in here. -->
  <div class="gate">
    <div class="card">
      <h1>CodeDistill</h1>
      <p>{me.message ?? 'You cannot use this server.'}</p>
      {#if me.needs === 'refused' && me.logout_url}<a class="primary" href={me.logout_url}>Sign out</a>{/if}
    </div>
  </div>
{:else if me.needs === 'token'}
  <div class="gate">
    <form class="card" onsubmit={sendToken}>
      <h1>CodeDistill</h1>
      <p>This server is reached over the network. Enter its access token, once, to use it from this browser.</p>
      <p class="muted">The person who runs the server has it: the file <code>api-token</code> in CodeDistill's configuration folder on the server (<code>~/.config/codedistill/api-token</code> on Linux), or Settings › Server access when CodeDistill is opened on the server itself.</p>
      <input type="password" bind:value={token} placeholder="Access token" autocomplete="off" required />
      {#if error}<p class="err">{error}</p>{/if}
      <button class="primary" type="submit" disabled={busy || !token.trim()}>{busy ? 'Checking…' : 'Continue'}</button>
    </form>
  </div>
{:else if adminOnly}
  <AdminOnly {me} />
{:else}
  <App />
{/if}

<style>
  .gate {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--p-0d0d0d);
    color: var(--text);
    padding: 16px;
  }
  .card {
    width: min(26rem, 100%);
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
    padding: 1.6rem;
    border: 1px solid var(--p-2a2a2a);
    border-radius: 8px;
    background: var(--p-141414);
  }
  h1 { margin: 0; font-size: 1.2rem; color: var(--p-99ccff); }
  p { margin: 0; font-size: 0.9rem; }
  .muted { color: var(--p-888888); font-size: 0.8rem; }
  .err { color: var(--p-e07a7a); font-size: 0.85rem; }
  input {
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--text);
    border-radius: 4px; padding: 0.45rem 0.6rem; font: inherit;
  }
  .primary, button {
    align-self: flex-start;
    background: var(--p-1e3a52); color: var(--p-cfe6ff); border: 1px solid var(--p-2d5578);
    border-radius: 4px; padding: 0.4rem 1rem; font: inherit; text-decoration: none; cursor: pointer;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  code { font-size: 0.78rem; }
</style>
