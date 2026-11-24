<!--
  Start a job.

  Deliberately generic: this form is built from the chosen workflow's DECLARED
  parameters, not from a branch per workflow. A workflow nobody has written yet
  gets a working submission dialog the moment its rows exist — which is the
  whole reason workflows and their parameters became data.

  Separate from the monitor on purpose. Watching work and starting it are
  different acts, and a panel that does both makes the common case (glancing at
  what is running) carry the weight of the rare one.
-->
<script lang="ts">
  import { can } from '../lib/session.svelte';
  import * as api from '../lib/api';
  import type { Workflow, WorkflowParam } from '../lib/api';
  // Scratchpad is declared but not exported by the client; the shape used here
  // is only what the picker needs.
  type Pad = { id: string; name: string };
  import Modal from './Modal.svelte';

  type Props = {
    projectId: string;
    open: boolean;
    onClose: () => void;
    /** Fired after a successful submission, so the monitor can show the run. */
    onSubmitted?: (jobId: string) => void;
    /** Pre-select a workflow when the dialog is opened from its own context. */
    initialWorkflow?: string;
    /** Answers supplied by whatever opened the dialog, by param key. */
    prefill?: Record<string, string>;
    /** How to name prefilled item ids ("gosec G401 · app.go"), for a field
     *  that takes several items: an id is not something a person can check. */
    prefillLabels?: Record<string, string>;
  };
  let { projectId, open, onClose, onSubmitted, initialWorkflow, prefill, prefillLabels }: Props = $props();

  let workflows = $state<Workflow[]>([]);
  let scratchpads = $state<Pad[]>([]);
  /** Candidate documents across every scratchpad in the project — a document
   *  dropped on any pad should be reachable from here, not just the active one. */
  let documents = $state<{ id: string; label: string; pad: string; kind: string }[]>([]);
  let uploading = $state(false);
  let chosen = $state('');
  let values = $state<Record<string, string>>({});
  let busy = $state(false);
  let error = $state('');
  /** Which field the server blamed, so the message lands on the control
   *  rather than as a sentence under the OK button. */
  let badField = $state('');
  let result = $state<api.SubmitResult | null>(null);
  /** Orders the job in the job queue when every slot is busy. */
  let priority = $state<api.JobPriority>('medium');
  let slots = $state<api.JobSlots | null>(null);

  const workflow = $derived(workflows.find((w) => w.id === chosen));

  $effect(() => {
    if (!open) return;
    void (async () => {
      error = '';
      const [wfs, sps] = await Promise.all([
        api.listWorkflows().catch(() => [] as Workflow[]),
        api.listScratchpads(projectId).catch(() => [] as Pad[]),
      ]);
      workflows = (wfs ?? []).filter((w) => w.enabled);
      api.getJobSlots().then((x) => (slots = x)).catch(() => (slots = null));
      scratchpads = sps ?? [];
      // Opened from a context (a finding, a scratchpad): that workflow, even if
      // a different one was chosen the last time the dialog was open.
      if (initialWorkflow) chosen = initialWorkflow;
      else if (!chosen) chosen = workflows[0]?.id ?? '';
      await loadDocuments();
      // Every opening starts from the workflow's defaults plus what this
      // opening supplied. Keeping the last opening's answers carried one
      // finding's item into a "fix the whole scratchpad" run.
      const w = workflows.find((x) => x.id === chosen);
      if (w) {
        const next: Record<string, string> = {};
        for (const p of w.params ?? []) next[p.key] = prefill?.[p.key] ?? p.default ?? '';
        values = next;
        lastLoaded = w.id;
      } else {
        for (const [k, v] of Object.entries(prefill ?? {})) values[k] = v;
      }
    })();
  });

  // Defaults are applied when the workflow changes, so switching does not carry
  // the previous workflow's answers into fields that happen to share a key.
  let lastLoaded = '';
  $effect(() => {
    const w = workflow;
    if (!w || w.id === lastLoaded) return;
    lastLoaded = w.id;
    const next: Record<string, string> = {};
    for (const p of w.params ?? []) next[p.key] = prefill?.[p.key] ?? p.default ?? '';
    values = next;
    badField = '';
    error = '';
    result = null;
    // A workflow that goes no higher than High cannot keep an Immediate choice.
    if (priority === 'immediate' && w.max_priority !== 'immediate') priority = 'high';
  });

  /** What could plausibly be a document: an uploaded file, or a text item with
   *  enough in it to be a specification rather than a note. Filtering here
   *  rather than showing everything — a picker listing 400 stickies is a picker
   *  nobody uses. */
  async function loadDocuments() {
    const pads = scratchpads;
    const found: { id: string; label: string; pad: string; kind: string }[] = [];
    for (const pad of pads) {
      let items: Awaited<ReturnType<typeof api.listItems>> = [];
      try {
        items = await api.listItems(pad.id);
      } catch { continue; }
      for (const it of items ?? []) {
        const name = it.file_name || it.name || '';
        const isFile = !!it.blob_sha;
        const readable = /\.(md|txt|odt|docx)$/i.test(name);
        if (isFile) {
          // A .pdf or an image is a file too; only offer what the reader can
          // actually read, rather than failing after they choose.
          if (!readable) continue;
          found.push({ id: it.id, label: name, pad: pad.name, kind: 'file' });
        } else if ((it.content ?? '').length > 400) {
          found.push({
            id: it.id,
            label: name || (it.content ?? '').split('\n')[0].slice(0, 60),
            pad: pad.name, kind: 'text',
          });
        }
      }
    }
    documents = found;
  }

  /** The file picker. A browser cannot hand over a filesystem path, so the file
   *  is uploaded — which makes it a scratchpad item, exactly what dropping it
   *  would have done. Both routes converge before the job is submitted. */
  async function pickFile(key: string, e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    const padId = scratchpads[0]?.id;
    if (!padId) { error = 'This project has no scratchpad to put the document on.'; return; }
    uploading = true;
    error = '';
    try {
      const item = await api.uploadBlob(padId, file);
      await loadDocuments();
      values[key] = item.id;
    } catch (err) {
      error = `Could not upload ${file.name}: ${err instanceof Error ? err.message : err}`;
    } finally {
      uploading = false;
      input.value = '';
    }
  }

  async function submit() {
    if (!workflow) return;
    busy = true;
    error = '';
    badField = '';
    try {
      const r = await api.submitJob(workflow.id, projectId, values, priority);
      result = r;
      onSubmitted?.(r.job_id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      // The server names the offending field; recover it from the message it
      // was attached to rather than re-posting to find out.
      badField = fieldFromError(error);
    } finally {
      busy = false;
    }
  }

  /** The API returns {"error": ..., "field": ...} but the client lifts only the
   *  message. Matching on the label is the cheap half of that; a mismatch just
   *  means the error shows without highlighting a control. */
  function fieldFromError(msg: string): string {
    for (const p of workflow?.params ?? []) {
      if (msg.toLowerCase().includes(p.label.toLowerCase())) return p.key;
    }
    return '';
  }

  function ordinal(n: number): string {
    const sfx = ['th', 'st', 'nd', 'rd'];
    const v = n % 100;
    return n + (sfx[(v - 20) % 10] || sfx[v] || sfx[0]);
  }

  function close() {
    result = null;
    priority = 'medium';
    error = '';
    onClose();
  }

  /** An unknown type renders as free text. Dropping it would submit the job
   *  with a missing argument and fail somewhere the user cannot connect. */
  function kindOf(p: WorkflowParam): string {
    const known = ['file', 'directory', 'scratchpad', 'scratchpad_item',
                   'project', 'text', 'bool', 'select'];
    return known.includes(p.type) ? p.type : 'text';
  }
</script>

<Modal {open} onClose={close} title="Start a job" width="34rem">
  {#if result}
    <!-- Submitted. What the run is, and where it went. -->
    <div class="done">
      {#if result.queue && !result.queue.starts_now}
        <!-- Every slot is busy: it waits its turn (docs/design/job-queue.md). -->
        <p class="started">Queued: <strong>{result.label ?? chosen}</strong>, {ordinal(result.queue.position)} in line.</p>
        <p class="detail">All {result.queue.slots} job slot{result.queue.slots === 1 ? ' is' : 's are'} busy; it starts when one frees.</p>
      {:else}
        <p class="started">Started on <strong>{result.label ?? chosen}</strong>.</p>
      {/if}
      {#if result.provider}
        <p class="detail">
          Running on {result.provider}{result.model ? ` (${result.model})` : ''}.
          {#if result.leaves_machine}
            <span class="warn">This sends the document off this machine.</span>
          {/if}
        </p>
      {/if}
      {#if result.note}<p class="detail">{result.note}</p>{/if}
      <p class="detail muted">It runs in the background. Watch it in Jobs.</p>
      <div class="actions">
        <button class="primary" onclick={close}>Done</button>
      </div>
    </div>
  {:else}
    <label class="field">
      <span class="lbl">Workflow</span>
      <select bind:value={chosen} disabled={busy}>
        {#each workflows as w (w.id)}<option value={w.id}>{w.name}</option>{/each}
      </select>
    </label>
    {#if workflow?.description}
      <p class="wfdesc">{workflow.description}</p>
    {/if}

    {#if workflow && (workflow.params ?? []).length === 0}
      <p class="muted">This workflow takes no parameters.</p>
    {/if}

    {#each workflow?.params ?? [] as p (p.key)}
      {@const kind = kindOf(p)}
      <div class="field" class:bad={badField === p.key}>
        {#if kind === 'bool'}
          <label class="check">
            <input
              type="checkbox"
              checked={values[p.key] === 'true'}
              disabled={busy}
              onchange={(e) => (values[p.key] = e.currentTarget.checked ? 'true' : '')}
            />
            <span>{p.label}</span>
          </label>
        {:else}
          <span class="lbl">
            {p.label}{#if !p.required}<span class="opt">optional</span>{/if}
          </span>
          {#if kind === 'scratchpad_item' && p.multiple}
            <!-- Several items: the ones the dialog was opened for, or none,
                 which means the whole scratchpad. Choosing from scratch is
                 done where the items are, not from a list of ids here. -->
            {@const ids = (values[p.key] ?? '').split(',').filter(Boolean)}
            {#if ids.length}
              <ul class="picked">
                {#each ids as itemId (itemId)}<li>{prefillLabels?.[itemId] ?? itemId}</li>{/each}
              </ul>
              <button type="button" class="linkish" disabled={busy} onclick={() => (values[p.key] = '')}>
                Work the whole scratchpad instead
              </button>
            {:else}
              <p class="help">The whole scratchpad.</p>
            {/if}
          {:else if kind === 'scratchpad_item'}
            <div class="docpick">
              <select bind:value={values[p.key]} disabled={busy || uploading}>
                <option value="">— choose a document —</option>
                {#each documents as d (d.id)}
                  <option value={d.id}>{d.label} · {d.pad}</option>
                {/each}
              </select>
              <label class="upload" class:busy={uploading}>
                {uploading ? 'Uploading…' : 'Upload…'}
                <input
                  type="file"
                  accept=".md,.txt,.odt,.docx"
                  disabled={busy || uploading}
                  onchange={(e) => pickFile(p.key, e)}
                />
              </label>
            </div>
            {#if documents.length === 0 && !uploading}
              <p class="help">
                Nothing on this project's scratchpads looks like a document yet.
                Upload one, or drag it onto a scratchpad first.
              </p>
            {/if}
          {:else if kind === 'scratchpad'}
            <select bind:value={values[p.key]} disabled={busy}>
              <option value="">— choose —</option>
              {#each scratchpads as sp (sp.id)}<option value={sp.id}>{sp.name}</option>{/each}
            </select>
          {:else if kind === 'select'}
            <select bind:value={values[p.key]} disabled={busy}>
              {#if !p.required}<option value="">— none —</option>{/if}
              {#each p.options ?? [] as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
            </select>
          {:else}
            <!-- A path is typed rather than picked: the browser cannot hand
                 back a real filesystem path, and the server reads the file. -->
            <input
              type="text"
              bind:value={values[p.key]}
              disabled={busy}
              placeholder={kind === 'file' ? '/path/to/document.odt'
                        : kind === 'directory' ? '/path/to/folder' : ''}
            />
          {/if}
        {/if}
        {#if p.help}<p class="help">{p.help}</p>{/if}
      </div>
    {/each}

    <label class="field">
      <span class="lbl">Priority</span>
      <select bind:value={priority} disabled={busy}>
        {#if workflow?.max_priority === 'immediate' && can('manage_all_jobs')}<option value="immediate">Immediate</option>{/if}
        <option value="high">High</option>
        <option value="medium">Medium</option>
        <option value="low">Low</option>
      </select>
      <!-- Said before Start, not after: whether it runs now or waits. -->
      {#if workflow?.fixed_priority}
        <p class="help">This workflow's jobs keep the priority they are given: it cannot be raised later, and waiting does not raise it.</p>
      {:else if priority === 'immediate'}
        <p class="help">Starts now. If every job slot is busy, the lowest-priority fix job pauses to make room and carries on where it stopped when a slot frees.</p>
      {:else if slots && slots.running >= slots.slots}
        <p class="help">All {slots.slots} job slot{slots.slots === 1 ? ' is' : 's are'} busy{slots.queued ? ` and ${slots.queued} waiting` : ''}: this will wait its turn. Higher priority goes first.</p>
      {:else}
        <p class="help">Used when every job slot is busy: higher priority goes first.</p>
      {/if}
    </label>

    {#if error}<p class="err" role="alert">{error}</p>{/if}

    <div class="actions">
      <button onclick={close} disabled={busy}>Cancel</button>
      <button class="primary" onclick={submit} disabled={busy || !workflow}>
        {busy ? 'Starting…' : 'Start'}
      </button>
    </div>
  {/if}
</Modal>

<style>
  .field { display: flex; flex-direction: column; gap: 0.25rem; margin-bottom: 0.9rem; }
  .lbl { font-size: 0.8rem; color: var(--text-dim); }
  .opt { margin-left: 0.4rem; font-size: 0.72rem; color: var(--p-666666); }
  .field.bad input, .field.bad select { border-color: var(--danger, #c0392b); }
  .wfdesc { margin: -0.4rem 0 0.9rem; font-size: 0.82rem; color: var(--text-dim); }
  .help { margin: 0; font-size: 0.75rem; color: var(--p-888888); }
  .check { display: flex; align-items: center; gap: 0.45rem; font-size: 0.85rem; }
  .check input { width: auto; }
  input[type='text'], select {
    background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 4px;
    padding: 0.35rem 0.5rem; font-size: 0.85rem;
  }
  .docpick { display: flex; gap: 0.5rem; align-items: center; }
  .docpick select { flex: 1; min-width: 0; }
  .upload {
    border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a);
    color: var(--text); border-radius: 4px; padding: 0.35rem 0.7rem;
    font-size: 0.8rem; cursor: pointer; white-space: nowrap;
  }
  .upload:hover { background: var(--p-333333); }
  .upload.busy { opacity: 0.6; cursor: default; }
  .upload input { display: none; }
  .actions { display: flex; justify-content: flex-end; gap: 0.5rem; margin-top: 1rem; }
  .actions button {
    border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a);
    color: var(--text); border-radius: 4px; padding: 0.35rem 0.9rem;
    font-size: 0.85rem; cursor: pointer;
  }
  .actions .primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .actions button:disabled { opacity: 0.5; cursor: default; }
  .err { color: var(--danger, #e74c3c); font-size: 0.82rem; }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
  .started { margin: 0 0 0.4rem; font-size: 0.9rem; }
  .detail { margin: 0.2rem 0; font-size: 0.82rem; color: var(--text-dim); }
  .warn { color: var(--warn, #e0a030); }
  .picked { margin: 0.1rem 0 0.2rem; padding-left: 1.1rem; font-family: ui-monospace, monospace; font-size: 0.8rem; }
  .linkish { align-self: flex-start; text-align: left; background: none; border: 0; padding: 0;
             color: var(--accent); font: inherit; font-size: 0.8rem; cursor: pointer; }
  .linkish { text-decoration: underline; text-underline-offset: 2px; }
  .linkish:disabled { opacity: 0.5; cursor: default; }
</style>
