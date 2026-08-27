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
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/agent"
	"codedistill/internal/blobstore"
	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// setupWithBlobs is setupWithStore that also wires a LocalStore +
// DefaultBlobConfig so the /blobs and upload endpoints are live.
// Returned BlobStore is rooted at a fresh tempdir per test.
func setupWithBlobs(t *testing.T) (*httptest.Server, *sqlite.Store, blobstore.BlobStore) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ag := agent.New(store, &stubClassifier{category: "BUG", reasoning: "_"},
		agent.WithClock(func() time.Time {
			return time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		}),
	)
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)

	bs, err := blobstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore: %v", err)
	}
	srv := httptest.NewServer(
		NewServer(store, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).
			WithBlobStore(bs, DefaultBlobConfig()).Handler(),
	)
	t.Cleanup(srv.Close)
	return srv, store, bs
}

// seedScratchpadForBlobs creates the project + scratchpad chain that
// the upload endpoint requires.
func seedScratchpadForBlobs(t *testing.T, s *sqlite.Store, sid string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := s.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: sid, ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
}

// makePNG produces a small valid PNG for upload tests. Width/height
// are picked so the dimension-extract path has something to scan.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// uploadFile builds a multipart upload of a single named file under
// the "file" field. Returns the request body + content-type header.
func uploadFile(t *testing.T, name string, content []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func TestUploadBlobHappyPath(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	body, contentType := uploadFile(t, "shot.png", makePNG(t, 32, 24))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, raw)
	}
	var item domain.ScratchpadItem
	if err := decodeJSON(resp.Body, &item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ContentType != "image" {
		t.Errorf("content_type = %q, want image", item.ContentType)
	}
	if item.MimeType != "image/png" {
		t.Errorf("mime_type = %q, want image/png", item.MimeType)
	}
	if item.Width != 32 || item.Height != 24 {
		t.Errorf("dims = %dx%d, want 32x24", item.Width, item.Height)
	}
	if item.BlobSHA == "" || len(item.BlobSHA) != 64 {
		t.Errorf("blob_sha = %q, want 64 hex chars", item.BlobSHA)
	}
	if item.FileName != "shot.png" {
		t.Errorf("file_name = %q, want shot.png", item.FileName)
	}
	if item.ByteSize == 0 {
		t.Errorf("byte_size should be > 0")
	}
	if item.ScratchpadID != "sp1" {
		t.Errorf("scratchpad_id = %q, want sp1", item.ScratchpadID)
	}

	// Round-trip: GET /api/v1/blobs/<sha> returns the same bytes
	// with image/png content-type and an ETag.
	resp2, err := srv.Client().Get(srv.URL + "/api/v1/blobs/" + item.BlobSHA)
	if err != nil {
		t.Fatalf("GET blob: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("blob status = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("blob content-type = %q, want image/png", ct)
	}
	if et := resp2.Header.Get("ETag"); et != `"`+item.BlobSHA+`"` {
		t.Errorf("etag = %q, want %q", et, `"`+item.BlobSHA+`"`)
	}
	gotBytes, _ := io.ReadAll(resp2.Body)
	if int64(len(gotBytes)) != item.ByteSize {
		t.Errorf("blob body size = %d, want %d", len(gotBytes), item.ByteSize)
	}

	// If-None-Match short-circuits to 304.
	req3, _ := http.NewRequest("GET", srv.URL+"/api/v1/blobs/"+item.BlobSHA, nil)
	req3.Header.Set("If-None-Match", `"`+item.BlobSHA+`"`)
	resp3, err := srv.Client().Do(req3)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", resp3.StatusCode)
	}
}

