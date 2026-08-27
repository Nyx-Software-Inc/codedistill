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

// Package risk scores how much a recorded change deserves human attention
// (glass-box Phase 4, slice 1 — attention routing). The score is a transparent
// heuristic over signals we already have: blast-radius (code metrics), the
// verification verdict, and sensitivity (always-review zones). It is the input
// the earned-trust dial (slice 4.2) later weighs against a project's track
// record to decide auto-clear vs. escalate.
//
// Like the change-complexity heuristic in internal/git, the weights are
// judgment calls meant to be tuned with data — so the assessment always carries
// its reasons, never just a number.
package risk

// Band is the human-facing risk tier.
type Band string

const (
	Low      Band = "Low"
	Medium   Band = "Medium"
	High     Band = "High"
	Critical Band = "Critical"
)

// Inputs are the signals for one implemented item, all already available
// elsewhere (git metrics + verification results + zone config).
type Inputs struct {
	// ComplexityBand is the item's change-complexity band from git metrics
	// ("Low" | "Moderate" | "High" | "Very High"); "" when no metrics.
	ComplexityBand string
	// HasVerification is false when no check has ever run for the item — an
	// unverified change is itself a risk.
	HasVerification bool
	// DeterministicFailing: a test/scanner is failing or errored.
	DeterministicFailing bool
	// ReviewRefuted: the adversarial AI reviewer refuted a criterion.
	ReviewRefuted bool
	// InAlwaysReviewZone: the change touches a hand-tagged critical path
	// (auth/billing/migrations) — a non-negotiable escalation floor.
	InAlwaysReviewZone bool
	// Reverted: a later commit reverted this item's implementation commit —
	// the strongest real-world signal the change was wrong. A Critical floor.
	Reverted bool
}

// Assessment is the scored result with its rationale.
type Assessment struct {
	Score   int      `json:"score"`
	Band    Band     `json:"band"`
	Reasons []string `json:"reasons"`
}

// Point weights — tunable. Blast-radius scales with the complexity band; an
// unverified or failing change adds the most, since "we don't know it's safe"
// is the core reason to look. A clean, green verification earns a reduction.
const (
	ptModerate   = 15
	ptHigh       = 40
	ptVeryHigh   = 70
	ptUnverified = 30
	ptDetFail    = 45
	ptRefuted    = 20
	ptZone       = 25
	ptReverted   = 60
	ptGreenBonus = -15

	thMedium   = 25 // < thMedium → Low
	thHigh     = 50 // < thHigh → Medium
	thCritical = 85 // < thCritical → High, else Critical
)

// Score combines the signals into a risk assessment. An always-review zone
// floors the band to at least High regardless of the other signals.
func Score(in Inputs) Assessment {
	score := 0
	reasons := []string{}

	switch in.ComplexityBand {
	case "Moderate":
		score += ptModerate
		reasons = append(reasons, "moderate change size")
	case "High":
		score += ptHigh
		reasons = append(reasons, "large, high-complexity change")
	case "Very High":
		score += ptVeryHigh
		reasons = append(reasons, "very large / scattered change")
	}

	switch {
	case in.DeterministicFailing:
		score += ptDetFail
		reasons = append(reasons, "deterministic checks are failing")
	case !in.HasVerification:
		score += ptUnverified
		reasons = append(reasons, "unverified — no checks have run")
	default:
		score += ptGreenBonus
		reasons = append(reasons, "deterministic checks passing")
	}
	if in.ReviewRefuted {
		score += ptRefuted
		reasons = append(reasons, "AI review refuted a criterion")
	}
	if in.InAlwaysReviewZone {
		score += ptZone
		reasons = append(reasons, "touches an always-review zone")
	}
	if in.Reverted {
		score += ptReverted
		reasons = append(reasons, "shipped change was later reverted")
	}

	if score < 0 {
		score = 0
	}
	band := bandFor(score)
	// Critical-area floor: a zone hit always warrants eyes.
	if in.InAlwaysReviewZone && bandRank(band) < bandRank(High) {
		band = High
	}
	// A reverted change is ground truth that it was wrong — always Critical.
	if in.Reverted {
		band = Critical
	}
	return Assessment{Score: score, Band: band, Reasons: reasons}
}

func bandFor(score int) Band {
	switch {
	case score < thMedium:
		return Low
	case score < thHigh:
		return Medium
	case score < thCritical:
		return High
	default:
		return Critical
	}
}

func bandRank(b Band) int {
	switch b {
	case Low:
		return 0
	case Medium:
		return 1
	case High:
		return 2
	case Critical:
		return 3
	}
	return 0
}

// NeedsReview is the slice-4.1 policy: High or Critical warrants human eyes.
// (Slice 4.2 replaces this with risk-vs-earned-trust.)
func NeedsReview(b Band) bool { return bandRank(b) >= bandRank(High) }

// Rank exposes the band ordering so callers can sort a queue by severity.
func Rank(b Band) int { return bandRank(b) }
