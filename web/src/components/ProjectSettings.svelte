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
  import { confirmDialog } from '../lib/dialog';
  import * as api from '../lib/api';
  import type { Project } from '../lib/types';
  import Modal from './Modal.svelte';
  import CustomFieldsAdmin from './CustomFieldsAdmin.svelte';
  import SkillsAdmin from './SkillsAdmin.svelte';

  // Per-project settings modal — currently scoped to rich-canvas
  // blob policy (max upload size + MIME allowlist). Reads/writes via
  // the generic /api/v1/projects/{pid}/settings endpoints; the
  // backend cascade (project → user → installation default)
  // resolves the effective config on every upload.
  //
  // Semantics: when a project setting is present, it FULLY REPLACES
  // the default for that key (not additive). The UI preloads with
  // the current effective list so the user can edit-in-place. "Reset
  // to defaults" DELETEs the project setting, falling back to the
  // cascade.

  type Props = {
    open: boolean;
    project: Project | null;
    onClose: () => void;
  };
  let { open, project, onClose }: Props = $props();

  // Defaults mirrored from internal/api/server.go DefaultBlobConfig().
  // Drift risk: if backend defaults change, update here. A future patch
  // could expose these via an API endpoint to avoid duplication.
  const DEFAULT_MAX_BYTES = 10 * 1024 * 1024; // 10 MiB
  const DEFAULT_ALLOWED_MIMES = [
    'image/png', 'image/jpeg', 'image/gif', 'image/webp',
    'application/pdf',
    'text/plain', 'text/csv', 'text/markdown',
    'application/json',
    'application/zip',
    'video/mp4', 'video/webm',
  ];

  const KEY_MAX = 'blob.max_bytes_per_item';
  const KEY_MIMES = 'blob.allowed_mimes';

  // Verification checks (glass-box Phase 3): the test suite + scanners, each an
  // optional per-kind command. Keys mirror verify.CheckSpecs in the backend.
  // `field` maps to VerifyDefaults so placeholders/prefill follow the project's
  // detected language; `placeholder` is the neutral fallback when undetected.
  const VERIFY_FIELDS = [
    { key: 'verify.test_command', field: 'test', label: 'Test command', placeholder: 'go test ./...' },
    { key: 'verify.lint_command', field: 'lint', label: 'Lint command', placeholder: 'golangci-lint run' },
    { key: 'verify.types_command', field: 'types', label: 'Type-check command', placeholder: 'go vet ./...' },
    { key: 'verify.sast_command', field: 'sast', label: 'SAST command', placeholder: 'gosec ./...' },
    { key: 'verify.vuln_command', field: 'vuln', label: 'Dependency-vuln command', placeholder: 'govulncheck ./...' },
  ] as const;

  // Example commands for the project's detected language (from repo_root marker
  // files). Drives language-aware placeholders + the "Use <lang> defaults"
  // button. null until loaded; language === '' means we couldn't detect.
  let verifyDefaults = $state<api.VerifyDefaults | null>(null);
  // Placeholder hint for a field: the detected-language example if we have one,
  // else the neutral Go-shaped fallback baked into VERIFY_FIELDS.
  function verifyPlaceholder(f: (typeof VERIFY_FIELDS)[number]): string {
    const d = verifyDefaults;
    if (d && d.language && d[f.field]) return d[f.field];
    return f.placeholder;
  }
  // Fill empty command fields with the detected-language defaults. Never
  // overwrites a command you've already entered; the user still saves explicitly.
  function applyVerifyDefaults() {
    const d = verifyDefaults;
    if (!d || !d.language) return;
    const next = { ...verifyCmds };
    for (const f of VERIFY_FIELDS) {
      if (!next[f.key]?.trim() && d[f.field]) next[f.key] = d[f.field];
    }
    verifyCmds = next;
  }

  let loading = $state(false);
  let saving = $state(false);
  let err = $state('');
  let saveMsg = $state('');

  // Tabs group the (now many) sections by concern. Save/load stay global —
  // tabs only control which sections are visible.
  const SETTINGS_TABS = [
    { id: 'uploads', label: 'Uploads' },
    { id: 'models', label: 'Models' },
    { id: 'verification', label: 'Verification' },
    { id: 'indexing', label: 'Indexing' },
    { id: 'items', label: 'Items' },
    { id: 'skills', label: 'Skills' },
    { id: 'trust', label: 'Trust' },
    { id: 'analysis', label: 'Analysis' },
  ];
  let activeTab = $state('uploads');

  // Form state. The fields below reflect what the user CURRENTLY
  // sees — they're seeded from the project setting if present, else
  // from the defaults above. `maxOverridden` / `mimesOverridden`
  // tell us whether the values came from a real project setting
  // (true) or the defaults (false) — used to show the "Override
  // active" vs "Using default" hint.
  let maxMiB = $state<number>(10);
  let maxOverridden = $state(false);
  let mimesText = $state(DEFAULT_ALLOWED_MIMES.join('\n'));
  let mimesOverridden = $state(false);
  // Per-kind verify commands keyed by setting key; empty = that check disabled.
  let verifyCmds = $state<Record<string, string>>({});
  // Adversarial AI-review layer (opt-in): an independent local model refutes each
  // criterion against the diff. Advisory — never moves criterion state.
  let aiReview = $state(false);
  const KEY_AI_REVIEW = 'verify.ai_review';
  // Which local model the reviewer runs on (empty = the app default). A stronger
  // code model makes the adversarial review a better guard.
  let reviewerModel = $state('');
  let availableModels = $state<string[]>([]);
  const KEY_REVIEWER_MODEL = 'verify.reviewer_model';
  // Per-role models (model-provider arc): each LLM role can run a different
  // local model. Empty = the app's default model. Local-only (Ollama).
  let classifierModel = $state('');
  let criteriaModel = $state('');
  const KEY_MODEL_CLASSIFIER = 'model.classifier';
  const KEY_MODEL_CRITERIA = 'model.criteria';
  // Always-review zones (glass-box Phase 4): path patterns whose changes always
  // escalate for human review regardless of risk (auth/billing/migrations).
  let zonesText = $state('');
  const KEY_ZONES = 'review.always_review_zones';
  // Human tightening of the earned-trust threshold (slice 4.2). Can only escalate
  // MORE than the machine's earned level, never less.
  let tightenTo = $state('');
  const KEY_TIGHTEN = 'review.tighten_to';
  // Indexing scope: this project decides what its code index covers. We never
  // make that call for someone else's repo — these globs add to (or, with
  // ignoreBuiltin, replace) our built-in generated/vendored conventions.
  let excludeText = $state('');
  let ignoreBuiltin = $state(false);
  const KEY_INDEX_EXCLUDE = 'index.exclude';
  const KEY_INDEX_IGNORE_BUILTIN = 'index.ignore_builtin';
  // Governance (glass-box Phase 6): the verification enforcement level. Levels
  // above "flag" require the paid Governance feature; otherwise advisory.
  let govVerification = $state<api.EnforcementLevel>('flag');
  let govReview = $state<api.EnforcementLevel>('flag');
  let govArchitecture = $state<api.EnforcementLevel>('flag');
  let govSecurity = $state<api.EnforcementLevel>('flag');
  let govLicensed = $state(false);
  // Earned-trust readout for the Trust tab (read-only context for the dial).
  let projTrust = $state<api.ProjectTrust | null>(null);
  let projTrustEscalate = $state('');
  $effect(() => {
    if (open && activeTab === 'trust' && project) {
      api.getReviewQueue(project.id)
        .then((q) => { projTrust = q.trust; projTrustEscalate = q.escalate_at_or_above; })
        .catch(() => { projTrust = null; });
    }
  });
  // Archive (item #19): what the scratchpad-item Delete button does.
  let deleteAction = $state<'delete' | 'archive' | 'prompt'>('prompt');
  const KEY_DELETE_ACTION = 'archive.delete_action';
  // Dedup advisory threshold (the "possibly related" banner cutoff; grouping
  // stays at 0.85). Lower = more banners surfaced. Default 0.73.
  let dedupThreshold = $state(0.73);
  const KEY_DEDUP_THRESHOLD = 'dedup.threshold';
  const KEY_GOV_VERIFY = 'governance.verification';
  const KEY_GOV_REVIEW = 'governance.review';
  const KEY_GOV_ARCH = 'governance.architecture';
  const KEY_GOV_SECURITY = 'governance.security';
  // Analysis pipeline (Enterprise): watched directory + auto-route target/threshold.
  let watchDir = $state('');
  let routeTarget = $state('');
  let routeMinSev = $state<'high' | 'medium' | 'low'>('high');
  const KEY_WATCH_DIR = 'analysis.watch_dir';
  const KEY_ROUTE_TARGET = 'analysis.findings_target_scratchpad';
  const KEY_ROUTE_MINSEV = 'analysis.auto_route_min_severity';

  // A GET that 404s means the setting is genuinely unset (use the default).
  // ANY OTHER error (500, network) is a transient failure — we must NOT treat
  // it as unset, because Save then writes the default over the real stored
  // value, silently wiping settings (audit H11). loadFailed records this so
  // Save can refuse until a clean reload.
  let loadFailed = $state(false);
  function unset(e: unknown): boolean {
    if (e instanceof Error && / 404\)$/.test(e.message)) return true;
    loadFailed = true; // transient — remember it
    return false;
  }

  async function load() {
    if (!project) return;
    loading = true;
    err = '';
    saveMsg = '';
    loadFailed = false;
    try {
      // Get-or-default for both keys. The settings GET returns 204 No Content
      // when the key isn't set, so `r` is undefined and the local default
      // stands (CE-review item 43 — it used to 404 and spam the console).
      try {
        const r = await api.getProjectSetting(project.id, KEY_MAX);
        const v = r?.value;
        if (typeof v === 'number' && v > 0) {
          maxMiB = Math.round(v / 1024 / 1024);
          maxOverridden = true;
        }
      } catch (e) { unset(e);
        maxMiB = Math.round(DEFAULT_MAX_BYTES / 1024 / 1024);
        maxOverridden = false;
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_MIMES);
        const v = r?.value;
        if (Array.isArray(v) && v.every((x) => typeof x === 'string')) {
          mimesText = v.join('\n');
          mimesOverridden = true;
        }
      } catch (e) { unset(e);
        mimesText = DEFAULT_ALLOWED_MIMES.join('\n');
        mimesOverridden = false;
      }
      const cmds: Record<string, string> = {};
      for (const f of VERIFY_FIELDS) {
        try {
          const r = await api.getProjectSetting(project.id, f.key);
          cmds[f.key] = typeof r?.value === 'string' ? r?.value : '';
        } catch (e) { unset(e);
          cmds[f.key] = '';
        }
      }
      verifyCmds = cmds;
      try {
        verifyDefaults = await api.getVerifyDefaults(project.id);
      } catch (e) { unset(e);
        verifyDefaults = null;
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_AI_REVIEW);
        aiReview = r?.value === true;
      } catch (e) { unset(e);
        aiReview = false;
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_REVIEWER_MODEL);
        reviewerModel = typeof r?.value === 'string' ? r?.value : '';
      } catch (e) { unset(e);
        reviewerModel = '';
      }
      try {
        availableModels = await api.listOllamaModels();
      } catch (e) { unset(e);
        availableModels = [];
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_MODEL_CLASSIFIER);
        classifierModel = typeof r?.value === 'string' ? r?.value : '';
      } catch (e) { unset(e);
        classifierModel = '';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_MODEL_CRITERIA);
        criteriaModel = typeof r?.value === 'string' ? r?.value : '';
      } catch (e) { unset(e);
        criteriaModel = '';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_ZONES);
        zonesText = Array.isArray(r?.value) ? (r?.value as string[]).join('\n') : '';
      } catch (e) { unset(e);
        zonesText = '';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_TIGHTEN);
        tightenTo = typeof r?.value === 'string' ? r?.value : '';
      } catch (e) { unset(e);
        tightenTo = '';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_INDEX_EXCLUDE);
        excludeText = Array.isArray(r?.value) ? (r?.value as string[]).join('\n') : '';
      } catch (e) { unset(e);
        excludeText = '';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_INDEX_IGNORE_BUILTIN);
        ignoreBuiltin = r?.value === true;
      } catch (e) { unset(e);
        ignoreBuiltin = false;
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_DELETE_ACTION);
        if (r?.value === 'delete' || r?.value === 'archive' || r?.value === 'prompt') deleteAction = r?.value;
      } catch (e) { unset(e);
        deleteAction = 'prompt';
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_DEDUP_THRESHOLD);
        if (typeof r?.value === 'number') dedupThreshold = r?.value;
      } catch (e) { unset(e);
        dedupThreshold = 0.73;
      }
      try {
        const r = await api.getProjectSetting(project.id, KEY_WATCH_DIR);
        if (typeof r?.value === 'string') watchDir = r?.value;
      } catch (e) { unset(e); watchDir = ''; }
      try {
        const r = await api.getProjectSetting(project.id, KEY_ROUTE_TARGET);
        if (typeof r?.value === 'string') routeTarget = r?.value;
      } catch (e) { unset(e); routeTarget = ''; }
      try {
        const r = await api.getProjectSetting(project.id, KEY_ROUTE_MINSEV);
        if (r?.value === 'high' || r?.value === 'medium' || r?.value === 'low') routeMinSev = r?.value;
      } catch (e) { unset(e); routeMinSev = 'high'; }
      try {
        const g = await api.getGovernance(project.id);
        govVerification = g.verification;
        govReview = g.review;
        govArchitecture = g.architecture;
        govSecurity = g.security;
        govLicensed = g.licensed;
      } catch (e) { unset(e);
        govVerification = 'flag';
        govReview = 'flag';
        govArchitecture = 'flag';
        govSecurity = 'flag';
        govLicensed = false;
      }
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  async function save() {
    if (!project || saving) return;
    // Refuse to save over a failed load — writing the defaults now would wipe
    // whatever's really stored (audit H11). Reload first.
    if (loadFailed) {
      err = 'Settings didn’t load fully — reload before saving, or you’d overwrite existing values.';
      return;
    }
    saving = true;
    err = '';
    saveMsg = '';
    try {
      const bytes = Math.round(maxMiB * 1024 * 1024);
      if (!Number.isFinite(bytes) || bytes < 1024) {
        throw new Error('Max size must be at least 1 KiB');
      }
      await api.setProjectSetting(project.id, KEY_MAX, bytes);

      const mimes = mimesText
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      if (mimes.length === 0) {
        throw new Error('At least one MIME type is required (or use Reset to defaults)');
      }
      await api.setProjectSetting(project.id, KEY_MIMES, mimes);

      // Verify commands: a set value writes the setting; empty clears it.
      for (const f of VERIFY_FIELDS) {
        const cmd = (verifyCmds[f.key] ?? '').trim();
        if (cmd) {
          await api.setProjectSetting(project.id, f.key, cmd);
        } else {
          try { await api.deleteProjectSetting(project.id, f.key); } catch { /* never-set */ }
        }
      }
      if (aiReview) {
        await api.setProjectSetting(project.id, KEY_AI_REVIEW, true);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_AI_REVIEW); } catch { /* never-set */ }
      }
      if (reviewerModel.trim()) {
        await api.setProjectSetting(project.id, KEY_REVIEWER_MODEL, reviewerModel.trim());
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_REVIEWER_MODEL); } catch { /* never-set */ }
      }
      if (classifierModel.trim()) {
        await api.setProjectSetting(project.id, KEY_MODEL_CLASSIFIER, classifierModel.trim());
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_MODEL_CLASSIFIER); } catch { /* never-set */ }
      }
      if (criteriaModel.trim()) {
        await api.setProjectSetting(project.id, KEY_MODEL_CRITERIA, criteriaModel.trim());
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_MODEL_CRITERIA); } catch { /* never-set */ }
      }
      const zones = zonesText.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      if (zones.length) {
        await api.setProjectSetting(project.id, KEY_ZONES, zones);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_ZONES); } catch { /* never-set */ }
      }
      const excludes = excludeText.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      if (excludes.length) {
        await api.setProjectSetting(project.id, KEY_INDEX_EXCLUDE, excludes);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_INDEX_EXCLUDE); } catch { /* never-set */ }
      }
      if (ignoreBuiltin) {
        await api.setProjectSetting(project.id, KEY_INDEX_IGNORE_BUILTIN, true);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_INDEX_IGNORE_BUILTIN); } catch { /* never-set */ }
      }
      if (tightenTo) {
        await api.setProjectSetting(project.id, KEY_TIGHTEN, tightenTo);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_TIGHTEN); } catch { /* never-set */ }
      }
      if (govVerification && govVerification !== 'flag') {
        await api.setProjectSetting(project.id, KEY_GOV_VERIFY, govVerification);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_GOV_VERIFY); } catch { /* never-set */ }
      }
      if (govReview && govReview !== 'flag') {
        await api.setProjectSetting(project.id, KEY_GOV_REVIEW, govReview);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_GOV_REVIEW); } catch { /* never-set */ }
      }
      if (govArchitecture && govArchitecture !== 'flag') {
        await api.setProjectSetting(project.id, KEY_GOV_ARCH, govArchitecture);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_GOV_ARCH); } catch { /* never-set */ }
      }
      if (govSecurity && govSecurity !== 'flag') {
        await api.setProjectSetting(project.id, KEY_GOV_SECURITY, govSecurity);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_GOV_SECURITY); } catch { /* never-set */ }
      }
      if (deleteAction !== 'prompt') {
        await api.setProjectSetting(project.id, KEY_DELETE_ACTION, deleteAction);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_DELETE_ACTION); } catch { /* never-set */ }
      }
      if (Math.abs(dedupThreshold - 0.73) > 1e-6) {
        await api.setProjectSetting(project.id, KEY_DEDUP_THRESHOLD, dedupThreshold);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_DEDUP_THRESHOLD); } catch { /* never-set */ }
      }
      if (watchDir.trim()) {
        await api.setProjectSetting(project.id, KEY_WATCH_DIR, watchDir.trim());
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_WATCH_DIR); } catch { /* never-set */ }
      }
      if (routeTarget.trim()) {
        await api.setProjectSetting(project.id, KEY_ROUTE_TARGET, routeTarget.trim());
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_ROUTE_TARGET); } catch { /* never-set */ }
      }
      if (routeMinSev !== 'high') {
        await api.setProjectSetting(project.id, KEY_ROUTE_MINSEV, routeMinSev);
      } else {
        try { await api.deleteProjectSetting(project.id, KEY_ROUTE_MINSEV); } catch { /* never-set */ }
      }

      maxOverridden = true;
      mimesOverridden = true;
      saveMsg = 'Saved.';
    } catch (e) {
      err = String(e);
    } finally {
      saving = false;
    }
  }

  async function resetToDefaults() {
    if (!project || saving) return;
    if (!await confirmDialog('Reset upload settings for this project to defaults? Existing items are not affected.', { title: 'Reset settings', confirmLabel: 'Reset' })) {
      return;
    }
    saving = true;
    err = '';
    saveMsg = '';
    try {
      // DELETE is idempotent — 404 on a never-set key is fine.
      try { await api.deleteProjectSetting(project.id, KEY_MAX); } catch { /* ignore */ }
      try { await api.deleteProjectSetting(project.id, KEY_MIMES); } catch { /* ignore */ }
      maxMiB = Math.round(DEFAULT_MAX_BYTES / 1024 / 1024);
      maxOverridden = false;
      mimesText = DEFAULT_ALLOWED_MIMES.join('\n');
      mimesOverridden = false;
      saveMsg = 'Reset. Project now uses cascade defaults.';
    } catch (e) {
      err = String(e);
    } finally {
      saving = false;
    }
  }

  // Reload whenever the modal opens against a different project.
  let lastKey = '';
  $effect(() => {
    const key = open && project ? project.id : '';
    if (key !== lastKey) {
      lastKey = key;
      if (key) void load();
    }
  });
