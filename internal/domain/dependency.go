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

package domain

import "time"

// What has to happen before what.
//
// Priority says what matters; this says what is POSSIBLE YET. They are
// independent, and conflating them is how a backlog ends up sorted by
// importance with the top item unstartable.

// Dependency kinds. Two, deliberately.
const (
	// DependencyBlocks: To must be done before From can start.
	DependencyBlocks = "blocks"
	// DependencyInforms: From is easier once To exists, but not blocked.
	//
	// This exists so a model has somewhere to put a weak relationship. Given
	// only "blocks", anything asked to find dependencies will claim everything
	// blocks everything, and a graph where all 49 items block each other
	// carries exactly as much information as no graph at all.
	DependencyInforms = "informs"
)

// Origins. A human edge outranks a derived one and is never silently replaced
// by a later run over the same document.
const (
	DependencyFromHuman     = "human"
	DependencyFromDecompose = "decompose"
)

// ItemDependency is an edge between two pieces of real work.
//
// Kind-plus-id rather than a column per table, because dependencies cross
// kinds: a bug blocks a use case, a todo waits on a knowledge entry being
// written down.
type ItemDependency struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	FromKind string `json:"from_kind"`
	FromID   string `json:"from_id"`
	ToKind   string `json:"to_kind"`
	ToID     string `json:"to_id"`

	Kind string `json:"kind"`

	// Rationale and Lines are the evidence. An edge a human cannot check is an
	// edge they must take on faith, which is the opposite of the point.
	Rationale string   `json:"rationale,omitempty"`
	Lines     [][2]int `json:"lines,omitempty"`

	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`

	// Status is how a proposed edge waits for a human. A hand-drawn edge is
	// accepted the moment it is drawn; a derived one is not.
	//   'proposed' | 'accepted' | 'rejected'
	Status string `json:"status"`
	// JobID is the run that proposed it, empty when a person drew it.
	JobID string `json:"job_id,omitempty"`
}

// Edge standings.
//
// Three, and the important one is ADVISORY: a derived edge is in force without
// anyone having ratified it. That is the opposite of how proposals work here,
// and the reason is the cost of being wrong. A bad proposal becomes junk work
// somebody carries for months; a bad edge misorders a queue and is noticed the
// moment someone looks at the item. Making people ratify forty-eight edges
// before starting work costs more than the ordering is worth.
//
// Dismissed edges are KEPT, not deleted, so a later run does not re-offer one
// somebody has already argued with.
const (
	// EdgeAdvisory: derived, in force, nobody has looked at it.
	EdgeAdvisory = "advisory"
	// EdgeAccepted: a person drew it, or confirmed a derived one.
	EdgeAccepted = "accepted"
	// EdgeDismissed: a person disagreed. Remembered, never re-proposed.
	EdgeDismissed = "dismissed"
)

// InForce reports whether this edge actually constrains the order. Advisory and
// accepted both do; only a dismissal releases it.
func (d ItemDependency) InForce() bool {
	return d.Status == EdgeAdvisory || d.Status == EdgeAccepted
}

// ItemKinds that can carry a dependency.
var ItemKinds = []string{"use_case", "todo", "bug", "kb"}

func ValidItemKind(k string) bool {
	for _, x := range ItemKinds {
		if x == k {
			return true
		}
	}
	return false
}

func ValidDependencyKind(k string) bool {
	return k == DependencyBlocks || k == DependencyInforms
}

// Blocking reports whether this edge actually stops work starting. Only
// "blocks" does; "informs" is advice.
func (d ItemDependency) Blocking() bool { return d.Kind == DependencyBlocks }
