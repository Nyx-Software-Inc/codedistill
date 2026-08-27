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
  // The closing workflow, ON the item (Rich's beef #2: review isn't a place
  // you go, it's part of finishing). Shown when an item sits in its agent-done
  // state — but sign-off only appears AFTER verification has run: you judge the
  // evidence, so there must be evidence. If checks are configured but haven't
  // run, the bar prompts to run them; if no checks are configured, sign-off is
  // a pure human judgment and is offered directly.
  import * as api from '../lib/api';
  import type { VerificationResponse, VerificationResult } from '../lib/types';
  import { alertDialog, promptDialog } from '../lib/dialog';

  type Props = {
    ownerType: 'todo_item' | 'bug_item' | 'use_case_item';
    ownerId: string;
    status: string;
    onDecided: () => void;
  };
  let { ownerType, ownerId, status, onDecided }: Props = $props();

  // Agent-done, not human-terminal: the states where sign-off is the next step.
  const AWAITING: Record<string, string[]> = {
    todo_item: ['complete'],
    bug_item: ['fixed'],
    use_case_item: ['completed'],
  };
  const show = $derived(AWAITING[ownerType]?.includes(status) ?? false);

  // Verification gate: load the item's verification state whenever the bar
  // becomes relevant (and after a run completes) so we know whether there's
  // evidence to sign off against.
  let vr = $state<VerificationResponse | null>(null);
  let hasCommit = $state(false);
  // The item's persisted decision. A prior approval means "already signed off":
  // the bar must render that state instead of re-prompting on every mount (Rich:
  // "every time I open it, it asks for approval"). Consistent with the review
  // queue, which also treats an approved decision as cleared.
  let prior = $state<api.ReviewDecision | null>(null);
  let loadedKey = $state('');
  let running = $state(false);
  let triggering = $state(false);
  let runErr = $state('');
  $effect(() => {
    if (show && loadedKey !== ownerId) {
      loadedKey = ownerId;
      void loadVerification();
    }
    if (!show) loadedKey = '';
  });
  async function loadVerification() {
    try {
      const [v, anchors, decision] = await Promise.all([
        api.getVerification(ownerType, ownerId),
        api.listCodeAnchors(ownerType, ownerId),
        api.getLatestReview(ownerType, ownerId),
      ]);
      vr = v;
      // A recorded commit is required to verify. Without one there's no
      // implementation to check — the item was marked done but its throughline
      // is broken (or it never had code, e.g. a hand-closed item).
      hasCommit = (anchors ?? []).some((a) => a.kind === 'commit' && !!a.revision);
      // Only an approval short-circuits to "signed off". A stale 'rejected' would
      // have reopened the item (bar hidden); if the item is terminal again after
      // rework, a fresh decision is needed, so don't honor an old rejection.
      prior = decision?.decision === 'approved' ? decision : null;
      running = (vr?.results ?? []).some((r) => r.verdict === 'running');
      if (running) setTimeout(() => { if (show) void loadVerification(); }, 2500);
    } catch {
      vr = null;
    }
  }

  // Only the LATEST result per check counts — a re-run supersedes the old
  // verdict. Judging every historical row means a stale fail (e.g. a check
  // that failed once and passes now) wrongly reads as failing. Results arrive
  // newest-first, so the first occurrence of each check name is the latest.
  const latestPerCheck = $derived.by(() => {
    const m = new Map<string, VerificationResult>();
    for (const r of vr?.results ?? []) if (!m.has(r.check_name)) m.set(r.check_name, r);
    return [...m.values()];
  });

  // Derived gate states.
  const checksConfigured = $derived((vr?.checks?.length ?? 0) > 0 && (vr?.available ?? false));
  const hasResults = $derived(latestPerCheck.length > 0);
  const failing = $derived(latestPerCheck.some((r) => r.verdict === 'fail' || r.verdict === 'error'));
  // Verification is only offerable when there's actually a commit to run it
  // against. No commit → no evidence path; sign off on judgment (or reopen).
  const verifiable = $derived(checksConfigured && hasCommit);
  // Sign-off is offered when there's evidence to judge — verification has run —
  // OR when it can't run here (no checks, or no commit): human judgment only.
  const canSignOff = $derived(!verifiable || (hasResults && !running));

  async function runVerification(useHead = false) {
    if (triggering || running) return;
    triggering = true;
    runErr = '';
    try {
      await api.triggerVerification(ownerType, ownerId, useHead);
      running = true;
      setTimeout(() => { if (show) void loadVerification(); }, 1500);
    } catch (e) {
      // Expected condition — surface inline in the bar, not a blocking dialog.
      runErr = String(e);
    } finally {
      triggering = false;
    }
  }

  let busy = $state(false);
  let done = $state<'' | 'approved' | 'rejected'>('');
  // Signed off already (persisted) OR just decided in this session.
  const signedOff = $derived(done === 'approved' || (done === '' && prior !== null));
  async function decide(decision: 'approved' | 'rejected') {
    if (busy) return;
    let note = '';
    if (decision === 'rejected') {
      const n = await promptDialog('Why does this need rework? (optional — lands on the item and in trust evidence). The item reopens for rework.', {
        title: 'Needs rework',
        confirmLabel: 'Reject & reopen',
        danger: true,
      });
      if (n === null) return;
      note = n;
    }
    busy = true;
    try {
      await api.recordReview(ownerType, ownerId, decision, note);
      done = decision;
      onDecided();
    } catch (e) {
      void alertDialog(`Review failed: ${e}`);
    } finally {
      busy = false;
    }
  }
