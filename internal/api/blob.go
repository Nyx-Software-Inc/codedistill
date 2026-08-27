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
	"errors"
	"fmt"
	"image"
	// Image-decoder registrations. DecodeConfig reads only the header bytes,
	// so dimension extraction is cheap. WebP comes from golang.org/x/image
	// (http.DetectContentType already sniffs the image/webp mime natively).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"codedistill/internal/blobstore"
	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/id"
)

// Grid defaults for new binary items. Match the MCP / text-item
// defaults so all canvas cards start the same shape; the user can
// resize after the fact.
const (
	blobDefaultGridW = 12
	blobDefaultGridH = 4
)

// blobSHAPattern matches a valid SHA-256 hex digest (64 lowercase hex
// chars). Used to validate path parameters before touching the store.
var blobSHAPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// uploadBlob handles POST /api/v1/scratchpads/{sid}/items/blob.
// Multipart upload of a single file under form field "file".
// Validates MIME against the allowlist, caps size at the configured
// limit, extracts pixel dimensions for images, stores in the blob
// store, and creates a scratchpad_item referencing the resulting sha.
// Returns the freshly-created item on 201.
//
// Failure modes:
//
//	413  upload exceeds MaxBytesPerItem
//	415  MIME not in AllowedMimes
//	400  missing/unreadable file field
//	404  scratchpad doesn't exist
//	503  no BlobStore wired
func (s *Server) uploadBlob(w http.ResponseWriter, r *http.Request) {
	if s.blobs == nil {
		writeMsg(w, http.StatusServiceUnavailable, "blob store not configured")
		return
	}
	sid := r.PathValue("sid")
	sp, err := s.store.GetScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}

	cfg := s.resolveBlobConfig(r.Context(), sid)
	uploaded := s.acceptBlobUpload(w, r, cfg)
	if uploaded == nil {
		return // response already written by helper
	}

	now := time.Now().UTC()
	nextRow, err := s.store.NextAvailableGridRow(r.Context(), sp.ID)
	if err != nil {
		// Best-effort cleanup of the orphan blob; the GC sweeper
		// (task #15) will pick up anything we miss.
		_ = s.blobs.Delete(r.Context(), uploaded.SHA)
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("grid row: %w", err))
		return
	}

	item := &domain.ScratchpadItem{
		ID:                  id.New(),
		ScratchpadID:        sp.ID,
		ContentType:         contentTypeForMime(uploaded.MimeType),
		Content:             "",
		ClassificationState: "unprocessed",
		Tags:                []string{},
		GridCol:             0,
		GridRow:             nextRow,
		GridW:               blobDefaultGridW,
		GridH:               blobDefaultGridH,
		BlobSHA:             uploaded.SHA,
		MimeType:            uploaded.MimeType,
		FileName:            uploaded.FileName,
		ByteSize:            uploaded.Size,
		Width:               uploaded.Width,
		Height:              uploaded.Height,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.store.CreateScratchpadItem(r.Context(), item); err != nil {
		_ = s.blobs.Delete(r.Context(), uploaded.SHA)
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("create item: %w", err))
		return
	}
	// Mirror createItem: a birth entry in the Log + live SSE so other clients
	// see the new card immediately, not on the 30s poll fallback (audit M25).
	s.recordEvent(r.Context(), "scratchpad_item", item.ID, "created", "Item created", sourceUI)
	s.bus.Publish(events.ItemsChanged)

	writeJSON(w, http.StatusCreated, item)
}

