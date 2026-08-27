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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractText(t *testing.T) {
	html := `<html><head><title>  My Page  </title>
		<style>.a{color:red}</style><script>var x=1;</script></head>
		<body><h1>Hello</h1><p>First paragraph.</p>
		<noscript>enable js</noscript>
		<p>Second &amp; last.</p></body></html>`
	title, text := extractText(strings.NewReader(html))
	if title != "My Page" {
		t.Errorf("title = %q, want My Page", title)
	}
	for _, want := range []string{"Hello", "First paragraph.", "Second & last."} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q; got %q", want, text)
		}
	}
	for _, bad := range []string{"color:red", "var x", "enable js"} {
		if strings.Contains(text, bad) {
			t.Errorf("text should not contain %q; got %q", bad, text)
		}
	}
}

func TestFetchPageText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>T</title></head><body><p>body words here</p></body></html>`))
	}))
	defer ts.Close()

	title, text, err := fetchPageText(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if title != "T" || !strings.Contains(text, "body words here") {
		t.Errorf("got title=%q text=%q", title, text)
	}
}

func TestFetchPageText_RejectsNonHTTP(t *testing.T) {
	if _, _, err := fetchPageText(context.Background(), "ftp://example.com"); err == nil {
		t.Error("expected error for non-http scheme")
	}
}
