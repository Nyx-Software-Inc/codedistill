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

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/nldate"
	"codedistill/internal/nlmeta"
	"codedistill/internal/storage"
)

// deriveItem creates a Todo_Item, Bug_Item, Knowledge_Entry, or UseCaseItem
// from a Scratchpad_Item in the given category. Returns the new derived
// item's ID.
//
// Phase 1: minimal field extraction for TODO/BUG/KB. Subject/title comes from
// the first line or a truncated snippet; other fields take the schema defaults
// (severity minor, priority none, status open/incomplete). Full field
// extraction for those types is Req 23 / Phase 2.
//
// USE_CASE additionally consumes the agent-extracted role/want/why from the
// Classification (populated when the source conveys an "as a [role] I want X
// so that Y" shape). When `want` is populated, Subject derives from it
// (more meaningful as a capability label) instead of the first line of source.
// The original paste lives in Description either way.
//
// Code anchors attached to the source Scratchpad_Item are copied onto the
// derived item at creation time (propagation per the plan's
// "copy at creation, then diverge" rule). Propagation failure is logged by
// the caller but does not block derived-item creation.
func deriveItem(
	ctx context.Context,
	store storage.Storage,
	item *domain.ScratchpadItem,
	projectID string,
	result Classification,
	newID func() string,
	now time.Time,
) (string, error) {
	id := newID()
	subject := firstLineOrTruncate(item.Content, 250)

	// Capture-time extraction (item-editing redesign, slice 3): pull a
	// natural-language due date from the source text (e.g. "… by Tuesday") onto
	// the derived work record. Deterministic + conservative; KB has no due date
	// field, so it's simply unused there.
	var due *time.Time
	if d, ok := nldate.Parse(item.Content, now); ok {
		due = &d
	}

	// Backlog #11: priority/severity from EXPLICIT signals in the capture
	// ("URGENT", "p1", "typo"…), deterministic like the due date — populated
	// only when the text says it, blank (schema default) otherwise, always
	// user-overridable on the work record.
	priority := "none"
	if p, ok := nlmeta.Priority(item.Content); ok {
		priority = p
	}
	severity := "minor"
	if s, ok := nlmeta.Severity(item.Content); ok {
		severity = s
	}

	var (
		createErr    error
		dstOwnerType string
	)
	switch result.Category {
	case "TODO":
		dstOwnerType = "todo_item"
		createErr = store.CreateTodoItem(ctx, &domain.TodoItem{
			ID: id, ProjectID: projectID, SourceItemID: item.ID,
			Subject: subject, Priority: priority, Status: "incomplete",
			DueDate: due, Tags: item.Tags,
			Origin: "agent-derived", CreatedAt: now,
		})
	case "BUG":
		dstOwnerType = "bug_item"
		createErr = store.CreateBugItem(ctx, &domain.BugItem{
			ID: id, ProjectID: projectID, SourceItemID: item.ID,
			Subject: subject, Severity: severity, Status: "open",
			DueDate: due, Tags: item.Tags,
			Origin: "agent-derived", CreatedAt: now,
		})
	case "KB":
		dstOwnerType = "knowledge_entry"
		createErr = store.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{
			ID: id, ProjectID: projectID, SourceItemID: item.ID,
			Title: subject, Content: item.Content, Tags: item.Tags, CreatedAt: now,
		})
	case "USE_CASE":
		dstOwnerType = "use_case_item"
		// Subject from `want` when populated — reads as a capability label
		// ("export a project as a zip") rather than the user-story preamble
		// ("As a developer") that would land in `subject` from the first
		// line of source. Falls back to first-line behavior otherwise.
		ucSubject := subject
		if want := strings.TrimSpace(result.Want); want != "" {
			ucSubject = firstLineOrTruncate(want, 250)
		}
		createErr = store.CreateUseCaseItem(ctx, &domain.UseCaseItem{
			ID: id, ProjectID: projectID, SourceItemID: item.ID,
			// Same nlmeta extraction todos already get — `priority` is computed
			// above for every capture regardless of category, and the use-case
			// branch simply never used it (migration 0062).
			Priority:    priority,
			Subject:     ucSubject,
			Description: item.Content, // original paste preserved verbatim
			Role:        result.Role,
			Want:        result.Want,
			Why:         result.Why,
			Status:      "open",
			DueDate:     due,
			Tags:        item.Tags,
			Origin:      "agent-derived",
			CreatedAt:   now, UpdatedAt: now,
		})
	default:
		return "", fmt.Errorf("unknown category %q", result.Category)
	}
	if createErr != nil {
		return id, createErr
	}
	// Best-effort anchor propagation. A failure here should not roll back
	// derived-item creation; the user can re-anchor by hand if needed.
	_ = store.CopyCodeAnchors(ctx, "scratchpad_item", item.ID, dstOwnerType, id, newID, now)
	return id, nil
}

// derivedOwnerType maps a classification category to the code-anchor /
// acceptance-criteria owner_type for its derived item.
func derivedOwnerType(category string) string {
	switch category {
	case "TODO":
		return "todo_item"
	case "BUG":
		return "bug_item"
	case "KB":
		return "knowledge_entry"
	case "USE_CASE":
		return "use_case_item"
	}
	return ""
}

// firstLineOrTruncate derives a subject line: the first line of s, cut to at
// most max RUNES. Cuts at a word boundary when one exists in the back half
// (no mid-word stumps), and never splits a multibyte character (the old
// byte-slice could emit invalid UTF-8).
func firstLineOrTruncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := string(runes[:max])
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:-–—") + "…"
}
