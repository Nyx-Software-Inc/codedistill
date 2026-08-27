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
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codedistill/internal/storage"
)

// wrapDupErr translates a SQLite UNIQUE-constraint violation into the
// backend-agnostic storage.ErrDuplicate sentinel. All other errors pass
// through unchanged. Used by Create/Update methods on entities with
// uniqueness constraints (workspaces, projects, scratchpads, codebases —
// migration 0012). Lets the API layer errors.Is(err, storage.ErrDuplicate)
// without parsing driver-specific error strings.
func wrapDupErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return storage.ErrDuplicate
	}
	return err
}

// nullString returns a NULL when s is empty, otherwise the string. Used for
// columns with CHECK-constrained enums or foreign keys that must be NULL when unset.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// stringOrEmpty unwraps a sql.NullString, returning "" for NULL.
func stringOrEmpty(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// nullTimePtr converts a *time.Time into a sql.NullTime.
func nullTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}


// nullIntPtr converts a *int into a sql.NullInt64, NULL for nil. Unlike
// nullInt64Zero, a real 0 round-trips as 0 (not NULL) — needed for columns
// like a process exit code where 0 is a meaningful value, not "unset".
func nullIntPtr(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

// nullFloatZero returns NULL when f is exactly 0, otherwise the value.
// Mirrors nullIntZero — used where 0 stands in for "not set" and the
// schema is happier with NULL than a sentinel zero.
func nullFloatZero(f float64) sql.NullFloat64 {
	if f == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: f, Valid: true}
}

// floatOrZero unwraps a sql.NullFloat64, returning 0.0 for NULL.
func floatOrZero(nf sql.NullFloat64) float64 {
	if nf.Valid {
		return nf.Float64
	}
	return 0
}

// nullInt64Zero is the int64-typed sibling of nullIntZero in
// code_anchors.go. Returns NULL when n is exactly 0, otherwise the
// value. Used for int64 columns where 0 stands in for "not set"
// (e.g. byte_size on non-binary scratchpad items).
func nullInt64Zero(n int64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}

// intOrZero unwraps a sql.NullInt64, returning 0 for NULL.
func intOrZero(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

// marshalTags serializes a tag slice for storage. Nil and empty both round-trip as "[]"
// so callers never see null in JSON responses.
func marshalTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// unmarshalTags parses the JSON stored in the tags column. Unparseable or empty
// input returns an empty slice (never nil) so JSON responses are stable.
func unmarshalTags(raw string) []string {
	out := []string{}
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// flexTime is a sql.Scanner for timestamp columns that tolerates every
// shape the modernc driver can hand back. The driver stores time.Time
// bindings as text whose format depends on the value's zone (nameless
// zones render as "-0400 -0400", named ones as "-0400 EDT", UTC as
// "+0000 UTC"); its scan-side converter only parses a subset, returning
// the raw string for the rest — which sql.NullTime rejects. Backlog bug
// #5 (the wedged matcher cursor) was this; go-git commit dates flowing
// into completed_at / implementation_date via match-confirm can mint
// the same shapes. Strings parse through parseFlexibleTime.
//
// Use for both NOT NULL and nullable columns: ptr() for *time.Time
// fields, .t for required ones.
type flexTime struct {
	t     time.Time
	valid bool
}

func (f *flexTime) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		*f = flexTime{}
		return nil
	case time.Time:
		*f = flexTime{t: x, valid: true}
		return nil
	case string:
		t, err := parseFlexibleTime(x)
		if err != nil {
			return err
		}
		*f = flexTime{t: t, valid: true}
		return nil
	case []byte:
		t, err := parseFlexibleTime(string(x))
		if err != nil {
			return err
		}
		*f = flexTime{t: t, valid: true}
		return nil
	default:
		return fmt.Errorf("flexTime: unsupported driver type %T", v)
	}
}

// ptr returns the scanned time as a *time.Time (nil for NULL).
func (f flexTime) ptr() *time.Time {
	if !f.valid {
		return nil
	}
	t := f.t
	return &t
}