</script>

<Modal open={open && project !== null} title={project ? `Project settings — ${project.name}` : ''} onClose={onClose} width="560px">
  {#snippet children()}
    {#if loading}
      <div class="muted">Loading…</div>
    {:else}
      <div class="settings-tabs tab-scroll">
        {#each SETTINGS_TABS as t (t.id)}
          <button
            type="button"
            class="settings-tab"
            class:active={activeTab === t.id}
            onclick={() => (activeTab = t.id)}
          >{t.label}</button>
        {/each}
      </div>

      {#if activeTab === 'uploads'}
      <section class="section">
        <h3>Uploads</h3>
        <p class="hint">
          Limits apply to new file/image uploads in this project. Existing items
          are not affected. The setting cascade is <em>project → user → installation
          default</em>; a value set here fully replaces the default for that field.
        </p>

        <label class="field">
          <span class="lbl">Max file size (MiB)</span>
          <input
            class="num"
            type="number"
            min="1"
            step="1"
            bind:value={maxMiB}
            disabled={saving}
          />
          <span class="status">
            {maxOverridden ? 'Project override active' : 'Using default (10 MiB)'}
          </span>
        </label>

        <label class="field column">
          <span class="lbl">Allowed MIME types (one per line)</span>
          <textarea
            class="mimes"
            rows="10"
            bind:value={mimesText}
            disabled={saving}
            spellcheck="false"
          ></textarea>
          <span class="status">
            {mimesOverridden ? 'Project override active' : 'Using default allowlist'}
          </span>
        </label>
      </section>
      {/if}

      {#if activeTab === 'models'}
      <section class="section">
        <h3>Models</h3>
        <p class="hint">
          The local model each AI role uses (model-provider arc). Local-only —
          models come from your machine's Ollama. Empty = the app's default
          model. The reviewer model lives in <em>Verification</em> below (it's
          tied to the AI-review toggle).
        </p>
        {#snippet modelField(label: string, get: () => string, set: (v: string) => void, hint: string)}
          <label class="field column">
            <span class="lbl">{label}</span>
            {#if availableModels.length > 0}
              <select class="cmd" value={get()} onchange={(e) => set((e.target as HTMLSelectElement).value)} disabled={saving}>
                <option value="">Default (app model)</option>
                {#each availableModels as m (m)}
                  <option value={m}>{m}</option>
                {/each}
              </select>
            {:else}
              <input
                class="cmd"
                type="text"
                placeholder="e.g. qwen2.5:14b (empty = app default)"
                value={get()}
                oninput={(e) => set((e.target as HTMLInputElement).value)}
                disabled={saving}
                spellcheck="false"
              />
            {/if}
            <span class="status">{hint}</span>
          </label>
        {/snippet}
        {@render modelField(
          'Classifier model',
          () => classifierModel,
          (v) => (classifierModel = v),
          'Sorts each paste into TODO/BUG/KB/USE_CASE. A small fast model is usually fine here.'
        )}
        {@render modelField(
          'Criteria drafter model',
          () => criteriaModel,
          (v) => (criteriaModel = v),
          'Drafts acceptance criteria for use cases and bugs. A stronger model writes sharper criteria.'
        )}
      </section>
      {/if}

      {#if activeTab === 'verification'}
      <section class="section">
        <h3>Verification</h3>
        <p class="hint">
          Commands the box runs to verify items in this project (glass-box
          Phase 3) — a test suite plus optional scanners. Each runs in an isolated
          checkout at the commit an item was implemented at; a non-zero exit means
          <em>failing</em>. Runs fire automatically when the agent records an
          implementation. Leave a field empty to skip that check.
        </p>
        {#if verifyDefaults?.language}
          <div class="detected">
            <span>Detected <strong>{verifyDefaults.language}</strong> — examples below match it.</span>
            <button type="button" class="link-btn" onclick={applyVerifyDefaults} disabled={saving}>
              Use {verifyDefaults.language} defaults
            </button>
          </div>
        {/if}
        {#each VERIFY_FIELDS as f (f.key)}
          <label class="field column">
            <span class="lbl">{f.label}</span>
            <input
              class="cmd"
              type="text"
              placeholder={verifyPlaceholder(f)}
              bind:value={verifyCmds[f.key]}
              disabled={saving}
              spellcheck="false"
            />
          </label>
        {/each}
        <label class="toggle">
          <input type="checkbox" bind:checked={aiReview} disabled={saving} />
          <span>
            <span class="lbl">Adversarial AI review</span>
            <span class="status">
              An independent local model reviews each criterion against the diff and
              tries to refute it — an advisory second opinion (never moves criterion
              state). Uses local LLM compute per criterion on each run.
            </span>
          </span>
        </label>
        {#if aiReview}
          <label class="field column indent">
            <span class="lbl">Reviewer model</span>
            {#if availableModels.length > 0}
              <select class="cmd" bind:value={reviewerModel} disabled={saving}>
                <option value="">Default (app model)</option>
                {#each availableModels as m (m)}
                  <option value={m}>{m}</option>
                {/each}
              </select>
            {:else}
              <input
                class="cmd"
                type="text"
                placeholder="e.g. qwen2.5-coder:32b (empty = app default)"
                bind:value={reviewerModel}
                disabled={saving}
                spellcheck="false"
              />
            {/if}
            <span class="status">
              A stronger, code-specialized local model (e.g. qwen2.5-coder) makes the
              review a better guard — larger models want a GPU. Must be a different
              model than the one writing the code.
            </span>
          </label>
        {/if}

      </section>
      {/if}

      {#if activeTab === 'indexing'}
      <section class="section">
        <h3>Indexing</h3>
        <p class="hint">
          What the code index covers in this project. Untracked and
          <code>.gitignore</code>'d files are never indexed; files your
          <code>.gitattributes</code> marks <code>linguist-generated</code>/<code
            >vendored</code
          > are skipped too. Add project-specific exclusions below — your repo,
          your call.
        </p>
        <label class="field column">
          <span class="lbl">Exclude from index</span>
          <textarea
            class="mimes"
            rows="4"
            placeholder={'build/**\nweb/public/vendor/**\n*.generated.go'}
            bind:value={excludeText}
            disabled={saving}
            spellcheck="false"
          ></textarea>
          <span class="status">
            One path pattern per line. <code>dir/**</code> excludes a subtree;
            globs like <code>*.lock</code> match by name; a plain name matches a
            path segment.
          </span>
        </label>
        <label class="toggle">
          <input type="checkbox" bind:checked={ignoreBuiltin} disabled={saving} />
          <span>
            <span class="lbl">Ignore built-in conventions</span>
            <span class="status">
              By default we also skip common generated/vendored paths
              (<code>node_modules</code>, <code>dist</code>, <code>vendor</code>,
              <code>*.min.js</code>…). Check this if your project's layout uses
              those names for real source — only your patterns above (and
              <code>.gitattributes</code>) will apply.
            </span>
          </span>
        </label>
      </section>
      {/if}

      {#if activeTab === 'items'}
      <section class="section">
        <h3>Custom fields</h3>
        <p class="hint">
          Your own typed fields on items — text, number, select, or date. Number
          fields are your metrics (e.g. add a “Story Points” number field).
          Edit values per item in its detail view.
        </p>
        <CustomFieldsAdmin projectId={project?.id ?? ''} />
      </section>
      {/if}

      {#if activeTab === 'skills'}
      <section class="section">
        <h3>Skills</h3>
        <p class="hint">
          Reusable plain-language instruction sets the agent retrieves over MCP
          (like steering files). Give each a title; the agent lists titles with
          <code>list_skills</code> and fetches one with <code>get_skill</code>.
          A Pro feature.
        </p>
        <SkillsAdmin projectId={project?.id ?? ''} />
      </section>
      {/if}

      {#if activeTab === 'items'}
      <section class="section">
        <h3>Archive</h3>
        <p class="hint">
          What the Delete button on a scratchpad item does. Archived items leave
          the canvas but stay in the database and in search, and can be restored.
        </p>
        <label class="field column">
          <span class="lbl">Delete button</span>
          <select class="cmd" bind:value={deleteAction} disabled={saving}>
            <option value="prompt">Ask each time — delete or archive (default)</option>
            <option value="archive">Archive the item</option>
            <option value="delete">Permanently delete the item</option>
          </select>
        </label>
      </section>

      <section class="section">
        <h3>Duplicate detection</h3>
        <p class="hint">
          The similarity cutoff for the “possibly related” banner on items (a
          dismissable hint — it never auto-groups). Lower surfaces more loosely
          related pairs; higher only flags near-identical ones. Auto-grouping
          stays at a stricter 0.85. Requires the Dedup feature.
        </p>
        <label class="field column">
          <span class="lbl">Related-banner threshold: {dedupThreshold.toFixed(2)}</span>
          <input
            type="range"
            min="0.5"
            max="0.95"
            step="0.01"
            bind:value={dedupThreshold}
            disabled={saving}
          />
        </label>
      </section>
      {/if}

      {#if activeTab === 'trust'}
      <section class="section">
        <h3>Trust</h3>
        <p class="hint">
          The trust dial, in one place: what this project has <em>earned</em> from its
          verification track record, how you tighten it, which paths always escalate,
          and what enforcement does when the bar isn't met.
        </p>
        {#if projTrust}
          <div class="trust-readout" title={projTrust.explanation}>
            <span class="tr-tier">Earned trust: <strong>{projTrust.tier}</strong></span>
            <span class="tr-meta">cleanly verified {projTrust.clean}/{projTrust.total} · auto-clearing below {projTrustEscalate}, escalating {projTrustEscalate}+ to Needs review</span>
          </div>
        {/if}

        <label class="field column">
          <span class="lbl">Always-review zones</span>
          <textarea
            class="mimes"
            rows="4"
            placeholder={'auth\nmigrations\nbilling'}
            bind:value={zonesText}
            disabled={saving}
            spellcheck="false"
          ></textarea>
          <span class="status">
            One path pattern per line. A change touching any of these (auth, billing,
            migrations…) always escalates for human review in the dashboard's review
            queue, regardless of its risk score. Plain names match a path segment;
            globs (e.g. <code>*.lock</code>) are supported.
          </span>
        </label>

        <label class="field column">
          <span class="lbl">Minimum review level</span>
          <select class="cmd" bind:value={tightenTo} disabled={saving}>
            <option value="">Automatic (earned trust)</option>
            <option value="Low">Low — review everything</option>
            <option value="Medium">Medium and above</option>
            <option value="High">High and above</option>
            <option value="Critical">Critical only</option>
          </select>
          <span class="status">
            The box sets an earned threshold from your verification track record. You
            can <em>tighten</em> it here (review more), but not loosen below what the
            machine has earned — and Critical changes + always-review zones always
            escalate.
          </span>
        </label>

        <h3 class="sub-h">Enforcement</h3>
        <p class="hint">
          Enforcement levels turn advisory checks into real ones (glass-box
          Phase 6). <em>off</em> ignores · <em>flag</em> is advisory (today's
          behavior) · <em>gate</em> blocks the action · <em>hard-block</em> is the
          strictest.
          {#if !govLicensed}
            <strong> Levels above “flag” require a Team/Enterprise license — until
            then they stay advisory.</strong>
          {/if}
        </p>
        <label class="field column">
          <span class="lbl">Verification</span>
          <select class="cmd" bind:value={govVerification} disabled={saving}>
            <option value="off">Off — ignore verification</option>
            <option value="flag">Flag — advisory (default)</option>
            <option value="gate">Gate — block completing an item while verification is failing</option>
            <option value="hard_block">Hard-block — require a passing verification before completion</option>
          </select>
          <span class="status">
            Applies when marking todos/bugs/use-cases done. {govLicensed ? 'Enforced.' : 'Advisory until licensed.'}
          </span>
        </label>
        <label class="field column">
          <span class="lbl">Human review</span>
          <select class="cmd" bind:value={govReview} disabled={saving}>
            <option value="off">Off</option>
            <option value="flag">Flag — advisory (default)</option>
            <option value="gate">Gate — block completing an item a reviewer rejected</option>
            <option value="hard_block">Hard-block — require a human approval before completion</option>
          </select>
          <span class="status">
            The oversight floor: the human-review decision becomes a real gate.
            {govLicensed ? 'Enforced.' : 'Advisory until licensed.'}
          </span>
        </label>
        <label class="field column">
          <span class="lbl">Architecture integrity</span>
          <select class="cmd" bind:value={govArchitecture} disabled={saving}>
            <option value="off">Off</option>
            <option value="flag">Flag — advisory (default)</option>
            <option value="gate">Gate — block completion while the diagram has a component with no code</option>
            <option value="hard_block">Hard-block — same, strictest</option>
          </select>
          <span class="status">
            Won't let work pile on a diagram that's provably lying: a ratified
            node whose area maps to no code (red in the Architecture panel) blocks
            completion. {govLicensed ? 'Enforced.' : 'Advisory until licensed.'}
          </span>
        </label>

        <label class="field column">
          <span class="lbl">Security findings</span>
          <select class="cmd" bind:value={govSecurity} disabled={saving}>
            <option value="off">Off</option>
            <option value="flag">Flag — advisory (default)</option>
            <option value="gate">Gate — block completion while a high-severity security finding is unresolved</option>
            <option value="hard_block">Hard-block — same, strictest</option>
          </select>
          <span class="status">
            Holds the bar: while Code Analysis has a live high-severity (CVSS ≥ 7)
            security finding anywhere in the project, no item can be completed —
            fix it or dismiss it with justification. {govLicensed ? 'Enforced.' : 'Advisory until licensed.'}
          </span>
        </label>
      </section>
      {/if}

      {#if activeTab === 'analysis'}
      <section class="section">
        <h3>Analysis pipeline</h3>
        <p class="hint">
          The baseline Scan button is free. These connect your CI / security
          tools' findings and route them into work — an Enterprise feature.
        </p>
        <label class="field column">
          <span class="lbl">Watched directory</span>
          <input class="cmd" type="text" bind:value={watchDir} disabled={saving}
            placeholder="/path/ci-drops-sarif-here" />
          <span class="status">
            SARIF files dropped in this folder are ingested automatically (polled
            ~30s). Leave blank to disable.
          </span>
        </label>
        <label class="field column">
          <span class="lbl">Auto-route findings to scratchpad</span>
          <input class="cmd" type="text" bind:value={routeTarget} disabled={saving}
            placeholder="Issues" />
          <span class="status">
            Name of a scratchpad in this project. Findings at/above the severity
            below become items there (once each). Leave blank to disable.
          </span>
        </label>
        <label class="field column">
          <span class="lbl">Auto-route minimum severity</span>
          <select class="cmd" bind:value={routeMinSev} disabled={saving}>
            <option value="high">High only (default)</option>
            <option value="medium">Medium and above</option>
            <option value="low">Low and above</option>
          </select>
        </label>
      </section>
      {/if}

      {#if loadFailed}
        <div class="err">Some settings couldn’t be loaded (a server or network error). Saving is disabled so existing values aren’t overwritten — <button class="link-btn" onclick={load}>reload</button> to try again.</div>
      {/if}
      {#if err}
        <div class="err">{err}</div>
      {/if}
      {#if saveMsg}
        <div class="ok">{saveMsg}</div>
      {/if}
    {/if}
  {/snippet}
  {#snippet footer()}
    <button class="btn ghost" onclick={resetToDefaults} disabled={saving || loading}>
      Reset to defaults
    </button>
    <div class="grow"></div>
    <button class="btn ghost" onclick={onClose} disabled={saving}>Cancel</button>
    <button class="btn primary" onclick={save} disabled={saving || loading || loadFailed}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .muted { color: var(--p-888888); font-size: 12px; padding: 20px; text-align: center; }
  .trust-readout {
    display: flex; flex-direction: column; gap: 2px; margin: 0 0 14px;
    background: var(--p-16222e); border: 1px solid var(--p-2d5578); border-radius: 6px;
    padding: 8px 12px;
  }
  .tr-tier { font-size: 13px; color: var(--p-cfe6ff); }
  .tr-meta { font-size: 11px; color: var(--p-7d97b0); }
  .sub-h { margin-top: 18px; }
  .settings-tabs {
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--p-3a3a3a);
    margin-bottom: 16px;
  }
  .settings-tab {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--p-888888);
    padding: 6px 12px;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }
  .settings-tab:hover { color: var(--p-cccccc); }
  .settings-tab.active { color: var(--p-e0e0e0); border-bottom-color: var(--p-99ccff); }
  .section { display: flex; flex-direction: column; gap: 14px; }
  .section h3 {
    margin: 0;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-aaaaaa);
  }
  .hint {
    margin: 0;
    font-size: 11px;
    color: var(--p-888888);
    line-height: 1.5;
  }
  .hint em { color: var(--p-aaaaaa); font-style: normal; }
  .detected {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 12px;
    font-size: 11px;
    color: var(--p-888888);
  }
  .detected strong { color: var(--p-99ccff); font-weight: 600; }
  .link-btn {
    background: none;
    border: none;
    padding: 0;
    color: var(--p-99ccff);
    font-size: 11px;
    cursor: pointer;
    text-decoration: underline;
  }
  .link-btn:disabled { color: var(--p-555555); cursor: default; text-decoration: none; }
  .field {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 10px;
  }
  .field.column {
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .lbl {
    color: var(--p-cccccc);
    font-size: 12px;
  }
  .num {
    background: var(--p-0c0c0c);
    color: var(--p-dddddd);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 4px 8px;
    font-size: 13px;
    width: 110px;
    font-variant-numeric: tabular-nums;
  }
  .num:focus { outline: none; border-color: var(--p-2d5578); }
  .mimes {
    background: var(--p-0c0c0c);
    color: var(--p-dddddd);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 6px 8px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    resize: vertical;
  }
  .mimes:focus { outline: none; border-color: var(--p-2d5578); }
  .cmd {
    background: var(--p-0c0c0c);
    color: var(--p-dddddd);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 5px 8px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    width: 100%;
    box-sizing: border-box;
  }
  .cmd:focus { outline: none; border-color: var(--p-2d5578); }
  .toggle { display: flex; align-items: flex-start; gap: 8px; cursor: pointer; }
  .toggle input { margin-top: 2px; flex: none; }
  .toggle .lbl { display: block; }
  .toggle .status { display: block; margin-top: 2px; }
  .field.indent { margin-left: 24px; }
  select.cmd { cursor: pointer; }
  .status {
    color: var(--p-777777);
    font-size: 10px;
    font-style: italic;
  }
  .err {
    margin-top: 12px;
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 6px 10px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
  }
  .ok {
    margin-top: 12px;
    color: var(--p-99eebb);
    font-size: 12px;
    padding: 6px 10px;
    background: var(--p-142a1a);
    border: 1px solid var(--p-205020);
    border-radius: 3px;
  }
  .btn {
    padding: 5px 14px;
    font-size: 12px;
    border-radius: 3px;
    cursor: pointer;
    border: 1px solid var(--p-333333);
    background: var(--p-1a1a1a);
    color: var(--p-cccccc);
  }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn.ghost:hover:not(:disabled) { background: var(--p-262626); color: var(--p-ffffff); }
  .btn.primary {
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .grow { flex: 1; }
</style>
