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

	"codedistill/internal/domain"
)

func seedDecompose(t *testing.T, s *Store) (projectID, jobID string) {
	t.Helper()
	p := seedProject(t, s)
	j := seedJobIn(t, s, p.ID, "job-1", domain.JobDecompose, "doc-1", domain.JobRunning)
	return p.ID, j.ID
}

func proposal(projectID, jobID, id, kind, subject, origin string) *domain.DecomposeProposal {
	return &domain.DecomposeProposal{
		ID: id, JobID: jobID, ProjectID: projectID, Kind: kind, Subject: subject,
		Origin: origin, Lines: [][2]int{{10, 12}},
	}
}

func TestSaveAndListProposals(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pid, jid := seedDecompose(t, s)
	now := fixedTime(t)

	run := &domain.DecomposeRun{
		JobID: jid, ProjectID: pid, SourceItemID: "doc-1", SourceLabel: "spec.odt",
		Sentences: 127, ExcludedLines: 31, TablesRead: 1,
		Corroborated: 22, TableOnly: 8, ProseOnly: 7, CreatedAt: now,
	}
	table := proposal(pid, jid, "p1", "use_case", "Search across all notes", domain.OriginTable)
	table.Corroborated, table.ExternalRef, table.Priority = true, "UC-18", "P1"
	props := []*domain.DecomposeProposal{
		proposal(pid, jid, "p2", "todo", "Wire the export endpoint", domain.OriginProse),
		table,
	}
	for _, p := range props {
		p.CreatedAt = now
	}
	if err := s.SaveDecomposeRun(ctx, run, props); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.GetDecomposeRun(ctx, jid)
	if err != nil || got == nil {
		t.Fatalf("GetDecomposeRun = %v, %v", got, err)
	}
	// The count that answers "how would I know something was missed".
	if got.TableOnly != 8 || got.Corroborated != 22 {
		t.Errorf("reconciliation lost: %+v", got)
	}

	list, err := s.ListProposals(ctx, jid, nil)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListProposals = %d, %v", len(list), err)
	}
	// Corroborated first: strongest evidence available, and what a reviewer can
	// accept in bulk without reading closely.
	if list[0].ID != "p1" {
		t.Errorf("ordering put %q first, want the corroborated table row", list[0].ID)
	}
	if !list[0].Corroborated || list[0].ExternalRef != "UC-18" || list[0].Priority != "P1" {
		t.Errorf("table fields lost: %+v", list[0])
	}
	if len(list[0].Lines) != 1 || list[0].Lines[0] != [2]int{10, 12} {
		t.Errorf("citation did not round-trip: %v", list[0].Lines)
	}
	for _, p := range list {
		if p.Status != domain.ProposalPending {
			t.Errorf("%s saved as %q — nothing may exist before a human accepts it", p.ID, p.Status)
		}
	}
}

// A slow review screen submitting twice must not create the item twice.
func TestDecideIsOnceOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pid, jid := seedDecompose(t, s)
	now := fixedTime(t)
	p := proposal(pid, jid, "p1", "todo", "Rate-limit export", domain.OriginProse)
	p.CreatedAt = now
	if err := s.SaveDecomposeRun(ctx, &domain.DecomposeRun{JobID: jid, ProjectID: pid, SourceItemID: "doc-1", CreatedAt: now},
		[]*domain.DecomposeProposal{p}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := s.DecideProposal(ctx, "p1", domain.ProposalAccepted, "todo-9", "", "u1", now); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// Second submit, different item id: must not overwrite.
	if err := s.DecideProposal(ctx, "p1", domain.ProposalAccepted, "todo-10", "", "u1", now); err != nil {
		t.Fatalf("second accept errored: %v", err)
	}
	list, _ := s.ListProposals(ctx, jid, nil)
	if list[0].CreatedItemID != "todo-9" {
		t.Fatalf("created item = %q, want todo-9 — the second submit won", list[0].CreatedItemID)
	}
}

