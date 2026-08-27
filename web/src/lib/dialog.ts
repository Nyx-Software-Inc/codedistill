// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

// Themed replacements for the native alert()/confirm()/prompt() popups —
// promise-based, rendered by the single <DialogHost/> mounted in App.
// Requests queue FIFO, so overlapping dialogs show one at a time instead of
// stacking browser-chrome boxes.
import { writable } from 'svelte/store';

export type DialogKind = 'alert' | 'confirm' | 'prompt';

export type DialogRequest = {
  id: number;
  kind: DialogKind;
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  danger: boolean;
  initial: string; // prompt only
  placeholder: string; // prompt only
  // alert resolves undefined; confirm resolves boolean; prompt resolves string|null.
  resolve: (v: never) => void;
};

export const dialogQueue = writable<DialogRequest[]>([]);

type CommonOpts = { title?: string; confirmLabel?: string; cancelLabel?: string; danger?: boolean };

let nextID = 1;

// Messages just dismissed by the user, with the time they were dismissed. A
// re-fire of the SAME alert within a short window is suppressed — so a caller
// stuck in a reactive loop (re-throwing the same error every tick) can't trap
// the user in an un-dismissable dialog no matter what. This is the structural
// guarantee: an alert you closed stays closed.
const recentlyDismissed = new Map<string, number>();
const REFIRE_SUPPRESS_MS = 800;

function push(req: DialogRequest) {
  dialogQueue.update((q) => {
    if (req.kind === 'alert') {
      // Already showing (or queued) — don't stack a twin.
      if (q.some((r) => r.kind === 'alert' && r.message === req.message)) {
        (req.resolve as (v: unknown) => void)(undefined);
        return q;
      }
      // Just dismissed and immediately re-fired — drop it (loop guard).
      const at = recentlyDismissed.get(req.message);
      if (at !== undefined && perfNow() - at < REFIRE_SUPPRESS_MS) {
        (req.resolve as (v: unknown) => void)(undefined);
        return q;
      }
    }
    return [...q, req];
  });
}

// Monotonic-ish clock without tripping the Date.now ban in workflow scripts;
// in the browser performance.now is always available.
function perfNow(): number {
  return typeof performance !== 'undefined' ? performance.now() : 0;
}

/** Styled alert(). Fire-and-forget is fine: `void alertDialog('...')`. */
export function alertDialog(message: string, opts: CommonOpts = {}): Promise<void> {
  return new Promise((resolve) => {
    push({
      id: nextID++,
      kind: 'alert',
      title: opts.title ?? 'Notice',
      message,
      confirmLabel: opts.confirmLabel ?? 'OK',
      cancelLabel: '',
      danger: opts.danger ?? false,
      initial: '',
      placeholder: '',
      resolve: resolve as DialogRequest['resolve'],
    });
  });
}

/** Styled confirm(): resolves true (confirm) / false (cancel, Esc, backdrop). */
export function confirmDialog(message: string, opts: CommonOpts = {}): Promise<boolean> {
  return new Promise((resolve) => {
    push({
      id: nextID++,
      kind: 'confirm',
      title: opts.title ?? 'Confirm',
      message,
      confirmLabel: opts.confirmLabel ?? 'OK',
      cancelLabel: opts.cancelLabel ?? 'Cancel',
      danger: opts.danger ?? false,
      initial: '',
      placeholder: '',
      resolve: resolve as DialogRequest['resolve'],
    });
  });
}

/** Styled prompt(): resolves the entered string, or null on cancel. */
export function promptDialog(
  message: string,
  opts: CommonOpts & { initial?: string; placeholder?: string } = {},
): Promise<string | null> {
  return new Promise((resolve) => {
    push({
      id: nextID++,
      kind: 'prompt',
      title: opts.title ?? 'Input',
      message,
      confirmLabel: opts.confirmLabel ?? 'OK',
      cancelLabel: opts.cancelLabel ?? 'Cancel',
      danger: opts.danger ?? false,
      initial: opts.initial ?? '',
      placeholder: opts.placeholder ?? '',
      resolve: resolve as DialogRequest['resolve'],
    });
  });
}

// A hard, unconditional escape: resolve EVERY queued dialog with its cancel
// value and empty the queue. Bound to Escape so no matter what re-queues a
// dialog — a reactive loop, a persisted drag offset, anything — the user can
// always get out. This is the last-resort guarantee behind the per-dialog
// buttons: one Escape clears the board.
export function flushDialogs() {
  dialogQueue.update((q) => {
    for (const r of q) {
      const v = r.kind === 'confirm' ? false : r.kind === 'prompt' ? null : undefined;
      try {
        (r.resolve as (v: unknown) => void)(v);
      } catch {
        /* a broken resolver must not block clearing the rest */
      }
      if (r.kind === 'alert') recentlyDismissed.set(r.message, perfNow());
    }
    return [];
  });
}

/** DialogHost calls this to finish the front-of-queue request. The queue
 * removal is unconditional (finally) and keyed by id with a drop-the-front
 * fallback — whatever else goes wrong, a click always dismisses a dialog. */
export function settleDialog(req: DialogRequest, value: unknown) {
  try {
    (req.resolve as (v: unknown) => void)(value);
  } finally {
    // Remember this alert's message so an immediate re-fire is suppressed
    // (the loop guard in push). Bound the map so it can't grow unbounded.
    if (req.kind === 'alert') {
      recentlyDismissed.set(req.message, perfNow());
      if (recentlyDismissed.size > 50) {
        const oldest = recentlyDismissed.keys().next().value;
        if (oldest !== undefined) recentlyDismissed.delete(oldest);
      }
    }
    dialogQueue.update((q) => {
      // Drop this request AND any other alert with the same message (a loop
      // may have queued several across ticks) — one dismiss clears them all.
      const before = q.length;
      const next = q.filter((r) => r.id !== req.id && !(r.kind === 'alert' && r.message === req.message));
      // Guarantee progress: if somehow nothing matched, drop the front.
      return next.length < before ? next : q.slice(1);
    });
  }
}
