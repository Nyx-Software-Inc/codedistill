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

package sqlite

import (
	"context"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// RatifyProposedArchitecture must promote every proposed node AND edge to
// ratified atomically, leaving already-ratified structure untouched (bug 100 —
// replaces the handler-side delete-then-recreate that could lose an edge).
func TestRatifyProposedArchitecture(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Two proposed nodes + one already-ratified node.
	mkNode := func(id, prov string) *domain.ArchitectureNode {
		return &domain.ArchitectureNode{ID: id, ProjectID: "p1", Name: id, Kind: "component", Provenance: prov, CreatedAt: now, UpdatedAt: now}
	}
	for _, n := range []*domain.ArchitectureNode{mkNode("n1", "proposed"), mkNode("n2", "proposed"), mkNode("n3", "ratified")} {
		if err := s.CreateArchitectureNode(ctx, n); err != nil {
			t.Fatalf("create node %s: %v", n.ID, err)
		}
	}
	// A proposed edge and an already-ratified edge.
	mkEdge := func(id, prov string) *domain.ArchitectureEdge {
		return &domain.ArchitectureEdge{ID: id, ProjectID: "p1", FromNode: "n1", ToNode: "n2", Label: "uses", Provenance: prov, CreatedAt: now}
	}
	for _, e := range []*domain.ArchitectureEdge{mkEdge("e1", "proposed"), mkEdge("e2", "ratified")} {
		if err := s.CreateArchitectureEdge(ctx, e); err != nil {
			t.Fatalf("create edge %s: %v", e.ID, err)
		}
	}

	if err := s.RatifyProposedArchitecture(ctx, "p1", now.Add(time.Minute)); err != nil {
		t.Fatalf("ratify: %v", err)
	}

	nodes, err := s.ListArchitectureNodes(ctx, "p1")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (none lost)", len(nodes))
	}
	for _, n := range nodes {
		if n.Provenance != "ratified" {
			t.Errorf("node %s provenance = %q, want ratified", n.ID, n.Provenance)
		}
	}
	edges, err := s.ListArchitectureEdges(ctx, "p1")
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want 2 (the proposed edge must survive as ratified, not be lost)", len(edges))
	}
	for _, e := range edges {
		if e.Provenance != "ratified" {
			t.Errorf("edge %s provenance = %q, want ratified", e.ID, e.Provenance)
		}
	}
}
