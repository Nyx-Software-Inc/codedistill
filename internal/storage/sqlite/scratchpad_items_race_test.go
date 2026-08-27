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

package sqlite

import (
	"context"
	"testing"
	"time"
)

// A stale classifier write (loaded before the OG worker wrote) must NOT clobber
// the og_* fields, and vice versa. This is the lost-update race that reverted a
// summarized URL item back to the bare link: the classifier loads the item at
// the start of its multi-second LLM call, then full-row-wrote its stale copy on
// top of the OG worker's fields. Field-scoped UpdateClassification /
// UpdateOGMetadata touch disjoint columns, so both writers' fields survive
// regardless of which one commits last.
func TestClassificationAndOGWritesDontClobber(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s) // "si1", ContentType text, no OG yet

	// The classifier loaded the item at the START of its LLM call — before the
	// OG worker wrote anything, so this copy has empty og_* fields.
	staleForClassify, err := s.GetScratchpadItem(ctx, "si1")
	if err != nil {
		t.Fatalf("load stale classifier copy: %v", err)
	}

	// Meanwhile the OG worker fetched + wrote the OpenGraph fields.
	og, err := s.GetScratchpadItem(ctx, "si1")
	if err != nil {
		t.Fatalf("load og copy: %v", err)
	}
	now := fixedTime(t).Add(time.Minute)
	og.OGTitle = "IBM"
	og.OGDescription = "International Business Machines"
	og.OGImageSHA = "abc123"
	og.OGFetchedAt = &now
	og.UpdatedAt = now
	if err := s.UpdateOGMetadata(ctx, og); err != nil {
		t.Fatalf("UpdateOGMetadata: %v", err)
	}

	// The classifier finishes LATER and writes its stale copy (og_* still empty).
	// With field-scoped writes this must NOT blank the OG fields.
	staleForClassify.ClassificationState = "pending-review"
	staleForClassify.ProposedCategory = "kb"
	staleForClassify.ClassificationReasoning = "looks like a reference"
	staleForClassify.UpdatedAt = now.Add(time.Second)
	if err := s.UpdateClassification(ctx, staleForClassify); err != nil {
		t.Fatalf("UpdateClassification: %v", err)
	}

	got, err := s.GetScratchpadItem(ctx, "si1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// OG fields survive the later classifier write.
	if got.OGTitle != "IBM" || got.OGDescription != "International Business Machines" || got.OGImageSHA != "abc123" {
		t.Errorf("OG fields clobbered by classifier write: title=%q desc=%q sha=%q",
			got.OGTitle, got.OGDescription, got.OGImageSHA)
	}
	if got.OGFetchedAt == nil {
		t.Error("og_fetched_at cleared by classifier write")
	}
	// Classification fields applied.
	if got.ClassificationState != "pending-review" || got.ProposedCategory != "kb" {
		t.Errorf("classification not applied: state=%q cat=%q", got.ClassificationState, got.ProposedCategory)
	}
	// Content (owned by neither field-scoped writer) is untouched.
	if got.Content != "hello" {
		t.Errorf("content unexpectedly changed: %q", got.Content)
	}
}