// TestUploadFileHappyPath covers Slice 2 — a non-image upload lands
// as content_type='file' rather than 'image', no width/height
// metadata, the rest of the blob fields still populate. Verifies the
// download endpoint serves with the right MIME.
func TestUploadFileHappyPath(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	// Plain text — http.DetectContentType returns "text/plain;
	// charset=utf-8" which the allowlist strips params from and
	// matches against "text/plain". Use enough bytes that the sniff
	// is unambiguous.
	const txt = "# heading\n\nsome notes about the thing\n\nmore notes\n"
	body, contentType := uploadFile(t, "notes.md", []byte(txt))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, raw)
	}
	var item domain.ScratchpadItem
	if err := decodeJSON(resp.Body, &item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ContentType != "file" {
		t.Errorf("content_type = %q, want file", item.ContentType)
	}
	if !strings.HasPrefix(item.MimeType, "text/plain") {
		t.Errorf("mime_type = %q, want prefix text/plain", item.MimeType)
	}
	if item.Width != 0 || item.Height != 0 {
		t.Errorf("dims = %dx%d, want 0x0 (non-image)", item.Width, item.Height)
	}
	if item.FileName != "notes.md" {
		t.Errorf("file_name = %q, want notes.md", item.FileName)
	}
	if item.ByteSize != int64(len(txt)) {
		t.Errorf("byte_size = %d, want %d", item.ByteSize, len(txt))
	}

	// Download the blob.
	resp2, err := srv.Client().Get(srv.URL + "/api/v1/blobs/" + item.BlobSHA)
	if err != nil {
		t.Fatalf("GET blob: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("blob status = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("blob content-type = %q, want text/plain prefix", ct)
	}
	got, _ := io.ReadAll(resp2.Body)
	if string(got) != txt {
		t.Errorf("blob body mismatch:\n got %q\nwant %q", got, txt)
	}
}

func TestUploadBlobRejectsBadMIME(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	// Arbitrary binary — http.DetectContentType returns
	// application/octet-stream for unrecognized bytes, which is NOT in
	// the default allowlist (and shouldn't be — it's the "I have no
	// idea what this is" fallback). Plain text USED to be the rejection
	// case but it's now allow-listed for Slice 2's file attachments.
	body, contentType := uploadFile(t, "mystery.bin", []byte{0x00, 0x01, 0x02, 0x03, 0x42, 0x00, 0xff, 0xfe})
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", resp.StatusCode)
	}
}

func TestUploadBlobRejectsOversize(t *testing.T) {
	// Configure a tiny limit (32 bytes) so we don't have to generate
	// a 10MiB body. A 64x64 PNG encodes to ~160 bytes — well over.
	srv := httptest.NewServer(buildServerForOverrides(t, BlobConfig{
		MaxBytesPerItem: 32,
		AllowedMimes:    []string{"image/png"},
		URLTTL:          time.Minute,
	}))
	t.Cleanup(srv.Close)

	body, contentType := uploadFile(t, "big.png", makePNG(t, 64, 64)) // ~160 bytes, well over 32
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 413; body=%s", resp.StatusCode, raw)
	}
}

