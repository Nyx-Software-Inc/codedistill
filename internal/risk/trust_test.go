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

import "testing"

func TestAssessTrustTiers(t *testing.T) {
	cases := []struct {
		clean, failed int
		tier          string
		band          Band
	}{
		{0, 0, "New", Medium},       // no evidence
		{3, 0, "New", Medium},       // too little volume
		{8, 2, "Building", High},    // enough volume, 80% clean
		{6, 4, "New", Medium},       // 60% clean — poor rate keeps it untrusting
		{20, 1, "Earned", Critical}, // high volume + ~95% clean
		{14, 0, "Building", High},   // 100% clean but just under the Earned volume bar
	}
	for _, c := range cases {
		tr := AssessTrust(c.clean, c.failed)
		if tr.Tier != c.tier || tr.EscalateAtOrAbove != c.band {
			t.Errorf("AssessTrust(%d,%d) = %s/%s, want %s/%s", c.clean, c.failed, tr.Tier, tr.EscalateAtOrAbove, c.tier, c.band)
		}
		if tr.Explanation == "" {
			t.Error("trust carried no explanation")
		}
	}
}

func TestEscalate(t *testing.T) {
	// At Earned trust (threshold Critical), a High change auto-clears...
	if Escalate(High, false, Critical) {
		t.Error("High should auto-clear at Earned trust")
	}
	// ...but a zone or a Critical always escalates.
	if !Escalate(High, true, Critical) {
		t.Error("zone change must always escalate")
	}
	if !Escalate(Critical, false, Critical) {
		t.Error("Critical must always escalate")
	}
	// At New trust (threshold Medium), a Medium change escalates.
	if !Escalate(Medium, false, Medium) {
		t.Error("Medium should escalate at New trust")
	}
	if Escalate(Low, false, Medium) {
		t.Error("Low should auto-clear even at New trust")
	}
}

func TestTightenedThreshold(t *testing.T) {
	// Human can tighten (escalate more — a lower band).
	if got := TightenedThreshold(High, Medium); got != Medium {
		t.Errorf("tighten High->Medium = %s, want Medium", got)
	}
	// Human cannot loosen past the machine ceiling.
	if got := TightenedThreshold(Medium, Critical); got != Medium {
		t.Errorf("loosen attempt should clamp to machine Medium, got %s", got)
	}
	// Empty human leaves the machine threshold.
	if got := TightenedThreshold(High, ""); got != High {
		t.Errorf("empty human = %s, want High", got)
	}
}
