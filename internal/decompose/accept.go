// =============================================================================
//
//	Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//	CodeDistill
//
//	Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//	Public License v3.0 (see the LICENSE file) and, separately, a commercial
//	license available from Nyx Software, Inc. Use outside the terms of one of those
//	licenses is prohibited.
//
//	SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
//
// =============================================================================
// Accepting a proposal — the moment something finally exists.
//
// Everything before this point is a candidate. Accept is what turns one into a
// real todo, bug, knowledge entry or use case, pointing back at the document it
// came from.
//
// The derived item's SourceItemID is the document, so the existing throughline
// works unchanged — an item derived from a spec is reachable from that spec
// exactly like one derived from a pasted note. What the item does NOT carry is
// the line span: that stays on the proposal, reachable through
// created_item_id, which avoids widening four tables with three columns each
// for a relationship one table already models.
//
// A narrow interface rather than storage.Storage, so this is testable without a
// database and so the package keeps its shallow dependencies.
package decompose

import (
	"context"
	"fmt"
	"strings"
	"time"

	"codedistill/internal/domain"
)

// originDerived is what the item tables' CHECK constraint allows, and it is
// accurate: an agent did derive this. The finer provenance — which document,
// which lines, which run — lives on the proposal row, reachable through
// created_item_id, rather than being bought with a migration that rewrites four
// tables to add one enum value.
const originDerived = "agent-derived"

// ItemStore is the slice of storage accepting needs.
type ItemStore interface {
	CreateScratchpadItem(ctx context.Context, i *domain.ScratchpadItem) error
	DeleteScratchpadItem(ctx context.Context, id string) error
	// NextAvailableGridRow keeps accepted work from landing on top of itself.
	NextAvailableGridRow(ctx context.Context, scratchpadID string) (int, error)
	CreateTodoItem(ctx context.Context, t *domain.TodoItem) error
	CreateBugItem(ctx context.Context, b *domain.BugItem) error
	CreateKnowledgeEntry(ctx context.Context, k *domain.KnowledgeEntry) error
	CreateUseCaseItem(ctx context.Context, u *domain.UseCaseItem) error
	DecideProposal(ctx context.Context, id, status, itemID, reason, userID string, at time.Time) error
}

