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
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// The guard lives in the store rather than the picker because the picker is one
// of several doors: the sequence job writes edges straight from a model's
// proposals and never passes through the UI's check.

const cycProject = "p-cyc"

func cycEdge(from, to string) *domain.ItemDependency {
	return &domain.ItemDependency{
		ID:        "e-" + from + "-" + to,
		ProjectID: cycProject,
		FromKind:  "todo", FromID: from,
		ToKind: "todo", ToID: to,
		Kind:      domain.DependencyBlocks,
		Origin:    domain.DependencyFromHuman,
		Status:    domain.EdgeAccepted,
		CreatedAt: time.Now().UTC(),
	}
}

// cycStore is a store with the two projects these tests reference already
// created — item_dependencies.project_id is a real FK.
func cycStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{cycProject, "p-other"} {
		if err := s.CreateProject(ctx, &domain.Project{ID: id, Name: id, CreatedAt: now}); err != nil {
			t.Fatalf("create project %s: %v", id, err)
		}
	}
	return s
}

func mustAdd(t *testing.T, s *Store, d *domain.ItemDependency) {
	t.Helper()
	if err := s.CreateDependency(context.Background(), d); err != nil {
		t.Fatalf("create %s->%s: %v", d.FromID, d.ToID, err)
	}
}

func TestDependency_RefusesTheDirectInverse(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	mustAdd(t, s, cycEdge("a", "b")) // a waits on b

	err := s.CreateDependency(ctx, cycEdge("b", "a")) // b waits on a
	if err == nil {
		t.Fatal("a two-node loop was accepted; neither item could ever start")
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Errorf("error should say what is wrong, got: %v", err)
	}
}

func TestDependency_RefusesALongCycle(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	// b -> c -> d -> a already in place.
	mustAdd(t, s, cycEdge("b", "c"))
	mustAdd(t, s, cycEdge("c", "d"))
	mustAdd(t, s, cycEdge("d", "a"))

	// "a waits on b" closes a four-node loop. An inverse-only check — the
	// obvious implementation — misses this entirely.
	if err := s.CreateDependency(ctx, cycEdge("a", "b")); err == nil {
		t.Fatal("a four-node loop was accepted")
	}
}

func TestDependency_AllowsADiamond(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	// a waits on b; b and c both wait on d. Orderable, and the guard must not
	// mistake a shared prerequisite for a cycle.
	mustAdd(t, s, cycEdge("a", "b"))
	mustAdd(t, s, cycEdge("b", "d"))
	mustAdd(t, s, cycEdge("c", "d"))

	if err := s.CreateDependency(ctx, cycEdge("a", "c")); err != nil {
		t.Fatalf("a diamond is not a cycle: %v", err)
	}
}

func TestDependency_DismissedEdgesDoNotConstrain(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	d := cycEdge("b", "a")
	d.Status = domain.EdgeDismissed
	mustAdd(t, s, d)

	// Someone argued with "b waits on a" and it is kept only so a later run does
	// not re-offer it. It must not veto the opposite edge now being asserted.
	if err := s.CreateDependency(ctx, cycEdge("a", "b")); err != nil {
		t.Fatalf("a dismissed edge still constrained the graph: %v", err)
	}
}

func TestDependency_AdvisoryEdgesDoConstrain(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	d := cycEdge("b", "a")
	d.Status = domain.EdgeAdvisory
	mustAdd(t, s, d)

	// A derived edge is in force with nobody having ratified it, so it can close
	// a loop just as an accepted one can.
	if err := s.CreateDependency(ctx, cycEdge("a", "b")); err == nil {
		t.Fatal("an advisory edge was treated as not constraining")
	}
}

func TestDependency_InformsLoopsAreHarmless(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	i1 := cycEdge("a", "b")
	i1.Kind = domain.DependencyInforms
	mustAdd(t, s, i1)

	i2 := cycEdge("b", "a")
	i2.Kind = domain.DependencyInforms
	// 'informs' orders nothing — it says two things are easier together, which
	// is true in both directions. Refusing this would be refusing a fact.
	if err := s.CreateDependency(ctx, i2); err != nil {
		t.Fatalf("an informs loop should be allowed: %v", err)
	}
}

func TestDependency_CycleGuardIsPerProject(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	mustAdd(t, s, cycEdge("a", "b"))

	// The same ids in a different project are different work. Reading the graph
	// without the project filter would make one project's edges veto another's.
	other := cycEdge("b", "a")
	other.ID = "e-other"
	other.ProjectID = "p-other"
	if err := s.CreateDependency(ctx, other); err != nil {
		t.Fatalf("another project's edges must not constrain this one: %v", err)
	}
}

func TestDependency_TerminatesOnDataThatAlreadyHasACycle(t *testing.T) {
	s := cycStore(t)
	ctx := context.Background()
	// Rows written before the guard existed can hold a loop. Inserted straight
	// through the DB so the guard itself cannot prevent the setup.
	for _, e := range [][2]string{{"a", "b"}, {"b", "a"}} {
		d := cycEdge(e[0], e[1])
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO item_dependencies
			   (id, project_id, from_kind, from_id, to_kind, to_id, kind,
			    rationale, lines, origin, created_at, status)
			 VALUES (?,?,?,?,?,?,?,'','[]',?,?,?)`,
			d.ID, d.ProjectID, d.FromKind, d.FromID, d.ToKind, d.ToID, d.Kind,
			d.Origin, d.CreatedAt, d.Status); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// A recursive walk over this would never return. The test passing at all is
	// the assertion; a hang is the failure.
	done := make(chan error, 1)
	go func() { done <- s.CreateDependency(ctx, cycEdge("c", "a")) }()
	select {
	case <-done:
		// Either verdict is fine; not hanging is the point.
	case <-time.After(5 * time.Second):
		t.Fatal("cycle guard hung on data that already contained a cycle")
	}
}
