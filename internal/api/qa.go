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
	"strings"

	"codedistill/internal/domain"
	"codedistill/internal/embed"
	"codedistill/internal/storage"
)

// Generator is the LLM-text dependency for the Q&A endpoint. Same shape
// as ollama.Client.Generate. Production wires the qwen2.5:7b client
// (already used for classification + mcp-suggest).
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// qa knobs. Retrieval is bounded so the prompt stays small enough for
// 7B context (~8K tokens generous budget). Snippet sizes match the
// storage-layer defaults for items / chunks search.
const (
	qaItemTopK        = 6
	qaChunkTopK       = 6
	qaItemMinScore    = 0.25
	qaChunkMinScore   = 0.25
	qaMaxItemSnippet  = 400 // chars per cited item
	qaMaxChunkSnippet = 800 // chars per cited code chunk
)

type qaRequest struct {
	Q         string `json:"q"`
	ProjectID string `json:"project_id"`
}

// qaCitation is a single piece of context the model could draw on.
// Round-trips to the SPA so the citation markers in the answer text
// can be made clickable + routed to the right surface.
type qaCitation struct {
	Marker       string `json:"marker"` // e.g. "item:abc" or "code:def"
	Kind         string `json:"kind"`   // item.kind or "code_chunk"
	ID           string `json:"id"`
	Title        string `json:"title"`
	FilePath     string `json:"file_path,omitempty"`
	LineStart    int    `json:"line_start,omitempty"`
	LineEnd      int    `json:"line_end,omitempty"`
	ScratchpadID string `json:"scratchpad_id,omitempty"`
}

type qaResponse struct {
	Answer     string       `json:"answer"`
	Citations  []qaCitation `json:"citations"`
	UsedItems  int          `json:"used_items"`
	UsedChunks int          `json:"used_chunks"`
}

// qa returns the Q&A handler. Closes over embedder + generator so the
// dependencies are explicit at the route registration.
func (s *Server) qa(embedder Embedder, generator Generator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if embedder == nil || generator == nil {
			writeMsg(w, http.StatusServiceUnavailable, "Q&A requires Ollama (both nomic-embed-text + qwen2.5:7b)")
			return
		}
		var req qaRequest
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

		// 1. Embed the question.
		qVec, err := embedder.Embed(r.Context(), req.Q)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if len(qVec) == 0 {
			writeJSON(w, http.StatusOK, qaResponse{Answer: "(empty question vector)"})
			return
		}

		// 2. Retrieve top items + top code chunks in parallel-ish (the
		//    storage queries are independent but sequential here for
		//    simplicity; both finish in milliseconds on solo-dev scale).
		itemCands, err := s.store.ListSearchCandidates(r.Context(), req.ProjectID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		items := rankItems(itemCands, qVec, qaItemMinScore, qaItemTopK)

		chunks, err := s.store.SearchCodeChunks(r.Context(), req.ProjectID, qVec, qaChunkMinScore, qaChunkTopK)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		// 3. Compose the prompt with the retrieved context. Citations
		//    travel back in the response so the SPA can render them.
		prompt, citations := buildQAPrompt(req.Q, items, chunks)

		if len(citations) == 0 {
			writeJSON(w, http.StatusOK, qaResponse{
				Answer: "I couldn't find anything in this project's items or code that looks relevant to that question.",
			})
			return
		}

		// 4. Generate.
		raw, err := generator.Generate(r.Context(), prompt)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}

		writeJSON(w, http.StatusOK, qaResponse{
			Answer:     strings.TrimSpace(raw),
			Citations:  citations,
			UsedItems:  len(items),
			UsedChunks: len(chunks),
		})
	}
}

// rankItems shares the cosine path with the search endpoint but stays
// inline here so the qa handler doesn't import a search helper that
// would otherwise need to be exported. Returns at most topK above
// minScore, descending by score.
type rankedItem struct {
	cand  storage.SearchCandidate
	score float32
}

