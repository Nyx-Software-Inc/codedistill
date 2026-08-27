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
  import {
    type SubmitShortcut,
    getSubmitShortcut,
    setSubmitShortcut,
    loadSubmitShortcut,
  } from '../lib/submitShortcut';

  let current = $state<SubmitShortcut>('ctrl-enter');
  let loading = $state(true);

  onMount(async () => {
    await loadSubmitShortcut();
    current = getSubmitShortcut();
    loading = false;
  });

  function pick(v: SubmitShortcut) {
    current = v;
    setSubmitShortcut(v);
  }
</script>

<div class="input-settings">
  <p class="hint">
    Controls how multi-line composers (the new-item entry, the Ask
    panel) treat the Enter key.
  </p>

  <fieldset disabled={loading}>
    <legend class="sr-only">Submit shortcut</legend>

    <label class="opt">
      <input
        type="radio"
        name="submit-shortcut"
        value="ctrl-enter"
        checked={current === 'ctrl-enter'}
        onchange={() => pick('ctrl-enter')}
      />
      <div class="opt-body">
        <div class="opt-title">Ctrl+Enter to submit</div>
        <div class="opt-sub">
          Enter adds a newline. Ctrl+Enter (or Cmd+Enter on macOS) submits.
          Default.
        </div>
      </div>
    </label>

    <label class="opt">
      <input
        type="radio"
        name="submit-shortcut"
        value="enter"
        checked={current === 'enter'}
        onchange={() => pick('enter')}
      />
      <div class="opt-body">
        <div class="opt-title">Enter to submit</div>
        <div class="opt-sub">
          Plain Enter submits. Shift+Enter adds a newline. Ctrl+Enter still
          submits as a fallback so muscle memory doesn't lose your message.
        </div>
      </div>
    </label>
  </fieldset>
</div>

<style>
  .input-settings { display: flex; flex-direction: column; gap: 14px; }

  .hint {
    color: var(--p-888888);
    font-size: 12px;
    line-height: 1.4;
    margin: 0;
  }

  fieldset {
    border: 0;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .opt {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 10px 12px;
    border: 1px solid rgba(0, 0, 0, 0.08);
    border-radius: 8px;
    cursor: pointer;
  }
  .opt:hover { background: rgba(0, 0, 0, 0.02); }
  .opt input[type="radio"] {
    margin-top: 2px;
    flex-shrink: 0;
  }
  .opt-body { display: flex; flex-direction: column; gap: 2px; }
  .opt-title { font-size: 13px; font-weight: 500; }
  .opt-sub { color: var(--p-888888); font-size: 12px; line-height: 1.4; }

  .sr-only {
    position: absolute;
    width: 1px; height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    border: 0;
  }
</style>
