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

// Package embed holds the vector-storage primitives shared by the agent
// (which writes embeddings during classification) and the search layer
// (which reads them back). Vectors are stored as raw float32 BLOBs in
// SQLite; cosine similarity is computed in Go at search time.
//
// Solo-dev scale (typically <10k items per project) makes a linear scan
// the right default — measured throughput is ~hundreds of thousands of
// 768-dim cosine sims per second, so search returns in milliseconds.
// If a workspace ever needs more, the swap-in is sqlite-vec.
package embed

import (
	"encoding/binary"
	"errors"
	"math"
)

// EncodeFloat32 packs a slice of float32 into little-endian bytes for
// BLOB storage. Returns nil for an empty input so the storage layer can
// store SQL NULL rather than a zero-length blob.
func EncodeFloat32(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeFloat32 unpacks little-endian bytes back into a float32 slice.
// Returns nil for an empty input. Errors when the byte length is not
// a multiple of 4.
func DecodeFloat32(b []byte) ([]float32, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b)%4 != 0 {
		return nil, errors.New("embed: blob length not a multiple of 4")
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// Cosine returns the cosine similarity of two equal-length vectors. The
// result is in [-1, 1] (1 = identical direction). Returns 0 when either
// input is zero-length, mismatched length, or has zero magnitude — that
// way ranking code can use the value directly without special-casing.
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na))*math.Sqrt(float64(nb)))
}
