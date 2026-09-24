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

// Proposal statuses. Three outcomes, not two: a duplicate is not a rejection,
// and conflating them discards the citation instead of attaching it to the work
// that already exists.
const (
	ProposalPending  = "pending"
	ProposalAccepted = "accepted"
	ProposalRejected = "rejected"
	ProposalLinked   = "linked"
)

// Where a proposal came from.
const (
	OriginProse = "prose" // inferred from sentences
	OriginTable = "table" // read from a record table, fields and all
)

// DecomposeRun is what one decomposition learned about a document. Facts about
// the document, kept out of the jobs row so that table never grows a column per
// job type.
type DecomposeRun struct {
	JobID        string `json:"job_id"`
	ProjectID    string `json:"project_id"`
	SourceItemID string `json:"source_item_id"`
	SourceLabel  string `json:"source_label,omitempty"`

	// SourceHash is the document's identity — same bytes, same document. Size
	// and ModifiedAt are for a human judging a revision, not for matching.
	SourceHash  string     `json:"source_hash,omitempty"`
	SourceBytes int64      `json:"source_bytes,omitempty"`
	ModifiedAt  *time.Time `json:"modified_at,omitempty"`

	Sentences      int `json:"sentences"`
	ExcludedLines  int `json:"excluded_lines"`
	TablesRead     int `json:"tables_read"`
	TablesDeclined int `json:"tables_declined"`

	// TableOnly is the count of requirements the author wrote down that prose
	// extraction did not find. It is the only honest answer to "how would I
	// know something was missed".
	Corroborated int `json:"corroborated"`
	TableOnly    int `json:"table_only"`
	ProseOnly    int `json:"prose_only"`

	DeclinedNodes int       `json:"declined_nodes"`
	RejectedMoves int       `json:"rejected_moves"`
	InferredLinks int       `json:"inferred_links"`
	Model         string    `json:"model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// DecomposeProposal is a candidate work item. Deliberately not an Item: nothing
// exists in a scratchpad until a human accepts it, and the review surface's
// promise that "nothing exists yet" has to be literally true.
type DecomposeProposal struct {
	ID        string `json:"id"`
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`

	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`

	Origin       string `json:"origin"`
	Corroborated bool   `json:"corroborated"`
	InferredLink bool   `json:"inferred_link,omitempty"`
	ExternalRef  string `json:"external_ref,omitempty"`
	Priority     string `json:"priority,omitempty"`

	// Lines is the citation, computed before any model ran.
	Lines [][2]int `json:"lines"`

	Status        string     `json:"status"`
	CreatedItemID string     `json:"created_item_id,omitempty"`
	LinkedItemID  string     `json:"linked_item_id,omitempty"`
	RejectReason  string     `json:"reject_reason,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	DecidedBy     string     `json:"decided_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Pending reports whether the proposal is still awaiting a human.
func (p *DecomposeProposal) Pending() bool { return p.Status == ProposalPending }

// ValidProposalStatus mirrors the CHECK on decompose_proposals.status.
func ValidProposalStatus(s string) bool {
	switch s {
	case ProposalPending, ProposalAccepted, ProposalRejected, ProposalLinked:
		return true
	}
	return false
}

// ValidProposalOrigin mirrors the CHECK on decompose_proposals.origin.
func ValidProposalOrigin(o string) bool { return o == OriginProse || o == OriginTable }

// DecomposeMove is one persisted reading result. Stored per pass so an
// interrupted run keeps what it already read: four of five passes once
// completed, cost 24 minutes, and were all discarded because nothing was
// written until the end.
type DecomposeMove struct {
	ID       string `json:"id"`
	Sentence int    `json:"sentence"`
	Role     string `json:"role"`
	Label    string `json:"label"`
	// Target is nil when the model flagged a refinement it could not attach.
	// Distinct from 0, which is a real sentence.
	Target *int `json:"target,omitempty"`
}

// DecomposePass records that one reading window finished, whatever it found.
type DecomposePass struct {
	ProjectID   string    `json:"project_id"`
	SourceHash  string    `json:"source_hash"`
	WindowStart int       `json:"window_start"`
	WindowEnd   int       `json:"window_end"`
	Rejected    int       `json:"rejected"`
	CreatedAt   time.Time `json:"created_at"`
}

// DecomposeRunSummary is a run as a review queue entry: what was read, and how
// much of it is still waiting on a person.
type DecomposeRunSummary struct {
	JobID        string    `json:"job_id"`
	SourceItemID string    `json:"source_item_id"`
	SourceLabel  string    `json:"source_label"`
	Sentences    int       `json:"sentences"`
	Model        string    `json:"model,omitempty"`
	CreatedAt    time.Time `json:"created_at"`

	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
	Linked   int `json:"linked"`
}