</script>

{#if show}
  <div class="review-bar" class:decided={signedOff} class:warn={canSignOff && failing && !signedOff}>
    {#if signedOff}
      <span class="msg ok">✓ Signed off{prior?.created_at ? ` on ${new Date(prior.created_at).toLocaleDateString()}` : ''} — recorded in the Log and as trust evidence.</span>
    {:else if done === 'rejected'}
      <span class="msg bad">✗ Sent back for rework — the item is reopened, recorded in the Log.</span>
    {:else if running}
      <span class="msg">⏳ Verification running… sign-off opens when it finishes.</span>
    {:else if !canSignOff}
      <!-- Checks are configured AND a commit exists, but no run yet. -->
      <span class="msg">
        Marked done — <strong>run verification before signing off</strong>. Your checks
        run in isolation at the recorded commit; you sign off against the result.
        {#if runErr}<span class="inline-err">— {runErr}</span>{/if}
      </span>
      <span class="btns">
        <button class="run" disabled={triggering} onclick={() => runVerification(false)}>
          {triggering ? 'Starting…' : '▶ Run verification'}
        </button>
      </span>
    {:else}
      <span class="msg" class:bad={failing}>
        {#if failing}
          ⚠ <strong>Verification is failing.</strong> Review the red checks — approving over red is on the record.
        {:else if verifiable}
          ✓ Verification passed — <strong>your sign-off closes it</strong> and feeds earned trust.
        {:else if checksConfigured && !hasCommit}
          <strong>No implementation commit is recorded</strong>, so there's nothing to check out "as built". Run the checks against the <strong>current commit</strong> (may have drifted since this was built), or sign off on judgment.
          {#if runErr}<span class="inline-err">— {runErr}</span>{/if}
        {:else}
          Marked done — <strong>your sign-off closes it</strong> (no automated checks configured; human judgment).
        {/if}
      </span>
      <span class="btns">
        {#if checksConfigured && !hasCommit}
          <button class="run" disabled={triggering} onclick={() => runVerification(true)}
            title="Run the project's checks against the current commit (HEAD)">
            {triggering ? 'Starting…' : '▶ Run against current commit'}
          </button>
        {/if}
        <button class="ok" disabled={busy} onclick={() => decide('approved')}>✓ Approve</button>
        <button class="bad" disabled={busy} onclick={() => decide('rejected')}>✗ Needs rework</button>
      </span>
    {/if}
  </div>
{/if}

<style>
  .review-bar {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    background: var(--p-16222e); border: 1px solid var(--p-2d5578); border-radius: 6px;
    padding: 8px 12px; margin: 0 0 12px;
  }
  .review-bar.decided { background: var(--p-121a12); border-color: var(--p-2c5a2c); }
  .review-bar.warn { background: var(--p-241414); border-color: var(--p-553030); }
  .msg { font-size: 12px; color: var(--p-cfe6ff); flex: 1; min-width: 200px; }
  .msg.ok { color: var(--p-a9d5a9); }
  .msg.bad { color: var(--p-e0a0a0); }
  .btns { display: inline-flex; gap: 6px; }
  .btns button { font-size: 12px; padding: 4px 12px; border-radius: 4px; cursor: pointer; }
  .btns .ok { background: var(--p-13210f); color: var(--p-a9d5a9); border: 1px solid var(--p-2c5a2c); }
  .btns .ok:hover:not(:disabled) { background: var(--p-2c5a2c); }
  .btns .bad { background: var(--p-2a1414); color: var(--p-e0a0a0); border: 1px solid var(--p-553030); }
  .btns .bad:hover:not(:disabled) { background: var(--p-553030); }
  .btns .run { background: var(--p-1a2530); color: var(--p-99ccff); border: 1px solid var(--p-2d5578); }
  .btns .run:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
</style>
