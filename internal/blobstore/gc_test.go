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
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubLiveLister implements LiveBlobLister with a hardcoded list —
// drives the watcher tests without pulling in a real Storage.
// inlineShas covers the composite-doc inline blob refs that the
// GCWatcher unions with the column-resident set.
type stubLiveLister struct {
	shas        []string
	inlineShas  []string
	err         error
	inlineErr   error
}

func (s stubLiveLister) ListLiveBlobShas(context.Context) ([]string, error) {
	return s.shas, s.err
}

func (s stubLiveLister) ListCompositeImageShas(context.Context) ([]string, error) {
	return s.inlineShas, s.inlineErr
}

// backdate sets the file's mtime so the minAge filter triggers.
func backdate(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

func TestLocalStoreGCRemovesOldOrphans(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Three blobs: A is referenced, B is an old orphan, C is a young orphan.
	shaA, _, err := s.Put(ctx, bytes.NewReader([]byte("live blob")))
	if err != nil {
		t.Fatalf("put A: %v", err)
	}
	shaB, _, err := s.Put(ctx, bytes.NewReader([]byte("old orphan")))
	if err != nil {
		t.Fatalf("put B: %v", err)
	}
	shaC, _, err := s.Put(ctx, bytes.NewReader([]byte("young orphan")))
	if err != nil {
		t.Fatalf("put C: %v", err)
	}

	// Backdate A and B to 48h ago; leave C fresh.
	backdate(t, s.finalPath(shaA), 48*time.Hour)
	backdate(t, s.finalPath(shaB), 48*time.Hour)

	// Live set contains only A. minAge = 24h.
	removed, err := s.GC(ctx, []string{shaA}, 24*time.Hour)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (only B should be swept)", removed)
	}

	// A still present (live).
	if _, err := os.Stat(s.finalPath(shaA)); err != nil {
		t.Errorf("A removed (should be live): %v", err)
	}
	// B gone (old + orphan).
	if _, err := os.Stat(s.finalPath(shaB)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("B still present (should be gone): %v", err)
	}
	// C still present (young; minAge protected it).
	if _, err := os.Stat(s.finalPath(shaC)); err != nil {
		t.Errorf("C removed (should be protected by minAge): %v", err)
	}
}

func TestLocalStoreGCSkipsTmpDirAndUnknownFiles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Drop a stray temp + a non-sha-named file under the root.
	tmpStray := filepath.Join(s.root, ".tmp", "blob-leftover.dat")
	if err := os.WriteFile(tmpStray, []byte("orphan temp"), 0o644); err != nil {
		t.Fatalf("write stray temp: %v", err)
	}
	backdate(t, tmpStray, 48*time.Hour)

	// Non-sha file under root.
	stranger := filepath.Join(s.root, "README.txt")
	if err := os.WriteFile(stranger, []byte("not a blob"), 0o644); err != nil {
		t.Fatalf("write stranger: %v", err)
	}
	backdate(t, stranger, 48*time.Hour)

	removed, err := s.GC(ctx, nil, 24*time.Hour)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 (only non-blob files exist)", removed)
	}

	// Both files still present — sweeper must not touch them.
	if _, err := os.Stat(tmpStray); err != nil {
		t.Errorf("stray temp removed: %v", err)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("stranger removed: %v", err)
	}
}

// TestLocalStoreGCDoesNotFollowSymlinkOutOfRoot plants a sha-named
// orphan that is actually a symlink pointing at a file OUTSIDE the
// store. The sweeper must unlink the symlink itself but never delete
// (or follow it to delete) the out-of-root target. Removals run through
// an os.Root handle, which cannot escape s.root (symlink TOCTOU / G122).
func TestLocalStoreGCDoesNotFollowSymlinkOutOfRoot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A victim file living OUTSIDE the store root.
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("must survive"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	// Plant a sha-named symlink at the sharded blob path, pointing at the
	// out-of-root victim. To the sweeper it looks like an ordinary orphan.
	sha := strings.Repeat("a", 64)
	link := s.finalPath(sha)
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatalf("mkdir shard: %v", err)
	}
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Negative minAge → cutoff in the future, so the (fresh) symlink is
	// age-eligible without depending on lchtimes to backdate a symlink.
	removed, err := s.GC(ctx, nil, -time.Hour)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (the orphan symlink)", removed)
	}

	// The symlink itself is gone...
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan symlink still present: %v", err)
	}
	// ...but the out-of-root target must be untouched.
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("victim outside root was deleted: %v", err)
	}
}