// Accept creates the work item a proposal describes and records the decision.
// Returns the new item's id.
//
// Each accepted proposal gets its OWN base scratchpad item, exactly as a
// classified capture does. It used to point every derived item at the DOCUMENT
// as its source, which had two consequences: forty-one todos shared one base
// item, and none of them could be placed anywhere — a work item has no
// scratchpad of its own, so it surfaces wherever its base item lives, and
// theirs was the document's pad whether that made sense or not.
//
// Giving each one a base item is what makes "put the document in Docs and the
// work in Backlog" expressible, with no column added to four tables. It also
// makes a decomposed todo structurally identical to a classified one, rather
// than a second kind of todo that renders differently for reasons nobody
// remembers.
//
// Provenance is unaffected: which document, which lines, which run lives on the
// proposal row, reachable through created_item_id. source_item_id never carried
// the citation.
//
// Creation happens BEFORE the decision is recorded. If the process dies between
// them the proposal stays pending and a retry makes a second item — visible,
// and fixable by a human. The other order loses the item silently while
// claiming it was created, which nothing can detect later.
func Accept(ctx context.Context, st ItemStore, p *domain.DecomposeProposal, scratchpadID, userID string, newID func() string, now time.Time) (string, error) {
	if p == nil || !p.Pending() {
		return "", fmt.Errorf("accept: proposal is not pending")
	}
	if scratchpadID == "" {
		return "", fmt.Errorf("accept: no scratchpad to put the work in")
	}
	id := newID()
	subject := strings.TrimSpace(p.Subject)
	if subject == "" {
		return "", fmt.Errorf("accept: proposal %s has no subject", p.ID)
	}

	// The document's own priority wins when it wrote one down. A spec that says
	// P0 has said something the classifier would only have guessed at.
	priority := "none"
	if pr := normalisePriority(p.Priority); pr != "" {
		priority = pr
	}

	// The base scratchpad item, created FIRST so the work item can point at it.
	//
	// Shaped exactly like a classified capture: state "classified", the kind in
	// proposed_category, and derived_item_id linking to the work. A decomposed
	// todo is then the same object as a classified one, which is what lets every
	// existing per-scratchpad query and card renderer work on it unchanged.
	// Size and position, which a card does not get for free. Omitting them
	// created forty-nine items at zero-by-zero on the same square, invisible
	// except for a resize handle — a card is only a card because it has
	// geometry, and nothing downstream supplies it.
	content := baseContent(subject, p.Body)
	gw, gh := domain.NaturalCardSize(content)
	row, err := st.NextAvailableGridRow(ctx, scratchpadID)
	if err != nil {
		// Not fatal: stacking at the top is recoverable with one restack,
		// refusing to accept the work is not.
		row = 0
	}

	base := &domain.ScratchpadItem{
		ID: newID(), ScratchpadID: scratchpadID,
		Name: subject, ContentType: "text", Content: content,
		GridCol: 0, GridRow: row, GridW: gw, GridH: gh,
		ClassificationState: "classified",
		ProposedCategory:    p.Kind,
		ClassificationReasoning: fmt.Sprintf("derived from %s",
			citation(p.Lines)),
		DerivedItemID: id,
		CreatedAt:     now, UpdatedAt: now,
	}
	if err := st.CreateScratchpadItem(ctx, base); err != nil {
		return "", fmt.Errorf("accept %s: %w", p.ID, err)
	}
	sourceItemID := base.ID

	switch p.Kind {
	case "todo":
		err = st.CreateTodoItem(ctx, &domain.TodoItem{
			ID: id, ProjectID: p.ProjectID, SourceItemID: sourceItemID,
			Subject: subject, Priority: priority, Status: "incomplete",
			Origin: originDerived, CreatedAt: now,
		})
	case "bug":
		err = st.CreateBugItem(ctx, &domain.BugItem{
			ID: id, ProjectID: p.ProjectID, SourceItemID: sourceItemID,
			Subject: subject, Severity: "minor", Status: "open",
			Origin: originDerived, CreatedAt: now,
		})
	case "kb":
		body := p.Body
		if body == "" {
			body = subject
		}
		err = st.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{
			ID: id, ProjectID: p.ProjectID, SourceItemID: sourceItemID,
			Title: subject, Content: body, CreatedAt: now,
		})
	case "use_case":
		err = st.CreateUseCaseItem(ctx, &domain.UseCaseItem{
			ID: id, ProjectID: p.ProjectID, SourceItemID: sourceItemID,
			Subject: subject, Description: p.Body, Priority: priority,
			Status: "open", Origin: originDerived,
			CreatedAt: now, UpdatedAt: now,
		})
	default:
		return "", fmt.Errorf("accept: unknown kind %q", p.Kind)
	}
	if err != nil {
		// Roll the base item back rather than leaving a card that claims to
		// have derived work which does not exist — the same orphan the
		// classifier's own rollback exists to prevent.
		_ = st.DeleteScratchpadItem(ctx, base.ID)
		return "", fmt.Errorf("accept %s: %w", p.ID, err)
	}

	// userID, not empty: this records WHO turned a proposal into work. It was
	// blank while the only caller was a command line with no notion of a user,
	// and the review screen made that a gap — approving work is exactly the
	// kind of act a multi-user install has to be able to attribute.
	if err := st.DecideProposal(ctx, p.ID, domain.ProposalAccepted, id, "", userID, now); err != nil {
		// The item exists. Saying so is better than reporting a clean failure
		// and leaving an orphan nobody knows about.
		return id, fmt.Errorf("accept %s: item %s created but the decision was not recorded: %w", p.ID, id, err)
	}
	return id, nil
}

// normalisePriority maps what a document wrote onto the three levels
// CodeDistill has (domain.Priorities: high, medium, low).
//
// A numbered scheme is compressed rather than flattened: P0 and P1 must not
// both become "high", because a spec that bothered to distinguish them said
// something, and collapsing it discards the only prioritisation the author
// actually did. MoSCoW falls out of the same ordering.
//
// Unrecognised values return "" rather than a guess. A document using its own
// scheme should be left alone, not silently reinterpreted.
func normalisePriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "p0", "0", "critical", "highest", "urgent", "blocker", "must", "must have":
		return "high"
	case "p1", "1", "high", "should", "should have":
		return "medium"
	case "p2", "2", "medium", "normal", "could", "could have", "nice to have":
		return "low"
	case "p3", "p4", "3", "4", "low", "lowest", "trivial", "won't", "wont have", "would":
		return "low"
	}
	return ""
}

// baseContent is what the card shows. Subject alone reads as a title with no
// body; subject plus the elaboration reads as the thing the document said.
func baseContent(subject, body string) string {
	body = strings.TrimSpace(body)
	if body == "" || body == subject {
		return subject
	}
	return subject + "\n\n" + body
}

// citation renders the line spans for the base item's reasoning line, so the
// card says where it came from without a lookup.
func citation(lines [][2]int) string {
	if len(lines) == 0 {
		return "the document"
	}
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		if l[0] == l[1] {
			parts = append(parts, fmt.Sprintf("line %d", l[0]))
			continue
		}
		parts = append(parts, fmt.Sprintf("lines %d-%d", l[0], l[1]))
	}
	return strings.Join(parts, ", ")
}