func TestDecideRejectsMalformedVerdicts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pid, jid := seedDecompose(t, s)
	now := fixedTime(t)
	p := proposal(pid, jid, "p1", "todo", "X", domain.OriginProse)
	p.CreatedAt = now
	_ = s.SaveDecomposeRun(ctx, &domain.DecomposeRun{JobID: jid, ProjectID: pid, SourceItemID: "doc-1", CreatedAt: now},
		[]*domain.DecomposeProposal{p})

	for name, call := range map[string]error{
		"pending is not a verdict": s.DecideProposal(ctx, "p1", domain.ProposalPending, "", "", "", now),
		"unknown verdict":          s.DecideProposal(ctx, "p1", "maybe", "", "", "", now),
		"accepted without an item": s.DecideProposal(ctx, "p1", domain.ProposalAccepted, "", "", "", now),
		"linked without a target":  s.DecideProposal(ctx, "p1", domain.ProposalLinked, "", "", "", now),
	} {
		if call == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Link is not rejection. Conflating them throws away the citation instead of
// attaching it to the work that already exists.
func TestLinkKeepsTheCitationAndIsNotARejection(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pid, jid := seedDecompose(t, s)
	now := fixedTime(t)
	p := proposal(pid, jid, "p1", "bug", "Archive count excludes soft-deleted rows", domain.OriginProse)
	p.CreatedAt = now
	_ = s.SaveDecomposeRun(ctx, &domain.DecomposeRun{JobID: jid, ProjectID: pid, SourceItemID: "doc-1", CreatedAt: now},
		[]*domain.DecomposeProposal{p})

	if err := s.DecideProposal(ctx, "p1", domain.ProposalLinked, "bug-18", "", "u1", now); err != nil {
		t.Fatalf("link: %v", err)
	}
	list, _ := s.ListProposals(ctx, jid, nil)
	if list[0].LinkedItemID != "bug-18" || list[0].CreatedItemID != "" {
		t.Errorf("link created something: %+v", list[0])
	}
	if len(list[0].Lines) == 0 {
		t.Error("linking discarded the citation")
	}
	// A link must not surface as rejection memory.
	rej, _ := s.RejectedProposals(ctx, pid, 0)
	if len(rej) != 0 {
		t.Errorf("a linked duplicate was recorded as a rejection: %+v", rej)
	}
}

// Rejections are project-scoped, so a judgement survives the draft it was made
// against.
func TestRejectionMemoryIsProjectScoped(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pid, jid := seedDecompose(t, s)
	now := fixedTime(t)
	p := proposal(pid, jid, "p1", "use_case", "Single sign-on via Okta", domain.OriginProse)
	p.CreatedAt = now
	_ = s.SaveDecomposeRun(ctx, &domain.DecomposeRun{JobID: jid, ProjectID: pid, SourceItemID: "doc-1", CreatedAt: now},
		[]*domain.DecomposeProposal{p})

	if err := s.DecideProposal(ctx, "p1", domain.ProposalRejected, "", "out of scope for Q4", "u1", now); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// A LATER decomposition of a revised document is a different job; the
	// judgement still has to be reachable.
	rej, err := s.RejectedProposals(ctx, pid, 0)
	if err != nil || len(rej) != 1 {
		t.Fatalf("RejectedProposals = %d, %v", len(rej), err)
	}
	if rej[0].RejectReason != "out of scope for Q4" {
		t.Errorf("reason lost: %q", rej[0].RejectReason)
	}
	if rej[0].DecidedAt == nil {
		t.Error("decided_at not stamped — a suppressed proposal must show when it was rejected")
	}
}

// Dropping the same file twice must not cost another fifteen minutes, and a
// REVISION must be recognisable as one rather than read as a new document.
func TestFindRunsDistinguishesRedropFromRevision(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	now := fixedTime(t)
	mod := now.Add(-48 * 60 * 60 * 1e9)

	j1 := seedJobIn(t, s, p.ID, "job-1", domain.JobDecompose, "doc-1", domain.JobRunning)
	v1 := &domain.DecomposeRun{
		JobID: j1.ID, ProjectID: p.ID, SourceItemID: "doc-1", SourceLabel: "spec.odt",
		SourceHash: "aaa111", SourceBytes: 1608146, ModifiedAt: &mod, CreatedAt: now,
	}
	if err := s.SaveDecomposeRun(ctx, v1, nil); err != nil {
		t.Fatalf("save v1: %v", err)
	}

	// Same bytes dropped again: the hash matches, so there is nothing to read.
	same, err := s.FindDecomposeRuns(ctx, p.ID, "aaa111", "")
	if err != nil || len(same) != 1 {
		t.Fatalf("identical re-drop not recognised: %d, %v", len(same), err)
	}
	if same[0].SourceBytes != 1608146 || same[0].ModifiedAt == nil {
		t.Errorf("size/modified lost — a human deciding about a re-drop needs them: %+v", same[0])
	}

	// Edited file: different hash, same name. That is a revision, and the one
	// worth reading again.
	byHash, _ := s.FindDecomposeRuns(ctx, p.ID, "bbb222", "")
	if len(byHash) != 0 {
		t.Errorf("a changed document matched by hash: %+v", byHash)
	}
	revision, err := s.FindDecomposeRuns(ctx, p.ID, "bbb222", "spec.odt")
	if err != nil || len(revision) != 1 || revision[0].SourceHash != "aaa111" {
		t.Fatalf("prior version not found for a revision: %d, %v", len(revision), err)
	}

	// A different document entirely matches nothing.
	if other, _ := s.FindDecomposeRuns(ctx, p.ID, "ccc333", "other.md"); len(other) != 0 {
		t.Errorf("an unrelated document matched: %+v", other)
	}
}
