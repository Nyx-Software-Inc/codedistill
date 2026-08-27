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

// Package blobstore is the content-addressed binary storage layer that
// backs scratchpad items whose content_type is image / file / sketch
// preview / etc. Items reference blobs by SHA-256; the store maps
// SHA → bytes and is responsible for nothing else (no refcounting,
// no auth, no MIME interpretation).
//
// Two impls live behind the same interface:
//
//   - LocalStore (this package, default) — content-addressed
//     filesystem under a configured root directory. Used by
//     single-user installs and self-hosted deployments that don't need
//     horizontal scale.
//   - S3Store (Slice 5, internal/blobstore/s3.go) — S3-compatible
//     object store (R2, GCS, S3, MinIO). Used by the hosted SaaS
//     offering and by self-hosters who scale horizontally.
//
// The interface is intentionally small: callers can swap stores via
// a single line of construction wiring. See docs/design/rich-canvas-design.md
// for the full architectural context.
package blobstore

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by Get / URL when the requested sha is not
// present in the store. Delete does NOT return ErrNotFound — it is
// idempotent on absence so callers can drop blobs without first
// checking presence.
var ErrNotFound = errors.New("blobstore: blob not found")

// BlobStore is the interface every storage backend satisfies. All
// methods are safe for concurrent use.
//
// Put returns the SHA-256 hex digest of the content plus its byte
// count. If a blob with the same content already exists the store
// returns the existing sha unchanged (free dedup). Callers that need
// per-upload identity (e.g. tracking who uploaded what when) should
// store that out-of-band keyed by sha.
//
// Get returns an open reader; callers must Close. The returned reader
// streams from storage — do not assume the full content is buffered.
//
// Delete removes the blob; absent blobs are a silent no-op. Callers
// must coordinate refcounting themselves (the store has no knowledge
// of which items reference a sha).
//
// URL returns an address that can be GETed to fetch the blob without
// going through the JSON API — used to hand MCP clients a direct
// fetch link rather than streaming bytes through `read_item`. The
// LocalStore returns a server-relative path that the HTTP layer
// resolves via the GET /api/v1/blobs/{sha} endpoint; the S3Store
// returns a presigned URL valid for at most `expires`. Implementations
// that don't enforce expiry (LocalStore) may ignore the parameter.
type BlobStore interface {
	Put(ctx context.Context, r io.Reader) (sha string, size int64, err error)
	Get(ctx context.Context, sha string) (io.ReadCloser, error)
	Delete(ctx context.Context, sha string) error
	URL(ctx context.Context, sha string, expires time.Duration) (string, error)
}
