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

	"codedistill/internal/seckb"
)

func TestSecurityKnowledgeStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if r, err := s.GetSecuritySource(ctx, "cwe"); err != nil || r != nil {
		t.Fatalf("never seen: %+v %v", r, err)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	row := &seckb.SourceRow{ID: "cwe", LastAttempt: &now, LastSuccess: &now, Version: "5.0", Entries: 2}
	if err := s.SaveSecuritySource(ctx, row); err != nil {
		t.Fatal(err)
	}
	row.LastError, row.Failures = "timeout", 1
	if err := s.SaveSecuritySource(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSecuritySource(ctx, "cwe")
	if err != nil || got.LastError != "timeout" || got.Failures != 1 || !got.LastSuccess.Equal(now) || got.Version != "5.0" {
		t.Fatalf("source = %+v %v", got, err)
	}
	if err := s.ReplaceSecurityEntries(ctx, "cwe", "5.0", []seckb.Entry{
		{Key: "CWE-916", Title: "Weak password hash", Body: "b", URL: "u"}, {Key: "CWE-327", Title: "Broken crypto"},
	}); err != nil {
		t.Fatal(err)
	}
	// A new version replaces the old entirely.
	if err := s.ReplaceSecurityEntries(ctx, "cwe", "5.1", []seckb.Entry{{Key: "CWE-916", Title: "Weak password hash v2"}}); err != nil {
		t.Fatal(err)
	}
	e, err := s.GetSecurityEntry(ctx, "cwe-916")
	if err != nil || e.Title != "Weak password hash v2" || e.Version != "5.1" || e.Source != "cwe" {
		t.Fatalf("entry = %+v %v", e, err)
	}
	if _, err := s.GetSecurityEntry(ctx, "CWE-327"); err == nil {
		t.Error("an entry dropped from the new version is still there")
	}
	if err := s.ReplaceKEV(ctx, "2026.11.09", []seckb.KEV{{CVE: "CVE-2026-1", Product: "P", CWEs: []string{"CWE-287", "CWE-290"}}}); err != nil {
		t.Fatal(err)
	}
	k, err := s.GetKEV(ctx, "cve-2026-1")
	if err != nil || k == nil || k.Product != "P" || len(k.CWEs) != 2 {
		t.Fatalf("kev = %+v %v", k, err)
	}
	if k, _ := s.GetKEV(ctx, "CVE-0000-0"); k != nil {
		t.Error("a CVE not on the list was found")
	}
	srcs, _ := s.ListSecuritySources(ctx)
	if len(srcs) != 1 {
		t.Errorf("sources = %d", len(srcs))
	}
}