// getBlob handles GET /api/v1/blobs/{sha}. Streams the blob bytes
// with Content-Type sniffed from the first 512 bytes and aggressive
// caching headers (the URL is content-addressed; the bytes never
// change). Supports If-None-Match for 304 round-trips.
func (s *Server) getBlob(w http.ResponseWriter, r *http.Request) {
	if s.blobs == nil {
		writeMsg(w, http.StatusServiceUnavailable, "blob store not configured")
		return
	}
	sha := strings.ToLower(r.PathValue("sha"))
	if !blobSHAPattern.MatchString(sha) {
		writeMsg(w, http.StatusBadRequest, "sha must be 64 hex chars")
		return
	}

	// ETag short-circuit: the sha is the strongest possible ETag
	// since the URL itself is content-addressed. Accept both quoted
	// (RFC-conformant) and bare forms — browsers are consistent but
	// hand-written clients sometimes drop the quotes.
	etag := `"` + sha + `"`
	if m := r.Header.Get("If-None-Match"); m == etag || m == sha {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rc, err := s.blobs.Get(r.Context(), sha)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			writeMsg(w, http.StatusNotFound, "blob not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("blob get: %w", err))
		return
	}
	defer rc.Close()

	// Sniff MIME from the first 512 bytes, then prepend those bytes
	// back to the response stream. ReadFull may return < 512 for tiny
	// blobs; that's fine — http.DetectContentType handles short input.
	head := make([]byte, 512)
	n, _ := io.ReadFull(rc, head)
	head = head[:n]

	w.Header().Set("Content-Type", http.DetectContentType(head))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(head)
	_, _ = io.Copy(w, rc)
}

// acceptedBlob is the metadata extracted from a multipart upload
// after MIME sniffing, size enforcement, dimension decode, and a
// successful BlobStore.Put. Returned by acceptBlobUpload so two
// endpoints (item-creating uploadBlob + bare uploadBlobOnly) share
// the validation + storage path without duplicating it.
type acceptedBlob struct {
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	FileName string `json:"file_name,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// acceptBlobUpload parses + validates a multipart upload under the
// given BlobConfig, decodes image dimensions, and stores the bytes
// in the BlobStore. On any validation or storage error it writes
// the HTTP response and returns nil — the caller should just
// return without further action. Caller is responsible for the
// scratchpad / item creation downstream.
//
// Expects the multipart body's file field to be named "file".
func (s *Server) acceptBlobUpload(w http.ResponseWriter, r *http.Request, cfg BlobConfig) *acceptedBlob {
	// Cap the request body BEFORE parsing so a malicious client can't
	// blow out memory with a giant multipart envelope. +4KB grace
	// covers the multipart boundary + header overhead for a single
	// part.
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBytesPerItem+4096)
	if err := r.ParseMultipartForm(cfg.MaxBytesPerItem); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeMsg(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("upload exceeds %d bytes", cfg.MaxBytesPerItem))
			return nil
		}
		writeMsg(w, http.StatusBadRequest, "invalid multipart body")
		return nil
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeMsg(w, http.StatusBadRequest, "missing 'file' form field")
		return nil
	}
	defer file.Close()

	// Read the full body into memory. Safe given MaxBytesReader cap
	// above; for v1 a 10MiB ceiling is fine. If we ever lift the
	// ceiling we'd switch to a streaming approach (sniff first 512,
	// stream remainder through TeeReader into the store and a hasher).
	buf, err := io.ReadAll(file)
	if err != nil {
		writeMsg(w, http.StatusBadRequest, "failed to read upload")
		return nil
	}
	// Per-file size check. MaxBytesReader on the request body above
	// guards against multipart-envelope blowups, but the multipart
	// parser will happily pass through a single oversize file inside
	// a small envelope. This is the authoritative per-blob cap.
	if int64(len(buf)) > cfg.MaxBytesPerItem {
		writeMsg(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("upload exceeds %d bytes", cfg.MaxBytesPerItem))
		return nil
	}

	mimeType := http.DetectContentType(buf)
	if !mimeAllowed(cfg.AllowedMimes, mimeType) {
		writeMsg(w, http.StatusUnsupportedMediaType,
			fmt.Sprintf("mime %q not allowed", mimeType))
		return nil
	}

	// Dimension decode is best-effort: corrupt headers on otherwise-
	// allowlisted images leave width/height at zero rather than
	// rejecting the upload.
	var width, height int
	if cfg2, _, decErr := image.DecodeConfig(bytes.NewReader(buf)); decErr == nil {
		width, height = cfg2.Width, cfg2.Height
	}

	sha, size, err := s.blobs.Put(r.Context(), bytes.NewReader(buf))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("blob put: %w", err))
		return nil
	}

	return &acceptedBlob{
		SHA: sha, Size: size, MimeType: mimeType,
		FileName: header.Filename, Width: width, Height: height,
	}
}

// uploadBlobOnly handles POST /api/v1/blobs — upload bytes, get back
// the SHA + metadata, no scratchpad_item created. Optional
// ?scratchpad_id=<id> query param scopes the BlobConfig cascade to
// that scratchpad's project (so per-project overrides apply); if
// omitted, falls back to user/installation defaults.
//
// Used by the SketchEditor preview-upload flow (where the sketch
// item already exists and just needs its blob_sha patched) and by
// the future composite-doc image paste flow (where the upload
// happens during typing, not at item-create time).
//
// Returns 201 with JSON: { sha, size, mime_type, file_name,
// width, height }.
func (s *Server) uploadBlobOnly(w http.ResponseWriter, r *http.Request) {
	if s.blobs == nil {
		writeMsg(w, http.StatusServiceUnavailable, "blob store not configured")
		return
	}
	sid := r.URL.Query().Get("scratchpad_id") // optional
	cfg := s.resolveBlobConfig(r.Context(), sid)
	uploaded := s.acceptBlobUpload(w, r, cfg)
	if uploaded == nil {
		return // response already written
	}
	writeJSON(w, http.StatusCreated, uploaded)
}

// mimeAllowed is the strict whitelist check. Empty allowlist
// rejects everything (fail-closed). Matches are exact, case-sensitive
// — http.DetectContentType returns lowercase canonical types, so
// callers can pass lowercase whitelist entries safely.
func mimeAllowed(allowlist []string, mimeType string) bool {
	// DetectContentType may return "image/png; charset=utf-8"-style
	// values with parameters; strip everything after the first ';'
	// before comparing.
	base := mimeType
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	for _, m := range allowlist {
		if m == base {
			return true
		}
	}
	return false
}

// contentTypeForMime maps a MIME type to a scratchpad_item
// content_type slug. Images land as 'image'; any other allowlisted
// MIME (PDF, plain text, CSV, markdown, JSON, ZIP, …) lands as
// 'file'. Sketches (Slice 4) don't flow through this path — they're
// JSON in scratchpad_items.content, not blob bytes.
func contentTypeForMime(mimeType string) string {
	base := mimeType
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	if strings.HasPrefix(base, "image/") {
		return "image"
	}
	return "file"
}
