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

package sqlite

import (
	"fmt"
	"time"
)

// flexTimeFormats is the set of layouts we accept when parsing timestamps
// read back from SQLite (which stores them as text). It spans RFC3339, Go's
// time.Time.String() output in various locales, and go-git's commit-time
// rendering. Used by the dashboard + item-history readers.
var flexTimeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700 MST",      // time.Time.String() in zone-named locales
	"2006-01-02 15:04:05.999999999 -0700 MST m=+0", // ditto with monotonic clock (older Gos)
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700 -0700", // go-git commits: zone name == offset
	"2006-01-02 15:04:05 -0700 -0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseFlexibleTime walks flexTimeFormats and returns the first successful
// parse. Returns the zero time + a wrapped error when none match.
func parseFlexibleTime(s string) (time.Time, error) {
	// Strip Go's monotonic-clock suffix " m=+..." before trying — none of
	// the layouts above match it, and stripping is safe because the
	// monotonic clock is informational only.
	if idx := indexOf(s, " m=+"); idx >= 0 {
		s = s[:idx]
	} else if idx := indexOf(s, " m=-"); idx >= 0 {
		s = s[:idx]
	}
	for _, f := range flexTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("parseFlexibleTime: no layout matched %q", s)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
