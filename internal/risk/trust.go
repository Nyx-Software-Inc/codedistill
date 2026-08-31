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

package risk

import "fmt"

// TightenSettingKey is the project-scoped human override (a band name) that may
// only tighten the machine's earned threshold — escalate more, never less.
const TightenSettingKey = "review.tighten_to"

// Trust is a project's earned-trust level (glass-box Phase 4, slice 4.2),
// derived from EVIDENCE — its verification track record — not self-assessment.
// It sets how much risk the box auto-clears: a new project escalates almost
// everything; one with a long clean record escalates only the genuinely risky.
// The machine owns this ceiling — it never reaches "auto-clear everything"
// (Critical changes and always-review zones always escalate).
type Trust struct {
	Tier   string `json:"tier"` // "New" | "Building" | "Earned"
	Clean  int    `json:"clean"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"` // verified items = clean + failed
	// EscalateAtOrAbove is the lowest band that escalates at this trust: bands
	// at or above it need human eyes, below it auto-clear.
	EscalateAtOrAbove Band   `json:"escalate_at_or_above"`
	Explanation       string `json:"explanation"`
}

// Trust-tier evidence thresholds — tunable. A project earns autonomy only with
// both enough evidence (volume) and a high clean rate; a poor clean rate keeps
// it untrusting no matter the volume.
const (
	trustMinVolBuilding = 5
	trustMinVolEarned   = 15
	trustRateBuilding   = 0.7
	trustRateEarned     = 0.9
)

// AssessTrust computes the earned trust from the count of cleanly-verified vs.
// failed implemented items.
func AssessTrust(clean, failed int) Trust {
	total := clean + failed
	rate := 0.0
	if total > 0 {
		rate = float64(clean) / float64(total)
	}

	tier, band := "New", Medium
	switch {
	case total >= trustMinVolEarned && rate >= trustRateEarned:
		tier, band = "Earned", Critical
	case total >= trustMinVolBuilding && rate >= trustRateBuilding:
		tier, band = "Building", High
	}

	t := Trust{
		Tier: tier, Clean: clean, Failed: failed, Total: total,
		EscalateAtOrAbove: band,
	}
	t.Explanation = explainTrust(t, rate)
	return t
}

func explainTrust(t Trust, rate float64) string {
	autoCleared := "nothing"
	switch t.EscalateAtOrAbove {
	case High:
		autoCleared = "Low/Medium-risk"
	case Critical:
		autoCleared = "up to High-risk"
	}
	if t.Total == 0 {
		return "New — no verified changes yet; every implemented change is escalated for review."
	}
	return fmt.Sprintf("%s — cleanly verified %d of %d changes (%.0f%%); auto-clearing %s changes, escalating %s+. Critical changes and always-review zones always escalate.",
		t.Tier, t.Clean, t.Total, rate*100, autoCleared, t.EscalateAtOrAbove)
}

// Escalate decides whether a change needs human eyes at the given trust
// threshold. Always-review zones and Critical risk escalate regardless (the
// absolute floor — the dial never auto-clears those).
func Escalate(band Band, inZone bool, threshold Band) bool {
	if inZone || band == Critical {
		return true
	}
	return Rank(band) >= Rank(threshold)
}

// TightenedThreshold applies a human override that may only TIGHTEN (escalate
// more — a lower band) the machine's earned threshold, never loosen it. An empty
// or looser human band leaves the machine threshold in force.
func TightenedThreshold(machine, human Band) Band {
	if human != "" && Rank(human) < Rank(machine) {
		return human
	}
	return machine
}
