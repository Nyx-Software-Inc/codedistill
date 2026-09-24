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

import "strings"

// How big a card is born.
//
// Moved out of the API layer because a second caller appeared and got it wrong
// by omission: accepting a decomposition proposal created scratchpad items with
// no geometry at all, so forty-nine of them arrived as zero-by-zero cards
// stacked on the same square. A shared estimator is how "every card has a
// sensible size" becomes true by construction rather than by each caller
// remembering.

// GridWidth matches the canvas grid and MUST equal the renderer's column count
// (ScratchpadGrid COLS). They diverged once — renderer 24, backend 12 — which
// made restack pack everything into the left half of the canvas.
const GridWidth = 24

// Estimator tuning (fit-to-content restack, and creation-time sizing). The numbers approximate the canvas card renderer:
// ~9 characters per grid column, ~2 text lines per grid row, one row of
// card chrome (title bar + padding). Estimates err small-and-resizable
// rather than precise — the user can always nudge a card.
const (
	CharsPerCol = 9
	LinesPerRow = 2

	// ChromeRows is what the card spends on furniture rather than text.
	//
	// Measured, not guessed: a card carries 10px of padding top and bottom
	// plus a row of icon buttons, about 42px against a 28px grid row. One row
	// therefore under-counted by half, and on a minimum-height card that left
	// roughly ELEVEN pixels for the content — which is why a new note could not
	// be identified without expanding it.
	ChromeRows = 2

	MinNaturalW = 4

	// MinNaturalH is the floor. Two rows was 56px total, nearly all of it
	// chrome. Three gives a short note about two readable lines, which is
	// enough to tell what it is.
	MinNaturalH = 3

	MaxNaturalH = 8  // beyond this, widen instead of growing taller
	MaxClampH   = 10 // absolute height cap even at full width
)

// NaturalCardSize estimates a card's grid size from its text content.
// Prefers narrow-and-tall over full-width: tries widths narrowest-first and
// takes the first whose wrapped height fits MaxNaturalH. Only truly long
// content goes full-width.
func NaturalCardSize(content string) (w, h int) {
	lines := strings.Split(content, "\n")
	heightAt := func(w int) int {
		wrapped := 0
		for _, l := range lines {
			n := (len(l) + w*CharsPerCol - 1) / (w * CharsPerCol)
			if n < 1 {
				n = 1
			}
			wrapped += n
		}
		h := (wrapped+LinesPerRow-1)/LinesPerRow + ChromeRows
		if h < MinNaturalH {
			h = MinNaturalH
		}
		return h
	}
	// Estimate width in a 12-column basis (the tuning constants above are
	// calibrated for it), then scale the chosen width up to the actual
	// GridWidth so proportions are preserved on the 24-column canvas.
	// Height is unaffected by the scale.
	const baseCols = 12
	scale := GridWidth / baseCols
	for _, w := range []int{MinNaturalW, 6, 8, baseCols} {
		if h := heightAt(w); h <= MaxNaturalH || w == baseCols {
			if h > MaxClampH {
				h = MaxClampH
			}
			return w * scale, h
		}
	}
	return GridWidth, MaxClampH // unreachable; loop always returns
}
