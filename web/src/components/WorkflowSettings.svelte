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
  /** How each agent role will run, or why it cannot (Fix scanner findings). */
  let agents = $state<api.AgentsStatus | null>(null);
  let providers = $state<ModelProvider[]>([]);
  let loaded = $state(false);
  let error = $state('');
  /** Confirmation that a change landed. Without it, a select that saves
   *  silently is indistinguishable from one that does nothing — which is
   *  exactly what this panel was accused of. */
  let saved = $state('');

  async function load() {
    try {
      const [wfs, ps, ag] = await Promise.all([api.listWorkflows(), api.listModelProviders(), api.agentsStatus().catch(() => null)]);
      agents = ag;
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

  /** The agent role behind a step of Fix scanner findings, if it is one. */
  function roleOf(wf: Workflow, workerType: string) {
    if (wf.id !== 'fix_findings') return undefined;
    return agents?.roles.find((r) => r.worker_type === workerType);
  }

  function via(runsOn: string): string {
    if (runsOn === 'claude-code') return 'Claude Code';
    if (runsOn === 'built-in') return "CodeDistill's agent";
    return runsOn;
  }

  /** Whether a workflow has a Coordinator to configure. */
  const coordinated = (wf: Workflow) => wf.steps.some((st) => st.worker_type === 'coordinator');

  async function coordinate(wf: Workflow, coordinator: 'agent' | 'rules', lenses: string[]) {
    if (lenses.length === 0) {
      error = 'Keep at least one lens: the Security Exploiter attacks every change, but someone must also check it is right.';
      return;
    }
    try {
      await api.setCoordination(wf.id, coordinator, lenses);
      await load();
      saved = coordinator === 'rules'
        ? `${wf.name}: the built-in rules make the judgment calls.`
        : `${wf.name}: the Coordinator makes the judgment calls.`;
      error = '';
      setTimeout(() => (saved = ''), 4000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
  function toggleLens(wf: Workflow, id: string, on: boolean) {
    const next = api.LENSES.map((l) => l.id).filter((l) => (l === id ? on : wf.lenses.includes(l)));
    void coordinate(wf, wf.coordinator, next);
  }

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
        </header>
        {#if w.description}<p class="desc">{w.description}</p>{/if}
        <!-- What the workflow declares about its jobs: the queue reads these,
             never the workflow's name (docs/design/workflows-and-roles.md). -->
        <p class="settings">
          <span class="tag" class:warn={!w.resumable}
                title={w.resumable ? 'Its jobs can pause and carry on where they stopped: they can be paused to make room for an Immediate job, resumed, and survive a restart.' : 'Its jobs cannot carry on where they stopped: once started they run to the end, and are never paused to make room.'}>
            {w.resumable ? 'can pause and resume' : 'runs to the end'}
          </span>
          <span class="tag" title="The highest priority its jobs may be given.">
            up to {w.max_priority === 'immediate' ? 'Immediate' : 'High'}
          </span>
          {#if w.fixed_priority}
            <span class="tag" title="Its jobs keep the priority they were given: nobody raises it, and waiting does not either.">fixed priority</span>
          {/if}
        </p>

        {#if coordinated(w)}
          <!-- Who makes the judgment calls, and who reviews each change
               (docs/design/coordinator-agent.md). -->
          <div class="coord">
            <div class="row">
              <span class="label">Judgment calls</span>
              <div class="seg" role="radiogroup" aria-label="Who makes the judgment calls">
                <button role="radio" aria-checked={w.coordinator === 'agent'} class:on={w.coordinator === 'agent'}
                        onclick={() => coordinate(w, 'agent', w.lenses)}>The Coordinator</button>
                <button role="radio" aria-checked={w.coordinator === 'rules'} class:on={w.coordinator === 'rules'}
                        onclick={() => coordinate(w, 'rules', w.lenses)}>Rules (built in)</button>
              </div>
            </div>
            <p class="hint">
              {#if w.coordinator === 'agent'}
                A model decides which items need work, which objections are real and what to try next, on the
                model chosen for the coordinator below. It writes no code and cannot pass work: only verification
                does. When it cannot decide, the built-in rules do, and the job says so.
              {:else}
                Every objection goes back to the Remediator, failed checks go back, and an item going in
                circles is handed to you: CodeDistill's behaviour before the Coordinator.
              {/if}
            </p>
            <div class="row lenses">
              <span class="label">Challengers</span>
              <div class="checks">
                <label class="lens fixed" title="Attacks every change in every round, and a security finding's plan. Not optional.">
                  <input type="checkbox" checked disabled /> Security Exploiter <span class="always">every change</span>
                </label>
                {#each api.LENSES as l (l.id)}
                  <label class="lens" title={l.question}>
                    <input type="checkbox" checked={w.lenses.includes(l.id)}
                           onchange={(e) => toggleLens(w, l.id, e.currentTarget.checked)} />
                    {l.name}
                  </label>
                {/each}
              </div>
            </div>
          </div>
        {/if}

        {#each w.steps as st (st.ordinal)}
          <div class="step">
            <div class="who">
              <span class="worker">{roleOf(w, st.worker_type)?.role ?? st.worker_type}</span>
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
              {#if roleOf(w, st.worker_type)?.unavailable}
                <p class="short">Cannot run: {roleOf(w, st.worker_type)?.unavailable}</p>
              {:else if roleOf(w, st.worker_type)}
                {@const role = roleOf(w, st.worker_type)!}
                <p class="need">runs through {via(role.runs_on)}{role.model ? ` · ${role.model}` : ''}</p>
              {/if}
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
  .settings { display: flex; flex-wrap: wrap; gap: 0.35rem; margin: 0.1rem 0 0.5rem; }
  .settings .tag { cursor: help; }
  .desc { margin: 0.3rem 0 0.6rem; font-size: 0.82rem; color: var(--text-dim); max-width: 46rem; }
  .step {
    display: flex; gap: 1rem; align-items: flex-start;
    padding: 0.45rem 0; border-top: 1px solid var(--p-2a2a2a);
  }
  .who { flex: 0 0 13rem; }
  .worker { display: block; font-size: 0.84rem; font-weight: 600; }
  .worker::first-letter { text-transform: uppercase; }
  .what { display: block; font-size: 0.75rem; color: var(--text-dim); }
  .pick { flex: 1; min-width: 0; }
  select {
    width: 100%; max-width: 28rem;
    background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 4px;
    padding: 0.3rem 0.45rem; font-size: 0.82rem;
  }
  .coord { border-top: 1px solid var(--p-2a2a2a); padding: 0.55rem 0 0.45rem; display: flex; flex-direction: column; gap: 0.4rem; }
  .coord .row { display: flex; align-items: center; gap: 1rem; }
  .coord .label { flex: 0 0 13rem; font-size: 0.82rem; }
  .seg { display: inline-flex; border: 1px solid var(--p-3a3a3a); border-radius: 5px; overflow: hidden; }
  .seg button { background: none; border: 0; color: var(--p-999999); font: inherit; font-size: 0.78rem; padding: 0.2rem 0.65rem; cursor: pointer; }
  .seg button + button { border-left: 1px solid var(--p-3a3a3a); }
  .seg button.on { background: var(--p-2a2a2a); color: var(--p-e0e0e0); }
  .hint { margin: 0 0 0 14rem; font-size: 0.74rem; color: var(--p-888888); max-width: 40rem; }
  .checks { flex: 1; min-width: 0; display: flex; flex-wrap: wrap; gap: 0.35rem 0.9rem; }
  .lens { display: inline-flex; align-items: center; gap: 0.3rem; font-size: 0.8rem; cursor: pointer; }
  .lens.fixed { cursor: help; color: var(--p-aaaaaa); }
  .always { font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--p-777777); }
  @media (max-width: 640px) {
    .coord .row { flex-wrap: wrap; }
    .coord .label { flex-basis: 100%; }
    .hint { margin-left: 0; }
  }
  .short { margin: 0.25rem 0 0; font-size: 0.76rem; color: var(--danger, #e74c3c); }
  .need { margin: 0.25rem 0 0; font-size: 0.72rem; color: var(--p-777777); }
  .err { color: var(--danger, #e74c3c); font-size: 0.82rem; }
  .ok { color: var(--ok, #4caf72); font-size: 0.82rem; }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
</style>
