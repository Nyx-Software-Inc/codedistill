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

// The review comments of one changeset, as a host (the changeset pane, a diff
// tab) holds them: resolve the canonical changeset row once, then keep the
// comment list current through every edit. Each host owns one of these. Two
// hosts on the same changeset each hold a copy — the common case, since a
// file commented on in the pane is then opened in the code pane — so every
// mutation bumps a module-level version and every host reloads on it. One
// extra GET per edit, and no host ever shows a comment another just made as
// missing.
//
// DiffTable never calls the API. It gets the comments for its file and a
// ReviewActions object, and reports what the reviewer did.

import * as api from './api';
import type { ChangesetRow, CommentSide, ReviewComment } from './api';

export type CommentAnchor = { side: CommentSide; line_start: number; line_end: number };

/** What a diff table may do with comments; the host wires these to a ReviewThreads. */
export type ReviewActions = {
  add: (path: string, a: CommentAnchor, body: string) => Promise<void>;
  edit: (id: string, body: string) => Promise<void>;
  resolve: (id: string, resolved: boolean) => Promise<void>;
  remove: (id: string) => Promise<void>;
  promote: (id: string, type: 'bug' | 'todo') => Promise<void>;
  openItem: (ownerType: 'bug_item' | 'todo_item', ownerId: string) => void;
};

// Bumped after every successful mutation; hosts depend on it in the effect
// that loads their threads.
const bus = $state({ v: 0 });
export function reviewVersion(): number {
  return bus.v;
}
function bump(): void {
  bus.v++;
}

export class ReviewThreads {
  changeset = $state<ChangesetRow | null>(null);
  comments = $state<ReviewComment[]>([]);
  loading = $state(false);
  error = $state('');

  private projectId = '';
  private key = '';
  private seq = 0;

  /** Load (or reload) for base..head. A new pair clears first; the same
   *  pair refreshes in place, so a reload never blanks the threads. */
  async load(projectId: string, base: string, head: string, kind?: string, ref?: string): Promise<void> {
    const key = `${projectId}|${base}|${head}`;
    const my = ++this.seq;
    if (key !== this.key) {
      this.key = key;
      this.changeset = null;
      this.comments = [];
    }
    this.projectId = projectId;
    this.error = '';
    if (!projectId || !head) return;
    this.loading = true;
    try {
      const cs = await api.ensureChangeset(projectId, { base, head, kind, ref });
      if (my !== this.seq) return;
      const r = await api.listReviewComments(projectId, cs.id);
      if (my !== this.seq) return;
      this.changeset = r.changeset;
      this.comments = r.comments;
    } catch (e) {
      if (my !== this.seq) return;
      this.error = String(e);
    } finally {
      if (my === this.seq) this.loading = false;
    }
  }

  reset(): void {
    this.key = '';
    this.seq++;
    this.changeset = null;
    this.comments = [];
    this.error = '';
    this.loading = false;
  }

  forPath(path: string): ReviewComment[] {
    return this.comments.filter((c) => c.path === path);
  }

  countFor(path: string, unresolvedOnly = true): number {
    let n = 0;
    for (const c of this.comments) {
      if (c.path === path && (!unresolvedOnly || !c.resolved_at)) n++;
    }
    return n;
  }

  get unresolved(): number {
    return this.comments.filter((c) => !c.resolved_at).length;
  }

  private replace(c: ReviewComment): void {
    const i = this.comments.findIndex((x) => x.id === c.id);
    if (i >= 0) this.comments[i] = c;
    else this.comments = [...this.comments, c];
  }

  /** The actions a DiffTable needs, bound to this changeset. Errors are
   *  surfaced on `error` and rethrown so a composer can keep its draft. */
  actions(openItem: ReviewActions['openItem']): ReviewActions {
    const must = (): ChangesetRow => {
      if (!this.changeset) throw new Error('Comments are not loaded yet');
      return this.changeset;
    };
    const wrap = async <T>(f: () => Promise<T>): Promise<T> => {
      this.error = '';
      try {
        const r = await f();
        bump();
        return r;
      } catch (e) {
        this.error = String(e);
        throw e;
      }
    };
    return {
      add: (path, a, body) => wrap(async () => {
        const cs = must();
        const c = await api.createReviewComment(this.projectId, cs.id, { path, ...a, body });
        this.replace(c);
      }),
      edit: (id, body) => wrap(async () => {
        const c = await api.updateReviewComment(this.projectId, must().id, id, { body });
        this.replace(c);
      }),
      resolve: (id, resolved) => wrap(async () => {
        const c = await api.updateReviewComment(this.projectId, must().id, id, { resolved });
        this.replace(c);
      }),
      remove: (id) => wrap(async () => {
        await api.deleteReviewComment(this.projectId, must().id, id);
        this.comments = this.comments.filter((c) => c.id !== id);
      }),
      promote: (id, type) => wrap(async () => {
        const r = await api.promoteReviewComment(this.projectId, must().id, id, { type });
        this.replace(r.comment);
      }),
      openItem,
    };
  }
}
