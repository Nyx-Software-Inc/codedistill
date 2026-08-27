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

func TestScoreBands(t *testing.T) {
	cases := []struct {
		name string
		in   Inputs
		want Band
		need bool
	}{
		{"clean small green", Inputs{ComplexityBand: "Low", HasVerification: true}, Low, false},
		{"unverified small", Inputs{ComplexityBand: "Low"}, Medium, false},
		{"big but green", Inputs{ComplexityBand: "High", HasVerification: true}, Medium, false},
		{"failing tests", Inputs{ComplexityBand: "Moderate", HasVerification: true, DeterministicFailing: true}, High, true},
		{"very large unverified + refuted", Inputs{ComplexityBand: "Very High", ReviewRefuted: true}, Critical, true},
		{"zone floors to High even when otherwise low", Inputs{ComplexityBand: "Low", HasVerification: true, InAlwaysReviewZone: true}, High, true},
		{"reverted forces Critical even when small + green", Inputs{ComplexityBand: "Low", HasVerification: true, Reverted: true}, Critical, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Score(c.in)
			if a.Band != c.want {
				t.Errorf("band = %q, want %q (score=%d, reasons=%v)", a.Band, c.want, a.Score, a.Reasons)
			}
			if NeedsReview(a.Band) != c.need {
				t.Errorf("needsReview = %v, want %v", NeedsReview(a.Band), c.need)
			}
			if len(a.Reasons) == 0 {
				t.Error("assessment carried no reasons")
			}
		})
	}
}

// A green verification reduces risk below an unverified one of the same size.
func TestGreenReducesRisk(t *testing.T) {
	green := Score(Inputs{ComplexityBand: "High", HasVerification: true})
	unver := Score(Inputs{ComplexityBand: "High"})
	if green.Score >= unver.Score {
		t.Errorf("green (%d) should score lower than unverified (%d)", green.Score, unver.Score)
	}
}
