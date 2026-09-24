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
  import * as api from '../lib/api';
  import type { ModelProvider, ProbeResult, ProbeOutcome } from '../lib/api';

  // Models tab: PROVIDERS ONLY — the connections available to this install.
  //
  // Which model runs which worker lives with the WORKFLOW that needs it, not
  // here: "decompose runs on Claude" is a fact about decomposing, and putting
  // it in a connections list made the two read as one setting.
  //
  // The design principle throughout is that a CONFIGURED provider and a WORKING
  // provider are different things, and the UI must never let the first look
  // like the second. Ollama answers /api/tags in a millisecond while wedged,
  // and truncates oversized prompts without erroring — both shipped in this
  // product for a year. So a provider shows "untested" until someone proves it,
  // and the proof reports what the server actually read.

  let providers = $state<ModelProvider[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Probe results live here rather than on the provider: they are observations
  // from this session, not stored state, and conflating them would let a stale
  // "verified" badge outlive the thing it described.
  let probes = $state<Record<string, ProbeResult>>({});
  let testing = $state<Record<string, boolean>>({});

  let editing = $state<string | null>(null);
  let draft = $state<Draft>(blankDraft());

  // Vendor first. Picking a company is the choice a user actually makes;
  // protocol and endpoint are consequences of it, and asking someone to type
  // "https://api.openai.com/v1" is asking them to know our implementation.
  let vendors = $state<api.Vendor[]>([]);
  let vendorId = $state('ollama');
  let discovered = $state<api.ModelSummary[]>([]);
  let discovering = $state(false);
  let discoverError = $state('');
  let describedCtx = $state(0);

  const vendor = $derived(vendors.find((v) => v.id === vendorId));

  type Draft = {
    name: string; protocol: string; endpoint: string;
    model: string; api_key: string; context_tokens: number; is_local: boolean;
  };

  /** AWS's three fields, packed into draft.api_key on save. Held apart here
   *  only so the form can have three inputs; the wire format is one secret. */
  let aws = $state({ access_key_id: '', secret_access_key: '', session_token: '' });

  /** Packs them, or returns '' meaning "keep whatever is stored" — the same
   *  convention every other provider's blank key field uses. */
  function packedAWS(): string {
    const a = aws.access_key_id.trim(), s = aws.secret_access_key.trim();
    if (!a && !s && !aws.session_token.trim()) return '';
    const out: Record<string, string> = { access_key_id: a, secret_access_key: s };
    if (aws.session_token.trim()) out.session_token = aws.session_token.trim();
    return JSON.stringify(out);
  }

  /** Mirrors domain.EndpointReach. The server clamps what gets STORED
   *  regardless; this decides what the dialog says and whether the question is
   *  worth asking at all. */
  const reach = $derived.by(() => {
    if (draft.protocol === 'anthropic' || draft.protocol === 'gemini') return 'internet';
    const host = (draft.endpoint ?? '')
      .replace(/^[a-z]+:\/\//i, '').split(/[/?#]/)[0]
      .replace(/^.*@/, '').replace(/:\d+$/, '')
      .replace(/^\[|\]$/g, '').toLowerCase().trim();
    if (!host) return 'internet';
    if (host === 'localhost' || /\.localhost$/.test(host)) return 'machine';
    if (/^127\./.test(host) || host === '::1' || host === '0.0.0.0') return 'machine';
    if (/^10\./.test(host) || /^192\.168\./.test(host)) return 'network';
    if (/^172\.(1[6-9]|2\d|3[01])\./.test(host) || /^169\.254\./.test(host)) return 'network';
    if (/\.(local|internal)$/.test(host)) return 'network';
    return 'internet';
  });

  /** Choosing a vendor fills in what follows from it. The endpoint stays
   *  editable: self-hosted and proxied deployments are normal, and a fixed
   *  endpoint would exclude them. */
  function pickVendor(id: string) {
    vendorId = id;
    discovered = [];
    discoverError = '';
    describedCtx = 0;
    const v = vendors.find((x) => x.id === id);
    if (!v || !v.supported) return;
    draft.protocol = v.protocol ?? 'openai';
    draft.endpoint = v.default_endpoint ?? '';
    draft.model = '';
    // A local vendor means nothing leaves the machine — recorded rather than
    // inferred from the URL, because an SSH tunnel looks local and is not.
    draft.is_local = id === 'ollama' || id === 'lmstudio' || id === 'vllm';
    if (!draft.name) draft.name = v.name;
  }

  /** Ask the vendor what it offers. A model string typed by hand fails at call
   *  time with a 404; a list removes that whole class of mistake. */
  async function discover() {
    discovering = true;
    discoverError = '';
    try {
      const r = await api.discoverModels({
        protocol: draft.protocol, endpoint: draft.endpoint,
        api_key: draft.protocol === 'bedrock' ? packedAWS() : draft.api_key, model: draft.model,
        provider_id: editing && editing !== 'new' ? editing : undefined,
      });
      discovered = r.models ?? [];
      if (r.error) discoverError = r.error;
      // A window the provider STATED, which is worth far more than one typed.
      const ctx = r.described?.context_length ?? 0;
      if (ctx > 0) {
        describedCtx = ctx;
        draft.context_tokens = ctx;
      }
    } catch (e) {
      discoverError = e instanceof Error ? e.message : String(e);
    } finally {
      discovering = false;
    }
  }

  /** Choosing a model re-asks, because context length is per-model and only
   *  knowable once a model is named. */
  async function pickModel(id: string) {
    draft.model = id;
    describedCtx = 0;
    const m = discovered.find((x) => x.id === id);
    if (m?.context_length) {
      describedCtx = m.context_length;
      draft.context_tokens = m.context_length;
      return;
    }
    await discover();
  }

  function blankDraft(): Draft {
    return {
      name: '', protocol: 'ollama', endpoint: 'http://localhost:11434',
      model: '', api_key: '', context_tokens: 16384, is_local: true,
    };
  }

  async function load() {
    loading = true;
    error = '';
    try {
      providers = (await api.listModelProviders()) ?? [];
      if (vendors.length === 0) vendors = (await api.listVendors()) ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function startAdd() {
    // Cleared on every open: these fields are never populated from the server
    // (the secret is not readable), so anything left here belongs to whichever
    // provider was being edited a moment ago.
    aws = { access_key_id: '', secret_access_key: '', session_token: '' };
    editing = 'new';
    draft = blankDraft();
    discovered = [];
    discoverError = '';
    describedCtx = 0;
    vendorId = 'ollama';
    pickVendor('ollama');
    draft.name = '';
  }

  function startEdit(p: ModelProvider) {
    aws = { access_key_id: '', secret_access_key: '', session_token: '' };
    editing = p.id;
    // api_key is deliberately blank: the UI is never given the stored key, and
    // sending an empty one back means "leave it alone".
    draft = {
      name: p.name, protocol: p.protocol, endpoint: p.endpoint, model: p.model,
      api_key: '', context_tokens: p.context_tokens, is_local: p.is_local,
    };
  }

  async function save() {
    try {
      if (editing === 'new') {
        await api.createModelProvider({ ...draft, enabled: true });
      } else if (editing) {
        await api.updateModelProvider(editing, { ...draft, enabled: true });
      }
      editing = null;
      await load();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function remove(p: ModelProvider) {
    if (!confirm(`Delete "${p.name}"? Any worker using it falls back to the app default.`)) return;
    try {
      await api.deleteModelProvider(p.id);
      delete probes[p.id];
      await load();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function test(p: ModelProvider) {
    testing = { ...testing, [p.id]: true };
    try {
      probes = { ...probes, [p.id]: await api.testModelProvider(p.id) };
    } catch (e) {
      probes = {
        ...probes,
        [p.id]: {
          outcome: 'unreachable',
          detail: e instanceof Error ? e.message : String(e),
          reachable: false, can_generate: false, latency_ms: 0,
        },
      };
    } finally {
      testing = { ...testing, [p.id]: false };
    }
  }

  const OUTCOME_LABEL: Record<ProbeOutcome, string> = {
    unreachable: 'Unreachable',
    model_unusable: 'Model unusable',
    working: 'Working',
    verified: 'Verified',
    context_short: 'Truncates long prompts',
  };

  function outcomeClass(o: ProbeOutcome): string {
    switch (o) {
      case 'verified': return 'ok';
      case 'working': return 'partial';
      case 'context_short': return 'warn';
      default: return 'bad';
    }
  }

</script>

<div class="models">
  <header>
    <div>
      <h2>Models</h2>
      <p class="sub">
        A connection to a model. Which workflow uses which one is set on the
        workflow itself.
      </p>
    </div>
    <button class="primary" onclick={startAdd}>Add provider</button>
  </header>

  {#if error}
    <p class="err" role="alert">{error}</p>
  {/if}

  <!-- ── Editor ──────────────────────────────────────────────────────── -->
  {#if editing}
    <div class="editor">
      <h3>{editing === 'new' ? 'Add a provider' : 'Edit provider'}</h3>

      <!-- Step 1: WHO. Everything else follows from this. -->
      <label class="block">
        Who are you connecting to?
        <select value={vendorId} onchange={(e) => pickVendor(e.currentTarget.value)}>
          {#each vendors as v (v.id)}
            <option value={v.id} disabled={!v.supported}>
              {v.name}{v.supported ? '' : ' — not supported yet'}
            </option>
          {/each}
        </select>
      </label>

      {#if vendor && !vendor.supported}
        <!-- Listed with a way forward rather than hidden: someone looking for
             Anthropic should find out here, not conclude we do no cloud models. -->
        <p class="warnbox">{vendor.note}</p>
      {:else}
        {#if vendor?.note}
          <p class="vendornote">{vendor.note}</p>
        {/if}

        <div class="grid">
          <label>Name<input bind:value={draft.name} placeholder="what you'll call it" /></label>
          <label class="wide">
            Endpoint
            <input bind:value={draft.endpoint} placeholder="https://…" />
            <span class="hint">Pre-filled for {vendor?.name ?? 'this vendor'}. Change it for a self-hosted or proxied deployment.</span>
          </label>

          {#if draft.protocol === 'bedrock'}
            <!-- AWS is three fields where every other vendor is one. They are
                 packed into the SAME encrypted secret rather than given their
                 own columns: the secret is the thing that is encrypted at rest
                 and never returned, and half a credential in a plaintext column
                 would sit outside that guarantee. -->
            <label class="wide">
              Access key ID
              <input
                bind:value={aws.access_key_id}
                placeholder={editing === 'new' ? 'AKIA…' : 'leave blank to keep the stored credentials'}
                autocomplete="off"
              />
            </label>
            <label class="wide">
              Secret access key
              <input type="password" bind:value={aws.secret_access_key} autocomplete="off" />
            </label>
            <label class="wide">
              Session token <span class="opt">only for temporary credentials</span>
              <input type="password" bind:value={aws.session_token} autocomplete="off" />
              <span class="hint">
                Stored encrypted, never sent back to this page. The region comes from the
                endpoint above; the credentials need <code>bedrock:InvokeModel</code> and
                <code>bedrock:ListFoundationModels</code>, and the model must be switched on
                under Model access in the Bedrock console.
              </span>
            </label>
          {:else if vendor?.needs_key}
            <label class="wide">
              API key
              <input
                type="password"
                bind:value={draft.api_key}
                placeholder={editing === 'new' ? 'required' : 'leave blank to keep the stored key'}
                autocomplete="off"
              />
              <span class="hint">Stored encrypted. Never sent back to this page.</span>
            </label>
          {/if}
        </div>

        <!-- Step 2: ASK THE VENDOR what it has, rather than making them type it. -->
        <div class="discover">
          <button onclick={discover} disabled={discovering || !draft.endpoint}>
            {discovering ? 'Asking…' : 'Show available models'}
          </button>
          {#if discoverError}
            <span class="probe bad">{discoverError}</span>
          {/if}
        </div>

        {#if discovered.length > 0}
          <label class="block">
            Model
            <select value={draft.model} onchange={(e) => pickModel(e.currentTarget.value)}>
              <option value="">— choose —</option>
              {#each discovered as m (m.id)}
                <option value={m.id}>
                  {m.id}{m.parameter_size ? ` (${m.parameter_size})` : ''}
                </option>
              {/each}
            </select>
          </label>
        {:else}
          <label class="block">
            Model
            <input bind:value={draft.model} placeholder="type it, or ask the vendor above" />
          </label>
        {/if}

        <div class="grid">
          <label>
            Context window
            <input type="number" bind:value={draft.context_tokens} min="512" step="1024" />
            {#if describedCtx > 0}
              <span class="hint ok">{vendor?.name} reports {describedCtx.toLocaleString()} tokens for this model.</span>
            {:else if vendor && !vendor.reports_context}
              <!-- Never let a typed number look like a stated fact. -->
              <span class="hint">{vendor.name} does not publish context windows, so this is your figure. It rejects oversized prompts rather than truncating, so a wrong number fails loudly.</span>
            {/if}
          </label>
          <!-- Locality is a FACT where it can be established and a question only
               where it cannot. A checkbox here let someone tick "nothing leaves
               this machine" on Gemini and be believed. Three answers, not two:
               a box on your own network is neither this machine nor the
               internet, and calling it either is a lie in one direction. -->
          {#if reach === 'internet'}
            <p class="locality remote">
              Requests go to {vendor?.name ?? 'this endpoint'} over the internet.
              Whatever you send it — including whole documents — leaves your network.
            </p>
          {:else if reach === 'network'}
            <p class="locality net">
              This address is on your own network: documents stay inside it, but do
              leave this machine. The usual setup when one box has the GPU in it.
            </p>
          {:else}
            <label class="check">
              <input type="checkbox" bind:checked={draft.is_local} />
              Runs on this machine — nothing leaves it
            </label>
            <p class="locality hint">
              Not provable from the address — a tunnel to another machine looks
              exactly like a local one. Untick it if this endpoint forwards on.
            </p>
          {/if}
        </div>
      {/if}

      <div class="editactions">
        <button
          class="primary"
          onclick={save}
          disabled={!vendor?.supported || !draft.name || !draft.endpoint || !draft.model}
        >Save</button>
        <button onclick={() => (editing = null)}>Cancel</button>
      </div>
    </div>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <!-- ── Providers ─────────────────────────────────────────────────── -->
    <section>
      <h3>Providers</h3>
      {#if providers.length === 0}
        <p class="muted">
          No providers configured. CodeDistill falls back to the model it was started with.
        </p>
      {/if}

      {#each providers as p (p.id)}
        {@const probe = probes[p.id]}
        <div class="card" class:disabled={!p.enabled}>
          <div class="row">
            <div class="ident">
              <span class="name">{p.name}</span>
              <span class="tag">{p.protocol}</span>
              {#if p.is_local}
                <span class="tag local">on this machine</span>
              {:else}
                <span class="tag remote">sends data off this machine</span>
              {/if}
            </div>
            <div class="actions">
              <button onclick={() => test(p)} disabled={testing[p.id]}>
                {testing[p.id] ? 'Testing…' : 'Test'}
              </button>
              <button onclick={() => startEdit(p)}>Edit</button>
              <button class="danger" onclick={() => remove(p)}>Delete</button>
            </div>
          </div>

          <div class="meta">
            <code>{p.endpoint}</code> · <code>{p.model}</code> ·
            {p.context_tokens.toLocaleString()} token window
            {#if p.has_api_key}· key set{/if}
          </div>

          {#if testing[p.id]}
            <p class="probe partial">
              Generating, and checking how much of a {p.context_tokens.toLocaleString()}-token
              prompt is really read. This takes a moment.
            </p>
          {:else if probe}
            <p class="probe {outcomeClass(probe.outcome)}">
              <strong>{OUTCOME_LABEL[probe.outcome]}</strong>
              — {probe.detail}
              {#if probe.latency_ms > 0}<span class="muted"> ({(probe.latency_ms / 1000).toFixed(1)}s)</span>{/if}
            </p>
          {:else}
            <!-- Never imply health that has not been demonstrated: a provider
                 can answer metadata instantly while generation is dead. -->
            <p class="probe untested">Not tested yet — configured is not the same as working.</p>
          {/if}
        </div>
      {/each}
    </section>

  {/if}

</div>

<style>
  .models { display: flex; flex-direction: column; gap: 1.5rem; padding: 1rem 0; }
  header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
  h2 { margin: 0; font-size: 1.05rem; }
  h3 { margin: 0 0 0.6rem; font-size: 0.8rem; text-transform: uppercase;
       letter-spacing: 0.08em; color: var(--text-dim); }
  .sub { margin: 0.25rem 0 0; font-size: 0.85rem; color: var(--text-dim); max-width: 46rem; }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
  .err { color: var(--danger, #f88); font-size: 0.85rem; }

  .card { border: 1px solid var(--border); border-radius: 8px; padding: 0.75rem 0.9rem;
          margin-bottom: 0.6rem; background: var(--bg-raised, transparent); }
  .card.disabled { opacity: 0.55; }
  .row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
  .ident { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .name { font-weight: 600; }
  .tag { font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.05em;
         border: 1px solid var(--border); border-radius: 999px; padding: 0.05rem 0.45rem;
         color: var(--text-dim); }
  .tag.remote { border-color: #b9821f; color: #d69b30; }
  .tag.local { border-color: var(--border); }
  .actions { display: flex; gap: 0.35rem; }
  .meta { margin-top: 0.4rem; font-size: 0.78rem; color: var(--text-dim); }
  .meta code { font-size: 0.78rem; }

  .probe { margin: 0.5rem 0 0; font-size: 0.82rem; }
  .probe.ok { color: #4caf7d; }
  .probe.partial { color: var(--text-dim); }
  .probe.warn { color: #d69b30; }
  .probe.bad { color: var(--danger, #f88); }
  .probe.untested { color: var(--text-dim); font-style: italic; }



  .editor { border: 1px solid var(--border); border-radius: 8px; padding: 0.9rem; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.7rem; }
  .grid label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.8rem; }
  .grid .wide { grid-column: 1 / -1; }
  .grid .check { flex-direction: row; align-items: center; gap: 0.45rem; grid-column: 1 / -1; }
  .hint { font-size: 0.72rem; color: var(--text-dim); }
  .hint.ok { color: #4caf7d; }
  .block { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.8rem;
           margin-bottom: 0.7rem; }
  .vendornote { margin: 0 0 0.7rem; font-size: 0.78rem; color: var(--text-dim); }
  .discover { display: flex; align-items: center; gap: 0.6rem; margin: 0.2rem 0 0.7rem; }
  .editactions { display: flex; gap: 0.4rem; margin-top: 0.8rem; }

  @media (max-width: 720px) {
      .grid { grid-template-columns: 1fr; }
  }
</style>
