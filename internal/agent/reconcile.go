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

package agent

import (
	"context"
	"time"
)

// maxFailedRetries caps how many times the sweep will auto-retry a `failed`
// item (per serve lifetime). Because the sweep is gated on model reachability,
// these are genuine failures, not outage failures — so after a few tries we stop
// hammering a capture the model can't classify. The manual "Reprocess" action
// (ReprocessNow) resets this, so the user always has the final say.
const maxFailedRetries = 5

// runSweeper does an immediate startup sweep, then sweeps every
// reconcileInterval until Stop/ctx cancellation. Launched only when
// WithReconcile set a positive interval.
func (a *Agent) runSweeper(ctx context.Context) {
	defer a.sweepWg.Done()
	a.sweep(ctx) // startup: rescue anything stranded across a restart
	t := time.NewTicker(a.reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.quit:
			return
		case <-t.C:
			a.sweep(ctx)
		}
	}
}

// sweep re-enqueues stranded items. It is gated on model reachability: when the
// model server is down it does nothing (the items stay put and are retried on a
// later sweep once the model is back), which keeps unprocessed items from
// churning to `failed` during an outage and preserves the failed-retry budget
// for genuine failures.
func (a *Agent) sweep(ctx context.Context) {
	if a.reachable != nil && !a.reachable(ctx) {
		a.log.Debug("classifier sweep skipped: model server unreachable")
		return
	}

	// Un-started work: never enqueued, or orphaned mid-flight (`processing`) by a
	// restart. Always safe to (re-)enqueue — dedup drops any that are genuinely
	// in-flight right now.
	if ids, err := a.store.ListScratchpadItemIDsByState(ctx, "unprocessed", "processing"); err != nil {
		a.log.Warn("classifier sweep: list unprocessed/processing failed", "err", err)
	} else {
		for _, id := range ids {
			a.sweepEnqueue(id)
		}
	}

	// Failed items: retry up to the cap. (Outage failures were avoided by the
	// reachability gate above, so a persistently-failing item here is a real
	// content problem — give up after maxFailedRetries.)
	failed, err := a.store.ListScratchpadItemIDsByState(ctx, "failed")
	if err != nil {
		a.log.Warn("classifier sweep: list failed failed", "err", err)
		return
	}
	for _, id := range failed {
		a.attemptsMu.Lock()
		n := a.attempts[id]
		if n >= maxFailedRetries {
			a.attemptsMu.Unlock()
			continue
		}
		a.attempts[id] = n + 1
		a.attemptsMu.Unlock()
		a.sweepEnqueue(id)
	}
}

// sweepEnqueue enqueues an item unless it's already queued/in-flight (dedup), and
// bails without enqueueing if the agent is stopping — so it can never send on a
// closed jobs channel.
func (a *Agent) sweepEnqueue(id string) {
	if _, loaded := a.inflight.LoadOrStore(id, struct{}{}); loaded {
		return // already queued or being processed
	}
	select {
	case a.jobs <- id:
	case <-a.quit:
		a.inflight.Delete(id)
	}
}

// ReprocessNow re-enqueues one item immediately, resetting its failed-retry
// count. Backs the per-item "Reprocess" button: an explicit user request bypasses
// the sweep's cap (the user gets the final say). No-op-safe to call when the item
// is already in-flight (dedup drops the duplicate at process time).
func (a *Agent) ReprocessNow(itemID string) {
	a.attemptsMu.Lock()
	delete(a.attempts, itemID)
	a.attemptsMu.Unlock()
	a.sweepEnqueue(itemID)
}
