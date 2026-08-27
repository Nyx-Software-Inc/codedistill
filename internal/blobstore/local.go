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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// LocalStore is a content-addressed filesystem BlobStore. Blobs land
// at `<root>/<sha[0:2]>/<sha>`. The two-character prefix shards keep
// per-directory file counts well below filesystem cliffs (ext4
// degrades past ~10k entries per dir).
//
// Writes are atomic: bytes stream into a temp file under
// `<root>/.tmp/`, the SHA is computed during the stream, then the
// temp file is renamed into its final sharded location. Same-content
// races resolve correctly because both writers compute the same sha
// and either rename produces the same final bytes.
type LocalStore struct {
	root string

	// urlPrefix is prepended to the sha when URL() builds the address.
	// Defaults to "/api/v1/blobs/" — the HTTP server's blob GET route.
	// Configurable so tests can assert exact URL shapes.
	urlPrefix string
}

// NewLocalStore returns a LocalStore rooted at the given directory.
// The root + the `.tmp` subdir are created with mode 0750 if missing
// (owner rwx, group r-x, no world access — the store holds user blobs).
// Returns an error if the root cannot be created or is not writable.
func NewLocalStore(root string) (*LocalStore, error) {
	if root == "" {
		return nil, errors.New("blobstore: root path is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("blobstore: abs root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, ".tmp"), 0o750); err != nil {
		return nil, fmt.Errorf("blobstore: mkdir root: %w", err)
	}
	return &LocalStore{root: abs, urlPrefix: "/api/v1/blobs/"}, nil
}

// WithURLPrefix overrides the path prefix returned by URL(). Used by
// hosted deployments behind a path-rewriting proxy and by tests.
func (s *LocalStore) WithURLPrefix(p string) *LocalStore {
	s.urlPrefix = p
	return s
}

// finalPath is `<root>/<sha[0:2]>/<sha>`. Caller is responsible for
// ensuring sha is valid (40+ hex chars); we don't re-validate on
// every call since shas only enter the store via Put which generates
// them and via the API layer which has its own validation.
func (s *LocalStore) finalPath(sha string) string {
	if len(sha) < 2 {
		// Defensive: a malformed sha would otherwise produce a path
		// like `<root>//<sha>` that succeeds but isn't shardable.
		// Returning a path that will fail-open is preferable to a
		// panic.
		return filepath.Join(s.root, sha)
	}
	return filepath.Join(s.root, sha[:2], sha)
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, ".tmp"), "blob-*")
	if err != nil {
		return "", 0, fmt.Errorf("blobstore: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// On any error path below we want the temp file cleaned up. The
	// success path renames it away first so the Remove becomes a
	// silent no-op.
	defer func() {
		tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	h := sha256.New()
	tee := io.TeeReader(r, h)
	n, err := io.Copy(tmp, tee)
	if err != nil {
		return "", 0, fmt.Errorf("blobstore: stream body: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, fmt.Errorf("blobstore: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("blobstore: close temp: %w", err)
	}
	sha := hex.EncodeToString(h.Sum(nil))

	dest := s.finalPath(sha)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", 0, fmt.Errorf("blobstore: mkdir shard: %w", err)
	}

	// Dedup fast-path: if the final file already exists we don't
	// need to rename. Skip via Stat before Rename so we don't burn
	// the inode on the duplicate.
	if _, err := os.Stat(dest); err == nil {
		// Already present — same content (sha matches). Drop the temp
		// (the deferred Remove handles it).
		return sha, n, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, fmt.Errorf("blobstore: stat dest: %w", err)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return "", 0, fmt.Errorf("blobstore: rename to dest: %w", err)
	}
	return sha, n, nil
}

func (s *LocalStore) Get(ctx context.Context, sha string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.finalPath(sha))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blobstore: open: %w", err)
	}
	return f, nil
}

// Delete removes the blob and prunes the shard dir if empty. Missing
// blobs are silent (idempotent contract). The empty-shard prune is
// best-effort — a non-empty dir simply means another blob shares the
// prefix.
func (s *LocalStore) Delete(ctx context.Context, sha string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.finalPath(sha)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blobstore: remove: %w", err)
	}
	// Best-effort: prune the shard directory if it just became empty.
	// Ignore the error — a non-empty dir is the common case.
	_ = os.Remove(filepath.Dir(path))
	return nil
}

// URL builds the HTTP path the LLM / browser uses to fetch the blob.
// The expires parameter is ignored — local paths are stable. The
// returned string is server-relative ("/api/v1/blobs/<sha>"); callers
// that need an absolute URL prepend the appropriate scheme + host.
//
// Returns ErrNotFound if the blob is absent, mirroring Get — this
// lets callers fail fast when generating MCP responses rather than
// handing the LLM a 404-able URL.
func (s *LocalStore) URL(ctx context.Context, sha string, _ time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := os.Stat(s.finalPath(sha)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("blobstore: stat: %w", err)
	}
	return s.urlPrefix + sha, nil
}
