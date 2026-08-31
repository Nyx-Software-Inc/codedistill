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
	"sort"
	"strings"

	"codedistill/internal/embed"
)

// Embedder is the search-side dependency on the embedding model. Same
// shape as agent.Embedder; declared here so internal/api doesn't have
// to import agent. Production wiring passes the same Ollama embed
// client that agent already uses.
type Embedder interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

// search runs an embedding-similarity search across the active project's
// items. Embeds the query via the same Ollama embedder the agent uses;
// scans every embedded row in the project; ranks by cosine similarity;
// returns the top N results with display fields the SPA can render
// directly. No round-trip to fetch full rows on click.

type searchRequest struct {
	Q         string `json:"q"`
	ProjectID string `json:"project_id"`
	Limit     int    `json:"limit"`
}

type searchHit struct {
	Kind           string  `json:"kind"`
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Snippet        string  `json:"snippet"`
	ScratchpadID   string  `json:"scratchpad_id,omitempty"`
	ScratchpadName string  `json:"scratchpad_name,omitempty"`
	Score          float32 `json:"score"`
}

type searchResponse struct {
	Hits    []searchHit `json:"hits"`
	Scanned int         `json:"scanned"` // number of candidates considered
}

const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
	// Minimum cosine score we'll surface. Below this the model couldn't
	// find anything meaningfully similar — returning low-score noise hurts
	// more than empty results. nomic-embed-text + reasonable queries
	// typically score ~0.5 on relevant matches and ~0.2 on unrelated.
	searchMinScore = 0.30
)

// search returns an http.HandlerFunc that closes over the embedder.
// Wired in server.go's route registration so the embedder dependency
// stays explicit at the boundary.
func (s *Server) search(embedder Embedder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(req.Q) == "" {
			writeMsg(w, http.StatusBadRequest, "q is required")
			return
		}
		if req.ProjectID == "" {
			writeMsg(w, http.StatusBadRequest, "project_id is required")
			return
		}
		limit := req.Limit
		if limit <= 0 {
			limit = searchDefaultLimit
		}
		if limit > searchMaxLimit {
			limit = searchMaxLimit
		}
		if embedder == nil {
			writeMsg(w, http.StatusServiceUnavailable, "search requires the embedder; check Ollama + nomic-embed-text")
			return
		}

		// 1. Embed the query.
		qVec, err := embedder.Embed(r.Context(), req.Q)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if len(qVec) == 0 {
			writeJSON(w, http.StatusOK, searchResponse{Hits: []searchHit{}})
			return
		}

		// 2. Pull all embedded rows in the project. Decode each blob;
		//    cosine vs. query vector.
		cands, err := s.store.ListSearchCandidates(r.Context(), req.ProjectID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		hits := make([]searchHit, 0, len(cands))
		for _, c := range cands {
			vec, err := embed.DecodeFloat32(c.Embedding)
			if err != nil || len(vec) == 0 {
				continue
			}
			score := embed.Cosine(qVec, vec)
			if score < searchMinScore {
				continue
			}
			hits = append(hits, searchHit{
				Kind:           string(c.Kind),
				ID:             c.ID,
				Title:          c.Title,
				Snippet:        c.Snippet,
				ScratchpadID:   c.ScratchpadID,
				ScratchpadName: c.ScratchpadName,
				Score:          score,
			})
		}

		// 3. Rank descending; cap at limit.
		sort.SliceStable(hits, func(i, j int) bool {
			return hits[i].Score > hits[j].Score
		})
		if len(hits) > limit {
			hits = hits[:limit]
		}

		writeJSON(w, http.StatusOK, searchResponse{Hits: hits, Scanned: len(cands)})
	}
}
