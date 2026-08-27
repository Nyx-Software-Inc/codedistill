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
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"codedistill/internal/exportbundle"
	"codedistill/internal/id"
)

// maxImportSize caps the upload size for /import. Generous enough for any
// realistic single-user bundle; tighten if abuse becomes a concern.
const maxImportSize = 50 * 1024 * 1024 // 50 MiB

// Export endpoints stream a zip bundle for backup, sharing, or moving content
// between machines. Two granularities: whole project, or single scratchpad.
// Format defined in internal/exportbundle.

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	project, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	b := s.bundler()
	data, err := b.Project(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	filename := fmt.Sprintf("codedistill-%s-%s.zip", filenameSlug(project.Name), bundleDateStamp())
	sendBundle(w, filename, data)
}

func (s *Server) exportScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	sp, err := s.store.GetScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	project, err := s.store.GetProject(r.Context(), sp.ProjectID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	b := s.bundler()
	data, err := b.Scratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	filename := fmt.Sprintf("codedistill-%s-%s-%s.zip",
		filenameSlug(project.Name), filenameSlug(sp.Name), bundleDateStamp())
	sendBundle(w, filename, data)
}

func (s *Server) bundler() *exportbundle.Bundler {
	return &exportbundle.Bundler{
		Store:           s.store,
		ExporterVersion: s.build.Version,
		ExporterSHA:     s.build.GitSHA,
		Now:             time.Now,
	}
}

// importBundle accepts a multipart/form-data upload with a "bundle" file
// field. Creates a new project named "<original> (imported)" with all
// child entities — see internal/exportbundle.Import for the full contract.
func (s *Server) importBundle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("read upload: %v", err))
		return
	}
	file, header, err := r.FormFile("bundle")
	if err != nil {
		writeMsg(w, http.StatusBadRequest,
			"missing form field 'bundle' (POST a multipart/form-data with the zip)")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("read upload: %w", err))
		return
	}
	if header != nil {
		s.log.Info("import: received bundle", "filename", header.Filename, "size", len(data))
	}

	res, err := exportbundle.Import(r.Context(), s.store, data, id.New, time.Now().UTC())
	if err != nil {
		var schemaErr *exportbundle.ErrSchemaTooNew
		if errors.As(err, &schemaErr) {
			writeMsg(w, http.StatusBadRequest, schemaErr.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func sendBundle(w http.ResponseWriter, filename string, data []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func bundleDateStamp() string {
	return time.Now().UTC().Format("20060102")
}

// slugRE collapses runs of non-alphanumeric characters into a single hyphen.
// Used for safe filenames; the user-facing project/scratchpad name still
// lives in the manifest, so import doesn't depend on the slug round-tripping.
var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func filenameSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "untitled"
	}
	return s
}