func rankItems(cands []storage.SearchCandidate, qVec []float32, minScore float32, topK int) []rankedItem {
	out := make([]rankedItem, 0, len(cands))
	for _, c := range cands {
		vec, err := embed.DecodeFloat32(c.Embedding)
		if err != nil || len(vec) == 0 {
			continue
		}
		score := embed.Cosine(qVec, vec)
		if score < minScore {
			continue
		}
		out = append(out, rankedItem{cand: c, score: score})
	}
	// Insertion sort — small N, stable.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].score < out[j].score; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// buildQAPrompt formats the retrieved items + chunks into a structured
// prompt with citation markers. Returns the prompt string and the
// parallel citation list the SPA needs to render clickable references.
func buildQAPrompt(question string, items []rankedItem, chunks []domain.CodeChunkSearchHit) (string, []qaCitation) {
	var b strings.Builder
	citations := []qaCitation{}

	b.WriteString(`You are answering a question about a software project, given a curated context of items (notes / todos / bugs / KB / use cases) and code chunks the user has indexed.

RULES:
- Use ONLY the context below. If the context doesn't have enough to answer, say "I don't have enough context to answer that" — do not invent facts.
- Cite specific sources in your answer using the markers shown next to each context entry (e.g., [item:abc] or [code:def]). Cite generously; the user wants to verify.
- Keep the answer concise. Markdown allowed for lists / bold but no headings.
- Don't summarize the rules back to the user. Just answer.

Question: `)
	b.WriteString(question)
	b.WriteString("\n\nContext:\n\n")

	if len(items) > 0 {
		b.WriteString("ITEMS:\n")
		for _, it := range items {
			marker := fmt.Sprintf("item:%s", shortID(it.cand.ID))
			b.WriteString("[")
			b.WriteString(marker)
			b.WriteString("] ")
			b.WriteString(kindLabel(it.cand.Kind))
			b.WriteString(" — ")
			b.WriteString(it.cand.Title)
			b.WriteString("\n")
			snippet := it.cand.Snippet
			if len(snippet) > qaMaxItemSnippet {
				snippet = snippet[:qaMaxItemSnippet] + "…"
			}
			b.WriteString(snippet)
			b.WriteString("\n\n")
			citations = append(citations, qaCitation{
				Marker:       marker,
				Kind:         string(it.cand.Kind),
				ID:           it.cand.ID,
				Title:        it.cand.Title,
				ScratchpadID: it.cand.ScratchpadID,
			})
		}
	}

	if len(chunks) > 0 {
		b.WriteString("CODE:\n")
		for _, c := range chunks {
			marker := fmt.Sprintf("code:%s", shortID(c.ID))
			b.WriteString("[")
			b.WriteString(marker)
			b.WriteString("] ")
			b.WriteString(c.FilePath)
			b.WriteString(fmt.Sprintf(":%d-%d\n", c.LineStart, c.LineEnd))
			snippet := c.Snippet
			if len(snippet) > qaMaxChunkSnippet {
				snippet = snippet[:qaMaxChunkSnippet] + "…"
			}
			b.WriteString("```\n")
			b.WriteString(snippet)
			b.WriteString("\n```\n\n")
			citations = append(citations, qaCitation{
				Marker:    marker,
				Kind:      "code_chunk",
				ID:        c.ID,
				Title:     fmt.Sprintf("%s:%d-%d", c.FilePath, c.LineStart, c.LineEnd),
				FilePath:  c.FilePath,
				LineStart: c.LineStart,
				LineEnd:   c.LineEnd,
			})
		}
	}

	b.WriteString("Answer:")
	return b.String(), citations
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func kindLabel(t storage.EmbeddableTable) string {
	switch t {
	case storage.TableScratchpadItems:
		return "Item"
	case storage.TableTodoItems:
		return "Todo"
	case storage.TableBugItems:
		return "Bug"
	case storage.TableKnowledgeEntries:
		return "KB"
	case storage.TableUseCaseItems:
		return "Use Case"
	}
	return string(t)
}
