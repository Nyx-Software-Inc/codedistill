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

package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"codedistill/internal/agent"
	"codedistill/internal/events"
)

// fakeGrouper records the request and returns a canned result, so the
// endpoint test exercises the HTTP layer without importing the paid
// internal/dedup impl (which the public mirror strips).
type fakeGrouper struct {
	lastReq                     agent.DedupScanRequest
	result                      *agent.DedupScanResult
	groupedItem, groupedPartner string
}

func (f *fakeGrouper) OnClassified(_ context.Context, _, _ string, _ []float32) {}
func (f *fakeGrouper) Scan(_ context.Context, req agent.DedupScanRequest) (*agent.DedupScanResult, error) {
	f.lastReq = req
	return f.result, nil
}
func (f *fakeGrouper) GroupPair(_ context.Context, itemID, partnerID string) error {
	f.groupedItem, f.groupedPartner = itemID, partnerID
	return nil
}

func TestDedupScanEndpoint(t *testing.T) {
	_, ag, store := setupWithStore(t)
	fake := &fakeGrouper{result: &agent.DedupScanResult{
		ItemsFlagged: 5, PadsScanned: 3,
		CrossPad: []agent.CrossPadMatch{{AID: "a", APad: "P1", BID: "b", BPad: "P2", Score: 0.97}},
	}}
	srv := httptest.NewServer(
		NewServer(store, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).
			WithDuplicateGrouper(fake).
			WithEventBus(events.NewBus()).
			Handler(),
	)
	t.Cleanup(srv.Close)

	var out struct {
		ItemsFlagged int `json:"items_flagged"`
		PadsScanned  int `json:"pads_scanned"`
		CrossPad     []struct {
			APad  string  `json:"a_pad"`
			BPad  string  `json:"b_pad"`
			Score float64 `json:"score"`
		} `json:"cross_pad"`
	}
	doJSON(t, srv, "POST", "/api/v1/dedup/scan",
		map[string]string{"scope": "project", "scope_id": "p1"}, 200, &out)
	if out.ItemsFlagged != 5 || out.PadsScanned != 3 || len(out.CrossPad) != 1 {
		t.Errorf("response = %+v", out)
	}
	if fake.lastReq.Scope != "project" || fake.lastReq.ScopeID != "p1" {
		t.Errorf("grouper saw %+v, want project/p1", fake.lastReq)
	}

	// Validation.
	doJSON(t, srv, "POST", "/api/v1/dedup/scan", map[string]string{"scope": "galaxy"}, 400, nil)
	doJSON(t, srv, "POST", "/api/v1/dedup/scan", map[string]string{"scope": "project"}, 400, nil)
	// global needs no scope_id.
	doJSON(t, srv, "POST", "/api/v1/dedup/scan", map[string]string{"scope": "global"}, 200, &out)
}

// TestDedupScanUnavailable: feature licensed (TestMain enables all) but
// no grouper wired → 503, not a panic.
func TestDedupScanUnavailable(t *testing.T) {
	srv, _, _ := setupWithStore(t)
	doJSON(t, srv, "POST", "/api/v1/dedup/scan",
		map[string]string{"scope": "global"}, 503, nil)
}
