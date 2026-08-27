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
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"codedistill/internal/domain"
)

func TestLooksLikeBareURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Bare URLs — all should upgrade to content_type='link'.
		{"https://example.com", true},
		{"http://example.com", true},
		{"https://example.com/path?q=foo#frag", true},
		{"  https://example.com  ", true}, // leading/trailing whitespace OK
		{"\nhttps://example.com\n", true},  // newline whitespace OK
		{"https://example.com/a/b/c", true},

		// Not bare URLs — keep as text.
		{"", false},
		{"hello world", false},
		{"Check this out: https://example.com", false}, // surrounding prose
		{"https://example.com is cool", false},          // trailing prose
		{"https://a.com https://b.com", false},          // multiple URLs
		{"https://a.com\nhttps://b.com", false},         // multi-line URLs
		{"ftp://example.com", false},                    // unsupported scheme
		{"file:///etc/passwd", false},                   // unsupported scheme
		{"javascript:alert(1)", false},                  // unsupported scheme
		{"example.com", false},                          // no scheme
		{"//example.com", false},                        // scheme-relative
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := looksLikeBareURL(c.in)
			if got != c.want {
				t.Errorf("looksLikeBareURL(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestCreateItemAutoUpgradesBareURLToLink covers the integration
// point: a POST with content=URL and no content_type lands as a
// 'link' item, which then triggers the Slice 3 OG fetch goroutine.
// Verifies the upgrade happens, the fetch doesn't (we don't have an
// OG server here; we just check the content_type assignment).
func TestCreateItemAutoUpgradesBareURLToLink(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	body, _ := json.Marshal(map[string]any{
		"content": "https://example.com/some/page",
		// no content_type
	})
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var item domain.ScratchpadItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ContentType != "link" {
		t.Errorf("content_type = %q, want link (URL should auto-upgrade)", item.ContentType)
	}
	if item.Content != "https://example.com/some/page" {
		t.Errorf("content corrupted: %q", item.Content)
	}
}

// TestCreateItemExplicitTextTypeWinsOverURL: when the client
// explicitly passes content_type='text', a URL in content should
// NOT auto-upgrade — explicit choice wins.
func TestCreateItemExplicitTextTypeWinsOverURL(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	body, _ := json.Marshal(map[string]any{
		"content":      "https://example.com",
		"content_type": "text",
	})
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var item domain.ScratchpadItem
	json.NewDecoder(resp.Body).Decode(&item)
	if item.ContentType != "text" {
		t.Errorf("content_type = %q, want text (explicit choice must win)", item.ContentType)
	}
}

// TestCreateItemProseStaysText: URL embedded in surrounding prose
// is NOT a bare URL — should land as 'text'.
func TestCreateItemProseStaysText(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	body, _ := json.Marshal(map[string]any{
		"content": "Check this out: https://example.com/page",
	})
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var item domain.ScratchpadItem
	json.NewDecoder(resp.Body).Decode(&item)
	if item.ContentType != "text" {
		t.Errorf("content_type = %q, want text (URL with surrounding prose stays text)", item.ContentType)
	}
}