func TestUploadBlobMissingFileField(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_, _ = mw.CreateFormField("wrong_field_name")
	_ = mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUploadBlobUnknownScratchpad(t *testing.T) {
	srv, _, _ := setupWithBlobs(t)
	body, contentType := uploadFile(t, "x.png", makePNG(t, 4, 4))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/nope/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetBlobValidatesSHA(t *testing.T) {
	srv, _, _ := setupWithBlobs(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/blobs/not-a-valid-sha")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetBlobNotFound(t *testing.T) {
	srv, _, _ := setupWithBlobs(t)
	// Valid sha shape, no such blob.
	missing := strings.Repeat("a", 64)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/blobs/" + missing)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestUploadBlobSettingsCascadeOverridesDefault wires a permissive
// installation default (10MiB) but seeds a project-scope override
// (32 bytes) and confirms the upload is rejected per the override.
// Exercises both halves of the cascade plumbing: the settings lookup
// and its application to the per-request limit.
func TestUploadBlobSettingsCascadeOverridesDefault(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	// Seed a project-scope override that's lower than the file's size.
	tightLimit, _ := json.Marshal(int64(32))
	if err := store.SetProjectSetting(context.Background(), &domain.ProjectSetting{
		ProjectID: "p1", Key: "blob.max_bytes_per_item",
		Value: tightLimit, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed project setting: %v", err)
	}

	body, contentType := uploadFile(t, "tight.png", makePNG(t, 64, 64)) // ~160 bytes, well over 32
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 413 (project setting should have overridden 10MiB default); body=%s",
			resp.StatusCode, raw)
	}
}

// TestUploadBlobSettingsCascadeAllowedMimes seeds a project-scope
// allowlist that includes only image/jpeg. Uploading a PNG (under
// the size cap) is then rejected with 415.
func TestUploadBlobSettingsCascadeAllowedMimes(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)
	seedScratchpadForBlobs(t, store, "sp1")

	jpegOnly, _ := json.Marshal([]string{"image/jpeg"})
	if err := store.SetProjectSetting(context.Background(), &domain.ProjectSetting{
		ProjectID: "p1", Key: "blob.allowed_mimes",
		Value: jpegOnly, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed project setting: %v", err)
	}

	body, contentType := uploadFile(t, "shot.png", makePNG(t, 16, 16))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/scratchpads/sp1/items/blob", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 415 (jpeg-only project setting should have rejected PNG); body=%s",
			resp.StatusCode, raw)
	}
}

// TestUploadBlobOnlyReturnsMetadataNoItem covers the new standalone
// POST /api/v1/blobs endpoint: bytes go in, sha + metadata come
// back, no scratchpad_item is created. Used by SketchEditor preview
// uploads and the future composite-doc image-paste flow.
func TestUploadBlobOnlyReturnsMetadataNoItem(t *testing.T) {
	srv, store, _ := setupWithBlobs(t)

	body, contentType := uploadFile(t, "preview.png", makePNG(t, 24, 16))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/blobs", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, raw)
	}
	var got acceptedBlob
	if err := decodeJSON(resp.Body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MimeType != "image/png" {
		t.Errorf("mime_type = %q, want image/png", got.MimeType)
	}
	if got.Width != 24 || got.Height != 16 {
		t.Errorf("dims = %dx%d, want 24x16", got.Width, got.Height)
	}
	if len(got.SHA) != 64 {
		t.Errorf("sha = %q, want 64 hex chars", got.SHA)
	}
	// No items should exist anywhere on this store — endpoint
	// stores bytes only.
	items, err := store.ListScratchpadItems(context.Background(), "any")
	if err == nil && len(items) > 0 {
		t.Errorf("unexpected items in store: %d", len(items))
	}
}

func TestUploadBlobOnlyRequiresBlobStore(t *testing.T) {
	// Mirror TestBlobEndpointsRequireBlobStore but for the new
	// standalone endpoint.
	store, _ := sqlite.Open(":memory:")
	t.Cleanup(func() { store.Close() })
	_ = store.Migrate(context.Background())
	ag := agent.New(store, &stubClassifier{category: "BUG"}, agent.WithClock(time.Now))
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)
	srv := httptest.NewServer(NewServer(store, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).Handler())
	t.Cleanup(srv.Close)

	body, contentType := uploadFile(t, "x.png", makePNG(t, 4, 4))
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/blobs", body)
	req.Header.Set("Content-Type", contentType)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestBlobEndpointsRequireBlobStore(t *testing.T) {
	// Server constructed WITHOUT WithBlobStore — upload/download
	// must 503.
	store, _ := sqlite.Open(":memory:")
	t.Cleanup(func() { store.Close() })
	_ = store.Migrate(context.Background())
	ag := agent.New(store, &stubClassifier{category: "BUG"}, agent.WithClock(time.Now))
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)
	srv := httptest.NewServer(NewServer(store, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).Handler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/api/v1/blobs/" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET status = %d, want 503", resp.StatusCode)
	}
}

// --- helpers local to this test file ---

func buildServerForOverrides(t *testing.T, cfg BlobConfig) http.Handler {
	t.Helper()
	store, _ := sqlite.Open(":memory:")
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := store.CreateProject(context.Background(), &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(context.Background(), &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	ag := agent.New(store, &stubClassifier{category: "BUG"}, agent.WithClock(time.Now))
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)
	bs, err := blobstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore: %v", err)
	}
	return NewServer(store, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil).
		WithBlobStore(bs, cfg).Handler()
}

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
