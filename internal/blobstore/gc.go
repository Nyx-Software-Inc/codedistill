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

package blobstore

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// LiveBlobLister is the subset of storage.Storage that the GC sweeper
// needs. ListLiveBlobShas returns shas referenced via dedicated
// columns (blob_sha for image/file items, og_image_sha for link
// items). ListCompositeImageShas returns shas extracted from inline
// markdown refs inside composite content. Both are unioned by the
// watcher before sweeping — anything not in the merged set AND
// older than minAge is eligible for deletion.
//
// Defined here as an interface so this package doesn't import
// internal/storage and the dependency arrow stays storage →
// blobstore, not both ways.
type LiveBlobLister interface {
	ListLiveBlobShas(ctx context.Context) ([]string, error)
	ListCompositeImageShas(ctx context.Context) ([]string, error)
}

// blobSHAFileName is what a content-addressed blob filename must look
// like: 64 lowercase hex chars. Anything else under the store root is
// foreign (a stale temp, an external file someone dropped in) and is
// left alone by the sweeper.
var blobSHAFileName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GC walks the store root and removes any blob file whose name (the
// sha) is NOT in `live` AND whose mtime is older than `minAge`.
// Returns the count removed. The minAge guard protects in-flight
// uploads from a "write blob, GC fires before item row commits"
// race — keep it generous (24h is the production default).
//
// Files in the `.tmp` subdir are always skipped. Files whose name
// doesn't match the sha shape are left alone (defensive: if a user
// or operator dropped something into the blob dir, we don't want
// the sweeper to take it).
//
// Walk errors on individual entries are absorbed — the sweeper is
// best-effort and the next tick can retry. A failure to traverse a
// whole directory propagates so the caller can log it.
func (s *LocalStore) GC(ctx context.Context, live []string, minAge time.Duration) (int, error) {
	liveSet := make(map[string]struct{}, len(live))
	for _, sha := range live {
		liveSet[sha] = struct{}{}
	}
	cutoff := time.Now().Add(-minAge)
	tmpPrefix := filepath.Join(s.root, ".tmp") + string(filepath.Separator)

	// Deletions go through a root-scoped handle so a symlink swapped into
	// the store between the walk's lstat and the unlink can't redirect the
	// Remove outside s.root (symlink TOCTOU / CWE-367). os.Root.Remove
	// refuses to traverse a symlink in any intermediate path component.
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	removed := 0
	err = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Individual-entry permission errors etc. shouldn't kill
			// the whole walk. Return the error for the entry but log
			// at the caller; WalkDir will continue with siblings.
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if path == tmpPrefix || (len(path) > len(tmpPrefix) && path[:len(tmpPrefix)] == tmpPrefix) {
			return nil
		}
		name := d.Name()
		if !blobSHAFileName.MatchString(name) {
			return nil
		}
		if _, alive := liveSet[name]; alive {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		if err := root.Remove(rel); err == nil {
			removed++
		}
		return ctx.Err()
	})
	return removed, err
}

// GCWatcher runs LocalStore.GC on a periodic tick. Built like the
// matcher / codeindex watchers — interval-driven, cancel-on-ctx,
// independent of HTTP request lifetimes.
type GCWatcher struct {
	store    LiveBlobLister
	blobs    *LocalStore
	interval time.Duration
	minAge   time.Duration
	log      *slog.Logger
}

// NewGCWatcher constructs a sweeper. interval=0 → 1h; minAge=0 → 24h.
// Both defaults match the design doc (rich-canvas Slice 1, task #15).
// Does no work until Run is called.
func NewGCWatcher(store LiveBlobLister, blobs *LocalStore, interval, minAge time.Duration, log *slog.Logger) *GCWatcher {
	if interval <= 0 {
		interval = time.Hour
	}
	if minAge <= 0 {
		minAge = 24 * time.Hour
	}
	if log == nil {
		log = slog.Default()
	}
	return &GCWatcher{store: store, blobs: blobs, interval: interval, minAge: minAge, log: log}
}

// Run blocks until ctx is cancelled. First tick is delayed by one
// interval so the process can finish boot before paying for an FS
// walk + DB read. Mirrors the matcher watcher's startup behavior.
func (w *GCWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.interval):
			w.tick(ctx)
		}
	}
}

func (w *GCWatcher) tick(ctx context.Context) {
	columnShas, err := w.store.ListLiveBlobShas(ctx)
	if err != nil {
		w.log.Warn("blob GC: list column shas failed", "err", err)
		return
	}
	inlineShas, err := w.store.ListCompositeImageShas(ctx)
	if err != nil {
		// Failing here would orphan-sweep composite inline images.
		// Bail out of this tick rather than risk that — the next
		// tick (1h later) will retry.
		w.log.Warn("blob GC: list composite shas failed; skipping tick", "err", err)
		return
	}
	live := mergeShaSets(columnShas, inlineShas)
	removed, err := w.blobs.GC(ctx, live, w.minAge)
	if err != nil {
		w.log.Warn("blob GC: sweep failed", "err", err, "removed", removed)
		return
	}
	if removed > 0 {
		w.log.Info("blob GC: swept orphans",
			"removed", removed,
			"live_count", len(live),
			"column_shas", len(columnShas),
			"inline_shas", len(inlineShas))
	}
}

// mergeShaSets de-dupes the union of two sha slices. Order
// unspecified.
func mergeShaSets(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}
