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
	"fmt"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/embed"
)

// fixedEmbedder returns the same vector for anything, so a seeded chunk and the
// item being classified score a perfect cosine match.
type fixedEmbedder struct{ vec []float32 }

func (f *fixedEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return f.vec, nil
}

// seedEmbeddedChunk writes one code_chunks row WITH an embedding — the state a
// database is left in after `index-code` has run under a valid license.
func seedEmbeddedChunk(t *testing.T, fx *fixture, id, path string, vec []float32) {
	t.Helper()
	ctx := context.Background()
	if err := fx.store.UpsertCodeChunk(ctx, &domain.CodeChunk{
		ID: id, ProjectID: "p1", FilePath: path,
		LineStart: 1, LineEnd: 20,
		ContentHash: "h-" + id, Content: "package main",
		CreatedAt: fx.nowT, UpdatedAt: fx.nowT,
	}); err != nil {
		t.Fatalf("upsert chunk: %v", err)
	}
	if err := fx.store.UpdateCodeChunkEmbedding(ctx, id, embed.EncodeFloat32(vec), fx.nowT); err != nil {
		t.Fatalf("embed chunk: %v", err)
	}
}

// newAnchorFixture builds an agent whose classifier suggests the seeded paths
// and whose embedder produces a vector matching the seeded chunks. opts are
// appended last so a test can turn the gate on.
func newAnchorFixture(t *testing.T, opts ...Option) *fixture {
	t.Helper()
	fx := newFixture(t)
	vec := []float32{1, 0, 0, 0}

	idCounter := 0
	base := []Option{
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string { idCounter++; return fmt.Sprintf("a-%d", idCounter) }),
		WithEmbedder(&fixedEmbedder{vec: vec}),
	}
	fx.agent = New(fx.store, fx.clf, append(base, opts...)...)
	fx.clf.category = "TODO"
	fx.clf.suggestedFiles = []string{"internal/api/server.go"}
	fx.seed(t, "full", "")
	seedEmbeddedChunk(t, fx, "chunk-1", "internal/api/server.go", vec)
	return fx
}

func anchorsFor(t *testing.T, fx *fixture, itemID string) []*domain.CodeAnchor {
	t.Helper()
	got, err := fx.store.ListCodeAnchors(context.Background(), "scratchpad_item", itemID)
	if err != nil {
		t.Fatalf("list anchors: %v", err)
	}
	return got
}

// Auto-anchoring is the paid CodeAnchors feature, and main.go gated it by only
// wiring WithProjectFileTree when licensed. But deriveAutoAnchorPaths has a
// SECOND source: SearchCodeChunks, which was called with no gate at all. When
// embedded code_chunks rows already exist, that path reaches CreateCodeAnchor
// and writes "agent-suggested" anchors on an unlicensed build.
//
// A fresh CE install is clean — index-code refuses, so there are no chunks. The
// realistic trigger is not the CE binary at all: it is a LAPSED license on the
// same commercial binary. Features switch off; the rows stay behind.
//
// Nothing exercised the chunk path before this file, which is how it survived
// (CE-review item 28).
func TestAutoAnchor_ChunkPathIsGated(t *testing.T) {
	fx := newAnchorFixture(t) // no WithCodeAnchors → unlicensed

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	if got := anchorsFor(t, fx, "si1"); len(got) != 0 {
		t.Errorf("unlicensed build created %d agent-suggested anchor(s) from pre-existing "+
			"code_chunks; want 0.\nThe chunk search is a second, ungated route into the "+
			"paid auto-anchor path.", len(got))
	}
}

// The gate must not break the feature it is gating.
func TestAutoAnchor_ChunkPathWorksWhenLicensed(t *testing.T) {
	fx := newAnchorFixture(t, WithCodeAnchors(true))

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	got := anchorsFor(t, fx, "si1")
	if len(got) != 1 {
		t.Fatalf("licensed build created %d anchors, want 1", len(got))
	}
	if got[0].Provenance != "agent-suggested" {
		t.Errorf("provenance = %q, want agent-suggested", got[0].Provenance)
	}
	if got[0].Path != "internal/api/server.go" {
		t.Errorf("path = %q, want internal/api/server.go", got[0].Path)
	}
}
