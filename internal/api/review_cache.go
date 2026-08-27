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

package api

import (
	"context"
	"time"

	"codedistill/internal/git"
)

// The review queue is polled (the Needs-Review badge + the dashboard) and, per
// implemented item, spawned a git subprocess PER commit anchor plus a
// 1000-commit revert scan PER request — thousands of queries and dozens of
// process spawns per poll on a large project (audit H15). These caches remove
// the repeated git work:
//   - commit metrics are IMMUTABLE by SHA (a commit's churn never changes), so
//     they're cached indefinitely (soft-capped);
//   - the revert scan changes only when new commits land, so it's cached per
//     repo with a short TTL.
// The queue's live data (decisions, flags, verdicts) is NOT cached — only the
// slow/immutable git computations — so nothing the user acts on goes stale.

const (
	metricsCacheCap = 8192
	revertCacheTTL  = 30 * time.Second
)

type revertCacheEntry struct {
	reverted map[string]bool
	at       time.Time
}

// cachedCommitMetrics returns a commit's metrics, computing (and caching) it
// only on the first request for that SHA. Keyed by repoRoot+SHA so distinct
// repos can't collide.
func (s *Server) cachedCommitMetrics(ctx context.Context, repo *git.Repo, repoRoot, rev string) (git.CommitMetrics, error) {
	key := repoRoot + "\x00" + rev
	s.metricsMu.Lock()
	if m, ok := s.metricsCache[key]; ok {
		s.metricsMu.Unlock()
		return m, nil
	}
	s.metricsMu.Unlock()

	m, err := repo.CommitMetrics(ctx, rev)
	if err != nil {
		return m, err
	}
	s.metricsMu.Lock()
	if s.metricsCache == nil {
		s.metricsCache = map[string]git.CommitMetrics{}
	}
	if len(s.metricsCache) >= metricsCacheCap {
		s.metricsCache = map[string]git.CommitMetrics{} // simple flush; re-fills cheaply
	}
	s.metricsCache[key] = m
	s.metricsMu.Unlock()
	return m, nil
}

// cachedReverted returns the reverted-commit set for a repo, re-scanning at
// most once per revertCacheTTL rather than on every request.
func (s *Server) cachedReverted(repo *git.Repo, repoRoot string, now time.Time) map[string]bool {
	s.revertMu.Lock()
	if e, ok := s.revertCache[repoRoot]; ok && now.Sub(e.at) < revertCacheTTL {
		s.revertMu.Unlock()
		return e.reverted
	}
	s.revertMu.Unlock()

	reverted, _ := repo.RevertedCommits(1000)
	s.revertMu.Lock()
	if s.revertCache == nil {
		s.revertCache = map[string]revertCacheEntry{}
	}
	s.revertCache[repoRoot] = revertCacheEntry{reverted: reverted, at: now}
	s.revertMu.Unlock()
	return reverted
}
