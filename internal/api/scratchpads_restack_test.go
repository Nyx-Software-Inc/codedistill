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
	"reflect"
	"strings"
	"testing"
)

// These tests assume the 24-column grid (gridWidth). The geometries
// were rescaled when the backend was unified with the renderer's 24
// columns (CodeDestill_imports bug #2).

// TestSkylinePack_CompactsGapsPreservesOrder — gaps collapse; cards that
// were far below pack up into the first free slots (which, on a 24-col
// grid, all land on row 0).
func TestSkylinePack_CompactsGapsPreservesOrder(t *testing.T) {
	got := skylinePack([]gridGeom{{6, 2}, {6, 2}, {4, 1}, {4, 1}})
	want := []gridSpot{{0, 0}, {6, 0}, {12, 0}, {16, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestSkylinePack_FillsGapsUnderShortItems — a tall + short item fill a
// row (each half the 24-col canvas); the next item tucks into the dead
// space under the short one instead of starting a fresh shelf.
//
//	A(12x6) | B(12x2)
//	        | C(12x2)   <- fills under B, beside A
func TestSkylinePack_FillsGapsUnderShortItems(t *testing.T) {
	got := skylinePack([]gridGeom{{12, 6}, {12, 2}, {12, 2}})
	want := []gridSpot{{0, 0}, {12, 0}, {12, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestSkylinePack_FullWidthForcesColumn — full-width (24) items can only
// stack.
func TestSkylinePack_FullWidthForcesColumn(t *testing.T) {
	got := skylinePack([]gridGeom{{24, 4}, {24, 4}, {24, 4}})
	want := []gridSpot{{0, 0}, {0, 4}, {0, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestSkylinePack_OverwideAndZeroClamped — legacy data hygiene: an
// over-wide item clamps to the full 24-col row; a zero becomes 1x1.
func TestSkylinePack_OverwideAndZeroClamped(t *testing.T) {
	got := skylinePack([]gridGeom{{99, 1}, {0, 0}, {4, 1}})
	want := []gridSpot{{0, 0}, {0, 1}, {1, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestSkylinePack_Empty(t *testing.T) {
	if got := skylinePack(nil); len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

// TestNaturalSize — the estimator prefers narrow cards and only goes
// full-width for content that would otherwise overflow the height cap.
// Widths are on the 24-col grid (the 12-col estimate is scaled ×2).
func TestNaturalSize(t *testing.T) {
	const scale = gridWidth / 12 // 2
	cases := []struct {
		name    string
		content string
		wantW   int
		minH    int
		maxH    int
	}{
		{"tiny note", "fix the thing", minNaturalW * scale, minNaturalH, 3},
		{"few short lines", "a\nb\nc\nd", minNaturalW * scale, 2, 4},
		{"paragraph wraps narrow", strings.Repeat("word ", 40), minNaturalW * scale, 4, maxNaturalH},
		{"medium paste goes wider", strings.Repeat("lorem ipsum dolor sit amet ", 22), 6 * scale, 4, maxNaturalH},
		{"huge paste caps out", strings.Repeat("x", 4000), gridWidth, maxNaturalH, maxClampH},
	}
	for _, tc := range cases {
		w, h := naturalSize(tc.content)
		if w != tc.wantW {
			t.Errorf("%s: w = %d, want %d", tc.name, w, tc.wantW)
		}
		if h < tc.minH || h > tc.maxH {
			t.Errorf("%s: h = %d, want %d..%d", tc.name, h, tc.minH, tc.maxH)
		}
		if w < minNaturalW || w > gridWidth || h < minNaturalH || h > maxClampH {
			t.Errorf("%s: (%d,%d) outside clamps", tc.name, w, h)
		}
	}
}
