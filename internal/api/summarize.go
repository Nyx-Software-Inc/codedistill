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
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/id"
)

// Web-page summarization (backlog item #6 / the AI-summary half of item #12):
// fetch a URL, extract its readable text, ask the local LLM for a summary, and
// drop it into the scratchpad as an item (summary + source link) that then
// classifies like any other paste — typically into KB. Rendering the page is
// deliberately out of scope (Rich: the summary + link is the key requirement).

// summaryMaxTextRunes caps how much page text we feed the model — enough for a
// faithful summary without blowing the context window on a long article.
const summaryMaxTextRunes = 12000

const summarizePrompt = `Summarize the following web page for someone capturing it as a reference note.
Write 3-6 sentences capturing the key points — what it is and why it matters. Plain prose, no preamble.

Title: %s
URL: %s

Page text:
<<<
%s
>>>`

func (s *Server) summarizeURL(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	sp, err := s.store.GetScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if s.generator == nil {
		writeMsg(w, http.StatusServiceUnavailable, "summarization needs a language model (no generator configured)")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		writeMsg(w, http.StatusBadRequest, "url is required")
		return
	}

	title, text, err := fetchPageText(r.Context(), rawURL)
	if err != nil {
		writeMsg(w, http.StatusBadGateway, "could not read the page: "+err.Error())
		return
	}
	if strings.TrimSpace(text) == "" {
		writeMsg(w, http.StatusBadGateway, "the page had no readable text to summarize")
		return
	}

	summary, err := s.generator.Generate(r.Context(), fmt.Sprintf(summarizePrompt, title, rawURL, text))
	if err != nil {
		writeMsg(w, http.StatusBadGateway, "summary generation failed: "+err.Error())
		return
	}
	summary = strings.TrimSpace(summary)

	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	b.WriteString(summary)
	b.WriteString("\n\nSource: ")
	b.WriteString(rawURL)

	now := time.Now().UTC()
	item := &domain.ScratchpadItem{
		ID: id.New(), ScratchpadID: sp.ID, ContentType: "text",
		Content: b.String(), ClassificationState: "unprocessed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateScratchpadItem(r.Context(), item); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if s.agent != nil {
		s.agent.Enqueue(item.ID)
	}
	// Mirror createItem: a birth entry in the Log + live SSE so other clients
	// see the new card immediately, not on the 30s poll fallback (audit M25).
	s.recordEvent(r.Context(), "scratchpad_item", item.ID, "created", "Item created", sourceUI)
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusCreated, item)
}

// fetchPageText fetches an http(s) URL and returns its <title> + readable body
// text (script/style/nav stripped), capped at summaryMaxTextRunes. Mirrors the
// fetch safety of fetchOG (timeout, redirect cap, size cap, html-only).
func fetchPageText(ctx context.Context, rawURL string) (title, text string, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= ogMaxRedirects {
				return fmt.Errorf("too many redirects (>%d)", ogMaxRedirects)
			}
			return nil
		},
	}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("User-Agent", "CodeDistill/page-summarizer")
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("status %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.HasPrefix(ct, "text/html") && !strings.HasPrefix(ct, "application/xhtml") {
		return "", "", fmt.Errorf("unexpected content-type %q", ct)
	}
	title, text = extractText(io.LimitReader(resp.Body, ogMaxBodyBytes))
	return title, text, nil
}

// extractText streams HTML and collects visible text + the <title>, skipping
// non-content elements. Runs bounded by summaryMaxTextRunes.
func extractText(body io.Reader) (title, text string) {
	z := html.NewTokenizer(body)
	var sb strings.Builder
	skip := 0 // depth inside script/style/noscript
	inTitle := false
	runes := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(title), strings.TrimSpace(sb.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "svg", "head":
				skip++
			case "title":
				inTitle = true
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "svg", "head":
				if skip > 0 {
					skip--
				}
			case "title":
				inTitle = false
			}
		case html.TextToken:
			t := strings.TrimSpace(string(z.Text()))
			if t == "" {
				continue
			}
			if inTitle && title == "" {
				title = t
				continue
			}
			if skip > 0 || runes >= summaryMaxTextRunes {
				continue
			}
			sb.WriteString(t)
			sb.WriteByte(' ')
			runes += len([]rune(t))
		}
	}
}
