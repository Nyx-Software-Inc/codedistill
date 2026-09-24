<!--
  Workflows: what each one is, and which model runs each of its workers.

  This lived in the job monitor, which was wrong twice over. Configuration is
  not monitoring — you set a model once and watch runs every day — and putting
  it on the panel meant the settings window, where someone goes to configure
  things, had no idea workflows existed.
-->
<script lang="ts">
  import * as api from '../lib/api';
  import type { Workflow, ModelProvider } from '../lib/api';

  let workflows = $state<Workflow[]>([]);
  let providers = $state<ModelProvider[]>([]);
  let loaded = $state(false);
  let error = $state('');
  /** Confirmation that a change landed. Without it, a select that saves
   *  silently is indistinguishable from one that does nothing — which is
   *  exactly what this panel was accused of. */
  let saved = $state('');

  async function load() {
    try {
      const [wfs, ps] = await Promise.all([api.listWorkflows(), api.listModelProviders()]);
      workflows = wfs ?? [];
      providers = (ps ?? []).filter((p) => p.enabled);
      error = '';
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }
  $effect(() => { void load(); });

  async function choose(wf: Workflow, ordinal: number, providerId: string) {
    try {
      await api.setStepProvider(wf.id, ordinal, providerId);
      await load();
      const p = providers.find((x) => x.id === providerId);
      saved = p ? `${wf.name} now runs on ${p.name}.` : `${wf.name} is back to the default model.`;
      setTimeout(() => (saved = ''), 4000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<section class="wfsettings">
  <h3>Workflows</h3>
  <p class="intro">
    A workflow is a kind of job and it runs workers. Each worker can be pointed at
    a different model — a strong one to read a specification, something cheap to
    summarise.
  </p>

  {#if error}<p class="err" role="alert">{error}</p>{/if}
  {#if saved}<p class="ok">{saved}</p>{/if}

  {#if !loaded}
    <p class="muted">Loading…</p>
  {:else if workflows.length === 0}
    <p class="muted">No workflows are defined.</p>
  {:else}
    {#each workflows as w (w.id)}
      <article class="wf">
        <header>
          <span class="name">{w.name}</span>
          {#if w.builtin}<span class="tag">built in</span>{/if}
          {#if !w.resumable}<span class="tag warn">cannot be resumed</span>{/if}
        </header>
        {#if w.description}<p class="desc">{w.description}</p>{/if}

        {#each w.steps as st (st.ordinal)}
          <div class="step">
            <div class="who">
              <span class="worker">{st.worker_type}</span>
              {#if st.label}<span class="what">{st.label}</span>{/if}
            </div>
            <div class="pick">
              <select
                value={st.provider_id ?? ''}
                onchange={(e) => choose(w, st.ordinal, e.currentTarget.value)}
              >
                <option value="">— whatever the app was started with —</option>
                {#each providers as p (p.id)}
                  <option value={p.id}>{p.name}{p.is_local ? '' : ' (off this machine)'}</option>
                {/each}
              </select>
              {#if st.needs_context && st.context_tokens && st.context_tokens < st.needs_context}
                <!-- Caught here rather than as a truncated answer forty minutes
                     into a run, which is the failure this product has actually
                     shipped and not noticed. -->
                <p class="short">
                  This model reads {st.context_tokens.toLocaleString()} tokens; the step needs
                  {st.needs_context.toLocaleString()}. It will silently read less than it is given.
                </p>
              {:else if st.needs_context}
                <p class="need">needs to read {st.needs_context.toLocaleString()} tokens</p>
              {/if}
            </div>
          </div>
        {/each}
      </article>
    {/each}
  {/if}
</section>

<style>
  .wfsettings { display: flex; flex-direction: column; gap: 0.75rem; }
  h3 { margin: 0; font-size: 1rem; }
  .intro { margin: 0; font-size: 0.85rem; color: var(--text-dim); max-width: 46rem; }
  .wf { border: 1px solid var(--border); border-radius: 8px; padding: 0.7rem 0.9rem; }
  .wf header { display: flex; align-items: center; gap: 0.5rem; }
  .name { font-weight: 600; font-size: 0.92rem; }
  .tag {
    font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.04em;
    color: var(--p-888888); border: 1px solid var(--p-3a3a3a);
    border-radius: 3px; padding: 0 0.3rem;
  }
  .tag.warn { color: var(--warn, #e0a030); border-color: var(--warn, #e0a030); }
  .desc { margin: 0.3rem 0 0.6rem; font-size: 0.82rem; color: var(--text-dim); max-width: 46rem; }
  .step {
    display: flex; gap: 1rem; align-items: flex-start;
    padding: 0.45rem 0; border-top: 1px solid var(--p-2a2a2a);
  }
  .who { flex: 0 0 13rem; }
  .worker { display: block; font-family: var(--mono, ui-monospace); font-size: 0.82rem; }
  .what { display: block; font-size: 0.75rem; color: var(--text-dim); }
  .pick { flex: 1; min-width: 0; }
  select {
    width: 100%; max-width: 28rem;
    background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 4px;
    padding: 0.3rem 0.45rem; font-size: 0.82rem;
  }
  .short { margin: 0.25rem 0 0; font-size: 0.76rem; color: var(--danger, #e74c3c); }
  .need { margin: 0.25rem 0 0; font-size: 0.72rem; color: var(--p-777777); }
  .err { color: var(--danger, #e74c3c); font-size: 0.82rem; }
  .ok { color: var(--ok, #4caf72); font-size: 0.82rem; }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
</style>
