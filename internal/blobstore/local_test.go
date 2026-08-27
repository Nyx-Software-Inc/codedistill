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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestStore returns a LocalStore rooted at a fresh tempdir
// (cleaned up by t.Cleanup) — every test gets isolation.
func newTestStore(t *testing.T) *LocalStore {
	t.Helper()
	root := t.TempDir()
	s, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func shaOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestLocalStorePutGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	body := []byte("hello blob world")

	sha, n, err := s.Put(ctx, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("Put size = %d, want %d", n, len(body))
	}
	if sha != shaOf(body) {
		t.Errorf("Put sha = %q, want %q", sha, shaOf(body))
	}

	rc, err := s.Get(ctx, sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("round-trip body mismatch:\n got %q\nwant %q", got, body)
	}
}

func TestLocalStorePutDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	body := []byte("identical bytes")

	sha1, _, err := s.Put(ctx, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	sha2, _, err := s.Put(ctx, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if sha1 != sha2 {
		t.Errorf("dedup: shas differ %q vs %q", sha1, sha2)
	}

	// Confirm only ONE file exists on disk under the shard directory.
	shard := filepath.Join(s.root, sha1[:2])
	entries, err := os.ReadDir(shard)
	if err != nil {
		t.Fatalf("ReadDir shard: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dedup: %d files in shard, want 1: %+v", len(entries), entries)
	}

	// Confirm no leaked temp files.
	tmpEntries, err := os.ReadDir(filepath.Join(s.root, ".tmp"))
	if err != nil {
		t.Fatalf("ReadDir tmp: %v", err)
	}
	if len(tmpEntries) != 0 {
		t.Errorf("temp dir has %d leftover files: %+v", len(tmpEntries), tmpEntries)
	}
}

func TestLocalStoreGetMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: err = %v, want ErrNotFound", err)
	}
}

func TestLocalStoreDeleteIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	body := []byte("delete me")
	sha, _, err := s.Put(ctx, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// First delete: succeeds, file is gone.
	if err := s.Delete(ctx, sha); err != nil {
		t.Errorf("first Delete: %v", err)
	}
	if _, err := s.Get(ctx, sha); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get post-delete: err = %v, want ErrNotFound", err)
	}

	// Second delete: silent no-op (idempotent contract).
	if err := s.Delete(ctx, sha); err != nil {
		t.Errorf("second Delete (idempotent): %v", err)
	}
	// Third delete on a sha never seen: also silent.
	if err := s.Delete(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Errorf("Delete unknown sha: %v", err)
	}
}

func TestLocalStoreURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	body := []byte("urlable")
	sha, _, err := s.Put(ctx, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.URL(ctx, sha, time.Hour)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	want := "/api/v1/blobs/" + sha
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}

	// Missing blob → ErrNotFound (fail-fast contract).
	if _, err := s.URL(ctx, "0000000000000000000000000000000000000000000000000000000000000000", time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("URL missing: err = %v, want ErrNotFound", err)
	}
}

func TestLocalStoreURLPrefixOverride(t *testing.T) {
	s := newTestStore(t).WithURLPrefix("/proxy/blob/")
	ctx := context.Background()
	sha, _, err := s.Put(ctx, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	url, err := s.URL(ctx, sha, 0)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.HasPrefix(url, "/proxy/blob/") {
		t.Errorf("URL = %q, want prefix /proxy/blob/", url)
	}
}

func TestLocalStoreContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := s.Put(ctx, bytes.NewReader([]byte("x"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Put cancelled ctx: err = %v, want context.Canceled", err)
	}
	if _, err := s.Get(ctx, "abc"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get cancelled ctx: err = %v, want context.Canceled", err)
	}
	if err := s.Delete(ctx, "abc"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete cancelled ctx: err = %v, want context.Canceled", err)
	}
	if _, err := s.URL(ctx, "abc", time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("URL cancelled ctx: err = %v, want context.Canceled", err)
	}
}

func TestLocalStoreConcurrentPutSameContent(t *testing.T) {
	// Same content, many goroutines, racing. All must succeed and
	// all must return the same sha. Final on-disk count = 1.
	s := newTestStore(t)
	ctx := context.Background()
	body := []byte("concurrent payload")
	const N = 16

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		shas []string
		errs []error
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sha, _, err := s.Put(ctx, bytes.NewReader(body))
			mu.Lock()
			shas = append(shas, sha)
			errs = append(errs, err)
			mu.Unlock()
		}()
	}
	wg.Wait()

	want := shaOf(body)
	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: Put err = %v", i, e)
		}
		if shas[i] != want {
			t.Errorf("goroutine %d: sha = %q, want %q", i, shas[i], want)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.root, want[:2]))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("concurrent put: %d files on disk, want 1", len(entries))
	}
}

func TestNewLocalStoreEmptyRootRejected(t *testing.T) {
	if _, err := NewLocalStore(""); err == nil {
		t.Errorf("NewLocalStore(\"\"): want error, got nil")
	}
}
