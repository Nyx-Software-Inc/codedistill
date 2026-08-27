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
	"net/http"
	"os"

	"codedistill/internal/datamodel"
	"codedistill/internal/git"
)

// Data-model / ER diagram (UC-45, slice 1). Deterministic and computed ON
// DEMAND from the project's SQL schema — no persistence, so it's always current
// and can't drift. Complements the (AI-drafted) architecture diagram: this one
// only draws what the SQL declares.

// maxDataModelBytes caps total SQL read for one request so a pathological repo
// can't blow up memory; real migration sets are a few hundred KB.
const maxDataModelBytes = 8 << 20

type dataModelResp struct {
	*datamodel.Model
	// RepoConfigured is false when the project has no repo_root — the client
	// shows "connect a repo" rather than "no schema found".
	RepoConfigured bool `json:"repo_configured"`
}

// getDataModel returns the ER model parsed from the project's SQL schema. Free
// read (mirrors the architecture GET). Empty tables + repo_configured=true means
// "a repo is set but no SQL schema was found" — the client says so plainly
// instead of drawing an empty diagram.
func (s *Server) getDataModel(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	proj, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	resp := dataModelResp{Model: &datamodel.Model{
		Tables: []datamodel.Table{}, Relations: []datamodel.Relation{},
		Sources: []string{}, Warnings: []string{},
	}}
	if proj.RepoRoot != "" {
		if repo, err := git.Open(proj.RepoRoot); err == nil {
			resp.RepoConfigured = true
			if paths, err := repo.Tree(); err == nil {
				resp.Model = datamodel.Parse(readSQLSources(proj.RepoRoot, datamodel.SelectSources(paths)))
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// readSQLSources reads the selected .sql files through a root-confined handle
// (G304/CWE-22 guard, same as the architecture reader), capping total bytes.
func readSQLSources(repoRoot string, paths []string) []datamodel.Source {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil
	}
	defer root.Close()
	out := make([]datamodel.Source, 0, len(paths))
	total := 0
	for _, p := range paths {
		if total > maxDataModelBytes {
			break
		}
		data, err := readWithinRoot(root, p)
		if err != nil {
			continue
		}
		total += len(data)
		out = append(out, datamodel.Source{Path: p, SQL: string(data)})
	}
	return out
}
