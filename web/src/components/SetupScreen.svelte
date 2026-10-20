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
  // First-run setup. Shown over the whole UI until Ollama answers and both
  // models are installed (or the user skips for this session). Replaces
  // scripts/install.sh for desktop users: install Ollama, pick the classifier
  // model, download what is missing, say where the data lives.
  import * as api from '../lib/api';

  type Props = { status: api.SetupStatus; onDone: () => void };
  let { status: initial, onDone }: Props = $props();

  // The prop is a snapshot taken at mount; this component owns the live copy.
  // svelte-ignore state_referenced_locally
  let status = $state<api.SetupStatus>(initial);
  // svelte-ignore state_referenced_locally
  let chosen = $state(initial.models.classifier || initial.models.recommended);
  let checking = $state(false);
  let err = $state('');
  // model name -> progress while a pull runs
  let pulls = $state<Record<string, { pct: number; label: string }>>({});
  let finishing = $state(false);

  const SIZES: Record<string, string> = {
    'qwen2.5:7b': 'about 4.7 GB',
    'qwen2.5:14b': 'about 9 GB',
    'nomic-embed-text': 'about 270 MB',
  };

  // Ollama lists "name:tag"; a bare name matches its ":latest" tag.
  const installed = (m: string) =>
    status.models.installed.some((i) => i === m || i === m + ':latest' || (!m.includes(':') && i.split(':')[0] === m));
  const choices = $derived.by(() => {
    const set = new Set<string>([status.models.recommended, status.models.default, ...status.models.installed.filter((m) => m !== status.models.embed)]);
    return [...set];
  });
  const ready = $derived(status.ollama.reachable && installed(chosen) && installed(status.models.embed));
  const isMac = $derived(status.platform === 'darwin');

  async function refresh() {
    try { status = await api.getSetupStatus(); } catch (e) { err = String(e); }
  }

  async function checkOllama() {
    checking = true; err = '';
    try {
      status = status.ollama.can_start ? await api.startOllama() : await api.getSetupStatus();
      if (!status.ollama.reachable && status.ollama.error) err = status.ollama.error;
    } catch (e) { err = String(e); }
    finally { checking = false; }
  }

  async function pull(model: string) {
    err = '';
    pulls[model] = { pct: 0, label: 'starting…' };
    try {
      await api.pullModel(model, (p) => {
        const pct = p.total && p.completed ? Math.round((p.completed / p.total) * 100) : pulls[model]?.pct ?? 0;
        pulls[model] = { pct, label: p.total ? `${p.status} ${pct}%` : p.status };
      });
      await refresh();
    } catch (e) {
      err = `${model}: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      delete pulls[model];
    }
  }

  async function finish() {
    finishing = true; err = '';
    try { await api.completeSetup(chosen); onDone(); }
    catch (e) { err = String(e); }
    finally { finishing = false; }
  }

  const gb = (n: number) => (n ? `${Math.round(n)} GB` : 'unknown');
</script>

<div class="setup" role="dialog" aria-modal="true" aria-labelledby="setup-title">
  <div class="card">
    <h1 id="setup-title">Welcome to CodeDistill</h1>
    <p class="lead">
      CodeDistill runs entirely on this machine. It needs <strong>Ollama</strong>, a local model server, and two
      models: one that reads and classifies your notes, and a small one that builds the search index.
      Nothing leaves your computer.
    </p>

    <!-- Step 1: Ollama -->
    <section class:done={status.ollama.reachable}>
      <h2><span class="num">1</span> Ollama</h2>
      {#if status.ollama.reachable}
        <p class="ok">Running at {status.ollama.endpoint}.</p>
      {:else if status.ollama.binary}
        <p>Ollama is installed at <code>{status.ollama.binary}</code> but is not running.</p>
        <div class="row">
          <button class="btn primary" onclick={checkOllama} disabled={checking}>{checking ? 'Starting…' : 'Start Ollama'}</button>
        </div>
      {:else}
        <p>Ollama is not installed.</p>
        <ol>
          <li>
            Download it from <a href="https://ollama.com/download" target="_blank" rel="noopener">ollama.com/download</a>
            {#if isMac}(the macOS app; drag it to Applications and open it once){/if}.
          </li>
          <li>Come back here and click <em>Check again</em>.</li>
        </ol>
        <div class="row">
          <a class="btn primary" href="https://ollama.com/download" target="_blank" rel="noopener">Get Ollama</a>
          <button class="btn" onclick={checkOllama} disabled={checking}>{checking ? 'Checking…' : 'Check again'}</button>
        </div>
      {/if}
      {#if !status.ollama.reachable && status.ollama.error && !err}
        <p class="muted small">{status.ollama.error}</p>
      {/if}
    </section>

    <!-- Step 2: models -->
    <section class:done={installed(chosen) && installed(status.models.embed)} class:off={!status.ollama.reachable}>
      <h2><span class="num">2</span> Models</h2>
      <p class="muted">
        This machine has {gb(status.memory_gb)} of memory.
        {#if status.models.recommended !== status.models.default}
          That is enough for the larger <code>{status.models.recommended}</code>, which reads more carefully; the
          smaller <code>{status.models.default}</code> is faster and fine for most notes.
        {:else}
          <code>{status.models.default}</code> is the right fit; the larger 14B model wants 16 GB or more.
        {/if}
      </p>
      <div class="models">
        <div class="model-head">Reads and classifies your notes</div>
        {#each choices as m (m)}
          <label class="model" class:selected={chosen === m}>
            <input type="radio" name="classifier" value={m} bind:group={chosen} disabled={!status.ollama.reachable} />
            <span class="name">{m}</span>
            {#if m === status.models.recommended}<span class="tag">recommended</span>{/if}
            <span class="state">
              {#if installed(m)}
                <span class="ok">installed</span>
              {:else if pulls[m]}
                <progress max="100" value={pulls[m].pct}></progress> <span class="small">{pulls[m].label}</span>
              {:else}
                <button class="btn small" onclick={() => pull(m)} disabled={!status.ollama.reachable}>Download{SIZES[m] ? ` (${SIZES[m]})` : ''}</button>
              {/if}
            </span>
          </label>
        {/each}
        <div class="model-head">Builds the search index</div>
        <div class="model fixed">
          <span class="name">{status.models.embed}</span>
          <span class="state">
            {#if installed(status.models.embed)}
              <span class="ok">installed</span>
            {:else if pulls[status.models.embed]}
              <progress max="100" value={pulls[status.models.embed].pct}></progress> <span class="small">{pulls[status.models.embed].label}</span>
            {:else}
              <button class="btn small" onclick={() => pull(status.models.embed)} disabled={!status.ollama.reachable}>Download ({SIZES[status.models.embed]})</button>
            {/if}
          </span>
        </div>
      </div>
      <p class="muted small">Downloads come from Ollama's library and are kept by Ollama; CodeDistill never downloads a model twice. You can change the model later per project under Settings → Models.</p>
    </section>

    <!-- Step 3: data -->
    <section class="done">
      <h2><span class="num">3</span> Your data</h2>
      <p class="muted">Notes, items and the search index are stored in <code>{status.data_dir}</code>. Back that folder up and you have everything.</p>
    </section>

    {#if err}<p class="error" role="alert">{err}</p>{/if}

    <div class="actions">
      <button class="btn ghost" onclick={onDone}>Skip for now</button>
      <button class="btn primary" onclick={finish} disabled={!ready || finishing}>
        {ready ? 'Start using CodeDistill' : 'Finish the steps above first'}
      </button>
    </div>
  </div>
</div>

<style>
  .setup {
    position: fixed; inset: 0; z-index: 1000;
    display: flex; align-items: flex-start; justify-content: center;
    overflow: auto; padding: 48px 16px;
    background: var(--p-0d0d0d);
  }
  .card {
    width: 100%; max-width: 680px;
    background: var(--p-1a1a1a); border: 1px solid var(--p-333333); border-radius: 12px;
    padding: 28px 32px 24px;
    color: var(--p-ffffff);
  }
  h1 { margin: 0 0 8px; font-size: 1.6rem; }
  .lead { margin: 0 0 20px; line-height: 1.5; color: var(--p-cccccc); }
  section { border-top: 1px solid var(--p-333333); padding: 16px 0; }
  section.off { opacity: 0.55; }
  h2 { font-size: 1.05rem; margin: 0 0 8px; display: flex; align-items: center; gap: 10px; }
  .num {
    display: inline-flex; width: 24px; height: 24px; border-radius: 50%; align-items: center; justify-content: center;
    font-size: 0.8rem; background: var(--p-333333); color: var(--p-ffffff);
  }
  section.done .num { background: var(--p-2d6840); color: var(--p-d8fdd0); }
  p { margin: 6px 0; line-height: 1.45; }
  .muted { color: var(--p-aaaaaa); }
  .small { font-size: 0.85rem; }
  .ok { color: var(--p-d8fdd0); }
  .error { color: var(--p-ff8888); margin-top: 12px; white-space: pre-wrap; }
  code { background: var(--p-333333); padding: 1px 5px; border-radius: 4px; font-size: 0.9em; }
  a { color: var(--p-66ccff); }
  ol { margin: 6px 0 10px; padding-left: 22px; line-height: 1.5; }
  .row { display: flex; gap: 10px; margin-top: 10px; align-items: center; }
  .models { margin: 10px 0 6px; border: 1px solid var(--p-333333); border-radius: 8px; overflow: hidden; }
  .model-head { padding: 6px 12px; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--p-888888); background: var(--p-10202a); }
  .model { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-top: 1px solid var(--p-333333); cursor: pointer; }
  .model.fixed { cursor: default; }
  .model.selected { background: var(--p-1e3a52); }
  .model .name { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .tag { font-size: 0.75rem; padding: 1px 7px; border-radius: 10px; background: var(--p-2d5578); color: var(--p-ffffff); }
  .state { margin-left: auto; display: flex; align-items: center; gap: 8px; }
  progress { width: 140px; height: 8px; }
  .btn.small { padding: 3px 10px; font-size: 0.85rem; }
  a.btn { text-decoration: none; display: inline-flex; align-items: center; }
  .actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--p-333333); }
</style>
