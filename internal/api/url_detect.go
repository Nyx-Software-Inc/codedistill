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
	"regexp"
	"strconv"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// bareURLRE matches a single http(s) URL with no internal whitespace.
// Anchored on both ends so any surrounding prose (or a second URL on
// another line) disqualifies. Conservative: only http/https schemes
// (which is also what fetchOG accepts).
var bareURLRE = regexp.MustCompile(`^https?://[^\s]+$`)

// looksLikeBareURL reports whether content is a single http(s) URL
// and nothing else. Used by createItem to auto-upgrade
// content_type='link' when the user pastes a URL without specifying
// a type — that's what makes Slice 3's OpenGraph fetch reachable
// from the "paste-and-Enter" UI flow.
//
// Conservative on purpose:
//   - leading/trailing whitespace OK (the caller hasn't trimmed yet
//     in this hot path; we do it here)
//   - internal whitespace (URL embedded in prose) → false
//   - multiple URLs → false (the regex would fail on the second http
//     prefix even if separated by whitespace)
//   - non-http schemes → false (fetchOG doesn't accept them either)
func looksLikeBareURL(content string) bool {
	return bareURLRE.MatchString(strings.TrimSpace(content))
}

// URL auto-detect for code anchors. Scans item content for GitHub/GitLab
// permalinks and PR/MR URLs; inserts matching CodeAnchors with
// provenance="url-detected" on the owning Scratchpad_Item.
//
// The patterns are deliberately conservative — only full permalinks with an
// SHA in the path qualify as "file" anchors. Branch-name links would rot the
// moment the branch moves; we don't want those as pinned anchors.

var (
	// GitHub blob permalink: https://github.com/{owner}/{repo}/blob/{sha}/{path}(#L42(-L80)?)?
	// Only SHA-looking refs (7+ hex) are accepted to avoid pinning to branches.
	ghBlobRE = regexp.MustCompile(`https?://github\.com/[^/\s]+/[^/\s]+/blob/([0-9a-fA-F]{7,40})/([^\s#?)]+)(?:#L(\d+)(?:-L(\d+))?)?`)

	// GitHub PR: https://github.com/{owner}/{repo}/pull/{n}
	ghPullRE = regexp.MustCompile(`https?://github\.com/[^/\s]+/[^/\s]+/pull/\d+`)

	// GitLab blob permalink: https://gitlab.com/{group}/{repo}/-/blob/{sha}/{path}(#L42(-L80)?)?
	glBlobRE = regexp.MustCompile(`https?://gitlab\.com/[^/\s]+/[^/\s]+/-/blob/([0-9a-fA-F]{7,40})/([^\s#?)]+)(?:#L(\d+)(?:-(\d+))?)?`)

	// GitLab MR: https://gitlab.com/{group}/{repo}/-/merge_requests/{n}
	glMRRE = regexp.MustCompile(`https?://gitlab\.com/[^/\s]+/[^/\s]+/-/merge_requests/\d+`)
)

// detectAnchorsInContent returns the CodeAnchors implied by the given content,
// each with ID/timestamps/owner unset — callers populate those.
func detectAnchorsInContent(content string) []*domain.CodeAnchor {
	var out []*domain.CodeAnchor

	for _, m := range ghBlobRE.FindAllStringSubmatch(content, -1) {
		a := &domain.CodeAnchor{Kind: "file", Revision: m[1], Path: m[2], Provenance: "url-detected", URL: m[0]}
		if m[3] != "" {
			if n, err := strconv.Atoi(m[3]); err == nil {
				a.LineStart = n
			}
		}
		if m[4] != "" {
			if n, err := strconv.Atoi(m[4]); err == nil {
				a.LineEnd = n
			}
		}
		out = append(out, a)
	}
	for _, m := range ghPullRE.FindAllString(content, -1) {
		out = append(out, &domain.CodeAnchor{Kind: "pr", URL: m, Provenance: "url-detected"})
	}
	for _, m := range glBlobRE.FindAllStringSubmatch(content, -1) {
		a := &domain.CodeAnchor{Kind: "file", Revision: m[1], Path: m[2], Provenance: "url-detected", URL: m[0]}
		if m[3] != "" {
			if n, err := strconv.Atoi(m[3]); err == nil {
				a.LineStart = n
			}
		}
		if m[4] != "" {
			if n, err := strconv.Atoi(m[4]); err == nil {
				a.LineEnd = n
			}
		}
		out = append(out, a)
	}
	for _, m := range glMRRE.FindAllString(content, -1) {
		out = append(out, &domain.CodeAnchor{Kind: "pr", URL: m, Provenance: "url-detected"})
	}
	return out
}

// refreshDetectedAnchors rebuilds the url-detected anchor set for the given
// scratchpad item. User-set and file-dropped anchors are preserved; only
// url-detected ones are replaced. Call from createItem and updateItem.
func refreshDetectedAnchors(ctx context.Context, store storage.Storage, itemID, content string, now time.Time) error {
	existing, err := store.ListCodeAnchors(ctx, ownerScratchpadItem, itemID)
	if err != nil {
		return err
	}
	// Drop prior url-detected anchors; they are recomputed from current content.
	for _, a := range existing {
		if a.Provenance == "url-detected" {
			if err := store.DeleteCodeAnchor(ctx, a.ID); err != nil {
				return err
			}
		}
	}
	for _, a := range detectAnchorsInContent(content) {
		a.ID = id.New()
		a.OwnerType = ownerScratchpadItem
		a.OwnerID = itemID
		a.CreatedAt = now
		a.UpdatedAt = now
		if err := store.CreateCodeAnchor(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
