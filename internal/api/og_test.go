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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

func TestParseOGHappyPath(t *testing.T) {
	html := `<!doctype html>
<html><head>
<meta property="og:title" content="The Title">
<meta property="og:description" content="A description.">
<meta property="og:image" content="https://example.com/img.png">
</head><body><h1>page</h1></body></html>`
	og := parseOG(strings.NewReader(html))
	if og.Title != "The Title" {
		t.Errorf("title = %q, want The Title", og.Title)
	}
	if og.Description != "A description." {
		t.Errorf("description = %q", og.Description)
	}
	if og.ImageURL != "https://example.com/img.png" {
		t.Errorf("imageURL = %q", og.ImageURL)
	}
}

func TestParseOGAcceptsNameAttribute(t *testing.T) {
	// Some CMSes emit name= instead of property=.
	html := `<head><meta name="og:title" content="From name attr"></head>`
	og := parseOG(strings.NewReader(html))
	if og.Title != "From name attr" {
		t.Errorf("title = %q", og.Title)
	}
}

func TestParseOGStopsAtHeadEnd(t *testing.T) {
	// Meta tags in <body> after </head> must be ignored — OG spec
	// says they live in head, and stopping early saves IO.
	html := `<head>
<meta property="og:title" content="HeadTitle">
</head><body>
<meta property="og:title" content="BodyTitle">
</body>`
	og := parseOG(strings.NewReader(html))
	if og.Title != "HeadTitle" {
		t.Errorf("title = %q, want HeadTitle (body meta should be ignored)", og.Title)
	}
}

func TestParseOGEmptyPage(t *testing.T) {
	og := parseOG(strings.NewReader(`<html><head></head><body>nothing</body></html>`))
	if og == nil {
		t.Fatal("og is nil; should be empty struct")
	}
	if og.Title != "" || og.Description != "" || og.ImageURL != "" {
		t.Errorf("expected empty OGMetadata, got %+v", og)
	}
}

func TestParseOGFirstWins(t *testing.T) {
	// Duplicate OG tags — first wins, per "if out.Title == ''"
	// guard. Real sites occasionally have malformed duplicates.
	html := `<head>
<meta property="og:title" content="First">
<meta property="og:title" content="Second">
</head>`
	og := parseOG(strings.NewReader(html))
	if og.Title != "First" {
		t.Errorf("title = %q, want First", og.Title)
	}
}

func TestFetchOGHappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<head>
<meta property="og:title" content="HTTPTitle">
<meta property="og:description" content="HTTPDesc">
<meta property="og:image" content="https://cdn.example.com/x.png">
</head>`)
	}))
	defer ts.Close()

	og, err := fetchOG(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("fetchOG: %v", err)
	}
	if og.Title != "HTTPTitle" || og.Description != "HTTPDesc" {
		t.Errorf("unexpected og: %+v", og)
	}
	if og.ImageURL != "https://cdn.example.com/x.png" {
		t.Errorf("imageURL = %q", og.ImageURL)
	}
}

func TestFetchOGResolvesRelativeImage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<head>
<meta property="og:image" content="/hero.jpg">
</head>`)
	}))
	defer ts.Close()

	og, err := fetchOG(context.Background(), ts.URL+"/page/123")
	if err != nil {
		t.Fatalf("fetchOG: %v", err)
	}
	// Relative /hero.jpg should resolve against the page URL's host.
	if !strings.HasPrefix(og.ImageURL, ts.URL+"/hero.jpg") {
		t.Errorf("imageURL = %q, want absolute under %s", og.ImageURL, ts.URL)
	}
}

func TestFetchOGRejectsNon2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer ts.Close()

	if _, err := fetchOG(context.Background(), ts.URL); err == nil {
		t.Errorf("expected error for 404, got nil")
	}
}

func TestFetchOGRejectsNonHTMLContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"foo":"bar"}`)
	}))
	defer ts.Close()

	if _, err := fetchOG(context.Background(), ts.URL); err == nil {
		t.Errorf("expected error for json content-type, got nil")
	}
}

func TestFetchOGRejectsBadScheme(t *testing.T) {
	if _, err := fetchOG(context.Background(), "ftp://example.com/foo"); err == nil {
		t.Errorf("expected error for ftp scheme")
	}
}

func TestFetchOGRejectsTooManyRedirects(t *testing.T) {
	// Build a server that always redirects to itself with a counter,
	// will exceed ogMaxRedirects (3).
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ts.URL+"/next", http.StatusFound)
	}))
	defer ts.Close()

	if _, err := fetchOG(context.Background(), ts.URL); err == nil {
		t.Errorf("expected too-many-redirects error")
	}
}

// TestFetchAndStoreOGEndToEnd exercises the full pipeline:
// HTTP server returns OG HTML + a PNG at the og:image URL;
// fetchAndStoreOG fetches the metadata, downloads the image into
// the BlobStore, and updates the scratchpad_item. Verifies all four
// OG fields land + og_image_sha actually resolves to bytes in the
// blob store.
func TestFetchAndStoreOGEndToEnd(t *testing.T) {
	srv, store, blobs := setupWithBlobs(t)
	_ = srv // returned by helper but unused in this test
	seedScratchpadForBlobs(t, store, "sp1")

	// HTTP server serves the link page AND the og:image at /hero.png.
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hero.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(makePNG(t, 8, 6))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<head>
<meta property="og:title" content="Pipeline title">
<meta property="og:description" content="Pipeline description">
<meta property="og:image" content="%s/hero.png">
</head>`, ts.URL)
	}))
	defer ts.Close()

	// Seed a link item pointing at the test server.
	ctx := context.Background()
	item := &domain.ScratchpadItem{
		ID: "linkItem", ScratchpadID: "sp1",
		ContentType: "link", Content: ts.URL,
		ClassificationState: "unprocessed",
		Tags:                []string{},
		CreatedAt:           time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	// Build a Server with the right wiring just for the fetchAndStoreOG call.
	s := NewServer(store, nil, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).
		WithBlobStore(blobs, DefaultBlobConfig())
	s.fetchAndStoreOG(ctx, item.ID, ts.URL)

	got, err := store.GetScratchpadItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.OGTitle != "Pipeline title" {
		t.Errorf("og_title = %q", got.OGTitle)
	}
	if got.OGDescription != "Pipeline description" {
		t.Errorf("og_description = %q", got.OGDescription)
	}
	if got.OGImageSHA == "" {
		t.Errorf("og_image_sha empty; image fetch should have populated it")
	}
	if got.OGFetchedAt == nil {
		t.Errorf("og_fetched_at not stamped")
	}
	// The image must be retrievable from the blob store with the
	// recorded SHA.
	rc, err := blobs.Get(ctx, got.OGImageSHA)
	if err != nil {
		t.Fatalf("blob get %s: %v", got.OGImageSHA, err)
	}
	rc.Close()
}

// TestFetchAndStoreOGFailureStillStamps verifies that a fetch
// failure (404 here) still sets og_fetched_at so we don't re-fetch
// indefinitely. Empty title/description with non-nil fetched_at is
// the "we tried, no metadata" signal.
func TestFetchAndStoreOGFailureStillStamps(t *testing.T) {
	srv, store, blobs := setupWithBlobs(t)
	_ = srv
	seedScratchpadForBlobs(t, store, "sp1")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer ts.Close()

	ctx := context.Background()
	item := &domain.ScratchpadItem{
		ID: "linkBad", ScratchpadID: "sp1",
		ContentType: "link", Content: ts.URL,
		ClassificationState: "unprocessed",
		Tags:                []string{},
		CreatedAt:           time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	s := NewServer(store, nil, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).
		WithBlobStore(blobs, DefaultBlobConfig())
	s.fetchAndStoreOG(ctx, item.ID, ts.URL)

	got, _ := store.GetScratchpadItem(ctx, item.ID)
	if got.OGFetchedAt == nil {
		t.Errorf("og_fetched_at should be stamped on failure too")
	}
	if got.OGTitle != "" || got.OGImageSHA != "" {
		t.Errorf("failure case should leave title/image empty; got %+v", got)
	}
}
