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

func newFinding(id, fp, sev string) *domain.CodeFinding {
	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	return &domain.CodeFinding{
		ID: id, ProjectID: "p1", Fingerprint: fp,
		Analyzer: "gosec", RuleID: "G101", Severity: sev, Source: "scan",
		FilePath: "internal/x.go", LineStart: 10, LineEnd: 10,
		Title: "hardcoded creds", Status: domain.FindingOpen,
		FirstSeenAt: now, LastSeenAt: now,
	}
}

func TestFindingUpsertAndResolveLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s)

	// First scan: two new findings.
	for _, f := range []*domain.CodeFinding{newFinding("f1", "fp1", "high"), newFinding("f2", "fp2", "low")} {
		inserted, err := s.UpsertCodeFinding(ctx, f)
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if !inserted {
			t.Fatalf("expected %s inserted as new", f.ID)
		}
	}

	open, err := s.ListCodeFindings(ctx, "p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("want 2 open, got %d", len(open))
	}
	// Severity ordering: high before low.
	if open[0].Severity != "high" {
		t.Errorf("want high first, got %s", open[0].Severity)
	}

	// Dismiss f2 — must survive a re-scan.
	if err := s.UpdateCodeFindingStatus(ctx, "f2", domain.FindingDismissed, "", time.Now().UTC()); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	// Re-scan: fp1 seen again (not new), fp2 NOT seen but it's dismissed so it
	// must not be resolved; a brand-new fp3 appears.
	again := newFinding("f1b", "fp1", "high")
	inserted, err := s.UpsertCodeFinding(ctx, again)
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if inserted {
		t.Errorf("fp1 should update in place, not insert")
	}
	if _, err := s.UpsertCodeFinding(ctx, newFinding("f3", "fp3", "medium")); err != nil {
		t.Fatalf("upsert fp3: %v", err)
	}

	resolved, err := s.ResolveStaleFindings(ctx, "p1", "scan", map[string]bool{"fp1": true, "fp3": true}, time.Now().UTC())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Only open findings missing from the scan resolve. fp2 is dismissed (skip),
	// fp1 + fp3 were seen — so nothing resolves.
	if resolved != 0 {
		t.Errorf("want 0 resolved, got %d", resolved)
	}

	// Now drop fp3 from a scan that only saw fp1 → fp3 resolves.
	resolved, err = s.ResolveStaleFindings(ctx, "p1", "scan", map[string]bool{"fp1": true}, time.Now().UTC())
	if err != nil {
		t.Fatalf("resolve2: %v", err)
	}
	if resolved != 1 {
		t.Errorf("want 1 resolved, got %d", resolved)
	}

	// Page now shows only fp1 (fp2 dismissed, fp3 resolved).
	open, _ = s.ListCodeFindings(ctx, "p1")
	if len(open) != 1 || open[0].Fingerprint != "fp1" {
		t.Fatalf("want only fp1 open, got %+v", open)
	}
}

// TestFindingTaxonomyRoundTrip pins the slice-3 columns: category,
// security_severity, and source persist and refresh on re-scan.
func TestFindingTaxonomyRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s)

	f := newFinding("f1", "fp1", "high")
	f.Category = "security"
	f.SecuritySeverity = 8.5
	f.Source = "scan"
	if _, err := s.UpsertCodeFinding(ctx, f); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	open, err := s.ListCodeFindings(ctx, "p1")
	if err != nil || len(open) != 1 {
		t.Fatalf("list: %v (n=%d)", err, len(open))
	}
	if g := open[0]; g.Category != "security" || g.SecuritySeverity != 8.5 || g.Source != "scan" {
		t.Fatalf("taxonomy not persisted: cat=%q sev=%v src=%q", g.Category, g.SecuritySeverity, g.Source)
	}

	// Re-scan with a refreshed score (e.g. the rule's CVSS was retuned) updates in place.
	again := newFinding("f1b", "fp1", "high")
	again.Category = "security"
	again.SecuritySeverity = 9.1
	again.Source = "ci"
	if inserted, err := s.UpsertCodeFinding(ctx, again); err != nil || inserted {
		t.Fatalf("re-upsert should update in place: inserted=%v err=%v", inserted, err)
	}
	open, _ = s.ListCodeFindings(ctx, "p1")
	if g := open[0]; g.SecuritySeverity != 9.1 || g.Source != "ci" {
		t.Errorf("re-scan did not refresh taxonomy: sev=%v src=%q", g.SecuritySeverity, g.Source)
	}
}

func TestAnalysisScanRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s)

	if _, err := s.LatestAnalysisScan(ctx, "p1"); err == nil {
		t.Fatalf("expected no scan yet")
	}
	fin := time.Date(2026, 6, 23, 9, 5, 0, 0, time.UTC)
	sc := &domain.AnalysisScan{
		ID: "s1", ProjectID: "p1", Trigger: "manual",
		StartedAt: fin.Add(-time.Minute), FinishedAt: &fin,
		FilesScanned: 42, FindingsNew: 3, FindingsResolved: 1,
		Skipped: "staticcheck not installed",
	}
	if err := s.CreateAnalysisScan(ctx, sc); err != nil {
		t.Fatalf("create scan: %v", err)
	}
	got, err := s.LatestAnalysisScan(ctx, "p1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.FilesScanned != 42 || got.FindingsNew != 3 || got.Skipped != "staticcheck not installed" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.FinishedAt == nil {
		t.Errorf("finished_at should round-trip")
	}
}