func TestLocalStoreGCAllOrphansAllOld(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Three orphans, all old. Empty live set → all three removed.
	var shas []string
	for i, body := range []string{"one", "two", "three"} {
		sha, _, err := s.Put(ctx, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
		shas = append(shas, sha)
		backdate(t, s.finalPath(sha), 48*time.Hour)
	}
	removed, err := s.GC(ctx, nil, 24*time.Hour)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}
	for _, sha := range shas {
		if _, err := os.Stat(s.finalPath(sha)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still present", sha)
		}
	}
}

func TestLocalStoreGCEmptyStore(t *testing.T) {
	s := newTestStore(t)
	removed, err := s.GC(context.Background(), nil, time.Hour)
	if err != nil {
		t.Fatalf("GC empty: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

func TestNewGCWatcherDefaults(t *testing.T) {
	s := newTestStore(t)
	w := NewGCWatcher(stubLiveLister{}, s, 0, 0, nil)
	if w.interval != time.Hour {
		t.Errorf("default interval = %v, want 1h", w.interval)
	}
	if w.minAge != 24*time.Hour {
		t.Errorf("default minAge = %v, want 24h", w.minAge)
	}
}

func TestGCWatcherTickSweeps(t *testing.T) {
	// Drives one tick directly (without spinning up Run) to confirm
	// the watcher plumbs the live-lister + GC call correctly.
	s := newTestStore(t)
	ctx := context.Background()

	keep, _, _ := s.Put(ctx, bytes.NewReader([]byte("keep me")))
	drop, _, _ := s.Put(ctx, bytes.NewReader([]byte("drop me")))
	backdate(t, s.finalPath(keep), 48*time.Hour)
	backdate(t, s.finalPath(drop), 48*time.Hour)

	w := NewGCWatcher(stubLiveLister{shas: []string{keep}}, s, time.Hour, 24*time.Hour, nil)
	w.tick(ctx)

	if _, err := os.Stat(s.finalPath(keep)); err != nil {
		t.Errorf("live blob removed: %v", err)
	}
	if _, err := os.Stat(s.finalPath(drop)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan blob still present: %v", err)
	}
}

// TestGCWatcherProtectsInlineCompositeShas confirms that shas
// referenced inline in composite content (returned by
// ListCompositeImageShas) are NOT swept even though they don't
// appear in any column-resident set. Real-world: a sketch preview
// would be in ListLiveBlobShas; an image pasted into a composite
// doc only shows up via the markdown ref.
func TestGCWatcherProtectsInlineCompositeShas(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	keepInline, _, _ := s.Put(ctx, bytes.NewReader([]byte("inline image")))
	dropOrphan, _, _ := s.Put(ctx, bytes.NewReader([]byte("not referenced")))
	backdate(t, s.finalPath(keepInline), 48*time.Hour)
	backdate(t, s.finalPath(dropOrphan), 48*time.Hour)

	w := NewGCWatcher(stubLiveLister{
		shas:       []string{}, // nothing in columns
		inlineShas: []string{keepInline},
	}, s, time.Hour, 24*time.Hour, nil)
	w.tick(ctx)

	if _, err := os.Stat(s.finalPath(keepInline)); err != nil {
		t.Errorf("inline-composite sha was swept: %v", err)
	}
	if _, err := os.Stat(s.finalPath(dropOrphan)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan still present: %v", err)
	}
}

// TestGCWatcherBailsOnCompositeListError verifies the safety
// guard: a failure listing inline shas would risk sweeping
// composite images, so the tick aborts entirely rather than
// proceed with a partial set.
func TestGCWatcherBailsOnCompositeListError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at, _, _ := s.Put(ctx, bytes.NewReader([]byte("at risk")))
	backdate(t, s.finalPath(at), 48*time.Hour)

	w := NewGCWatcher(stubLiveLister{
		shas:      []string{},
		inlineErr: errors.New("simulated"),
	}, s, time.Hour, 24*time.Hour, nil)
	w.tick(ctx)

	if _, err := os.Stat(s.finalPath(at)); err != nil {
		t.Errorf("blob removed despite tick failure: %v", err)
	}
}
