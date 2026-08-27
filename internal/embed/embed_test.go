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

package embed

import (
	"math"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 0.5, -0.25, 1e-6, math.MaxFloat32, -math.MaxFloat32}
	b := EncodeFloat32(in)
	if len(b) != 4*len(in) {
		t.Fatalf("encoded length = %d, want %d", len(b), 4*len(in))
	}
	out, err := DecodeFloat32(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("decoded length = %d, want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("element %d: got %v, want %v", i, out[i], in[i])
		}
	}
}

func TestEncodeEmptyReturnsNil(t *testing.T) {
	if EncodeFloat32(nil) != nil {
		t.Errorf("encode(nil) should be nil")
	}
	if EncodeFloat32([]float32{}) != nil {
		t.Errorf("encode(empty) should be nil")
	}
}

func TestDecodeEmptyReturnsNil(t *testing.T) {
	v, err := DecodeFloat32(nil)
	if err != nil || v != nil {
		t.Errorf("decode(nil) = (%v, %v), want (nil, nil)", v, err)
	}
}

func TestDecodeRejectsBadLength(t *testing.T) {
	if _, err := DecodeFloat32([]byte{1, 2, 3}); err == nil {
		t.Errorf("3-byte input should error (not multiple of 4)")
	}
}

func TestCosineIdentical(t *testing.T) {
	v := []float32{1, 2, 3}
	if got := Cosine(v, v); math.Abs(float64(got-1)) > 1e-6 {
		t.Errorf("identical vectors: got %v, want ~1", got)
	}
}

func TestCosineOpposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	if got := Cosine(a, b); math.Abs(float64(got+1)) > 1e-6 {
		t.Errorf("opposite vectors: got %v, want ~-1", got)
	}
}

func TestCosineOrthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := Cosine(a, b); math.Abs(float64(got)) > 1e-6 {
		t.Errorf("orthogonal vectors: got %v, want ~0", got)
	}
}

func TestCosineEdgeCases(t *testing.T) {
	if Cosine(nil, nil) != 0 {
		t.Errorf("nil inputs should be 0")
	}
	if Cosine([]float32{1, 2}, []float32{1}) != 0 {
		t.Errorf("mismatched length should be 0")
	}
	if Cosine([]float32{0, 0, 0}, []float32{1, 2, 3}) != 0 {
		t.Errorf("zero vector should be 0")
	}
}
