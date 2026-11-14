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

package seckb

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// SourceRow is what is kept about one source between refreshes.
type SourceRow struct {
	ID           string
	LastAttempt  *time.Time
	LastSuccess  *time.Time
	LastError    string
	Failures     int    // consecutive failed refreshes, for back-off
	Version      string // the source's own version of what is stored
	LastModified string // its Last-Modified, for the next conditional download
	Entries      int    // how many entries are stored from it
}

// Status is the row as State and Due read it.
func (r SourceRow) Status() Status {
	return Status{LastAttempt: r.LastAttempt, LastSuccess: r.LastSuccess, LastError: r.LastError}
}

// Store keeps the knowledge base. Replacing a source's entries is one
// transaction: a reader sees the old version or the new, never half of each.
type Store interface {
	GetSecuritySource(ctx context.Context, id string) (*SourceRow, error) // nil, nil when never seen
	SaveSecuritySource(ctx context.Context, r *SourceRow) error
	ReplaceSecurityEntries(ctx context.Context, source, version string, es []Entry) error
	ReplaceKEV(ctx context.Context, version string, ks []KEV) error
}

// Refresher brings the sources up to date on their schedules.
type Refresher struct {
	Store  Store
	Client *http.Client
	Log    *slog.Logger
	Now    func() time.Time
	// Enabled says whether refreshing may reach the network. Off on an
	// air-gapped install, which imports knowledge packs instead; staleness
	// is tracked the same way either way.
	Enabled func() bool
}

// RunOnce refreshes every source that is due, and returns how many it
// refreshed. A failure is recorded on its source and the others still run.
func (f *Refresher) RunOnce(ctx context.Context) int {
	if f.Enabled != nil && !f.Enabled() {
		return 0
	}
	n := 0
	for _, s := range Sources {
		row, err := f.Store.GetSecuritySource(ctx, s.ID)
		if err != nil {
			f.log().Warn("security knowledge: could not read source state", "source", s.ID, "err", err)
			continue
		}
		if row == nil {
			row = &SourceRow{ID: s.ID}
		}
		if !Due(s, row.Status(), f.now(), row.Failures) {
			continue
		}
		if f.refresh(ctx, s, row) {
			n++
		}
	}
	return n
}

// RunAll refreshes every source now, whatever its schedule: the person asked.
func (f *Refresher) RunAll(ctx context.Context) int {
	if f.Enabled != nil && !f.Enabled() {
		return 0
	}
	n := 0
	for _, s := range Sources {
		row, err := f.Store.GetSecuritySource(ctx, s.ID)
		if err != nil {
			continue
		}
		if row == nil {
			row = &SourceRow{ID: s.ID}
		}
		if f.refresh(ctx, s, row) {
			n++
		}
	}
	return n
}

// refresh downloads, parses and stores one source. A failed download or a
// file that does not parse leaves what is stored untouched: stale data the
// person is told about is better than none.
func (f *Refresher) refresh(ctx context.Context, s Source, row *SourceRow) bool {
	now := f.now()
	row.LastAttempt = &now
	fail := func(err error) bool {
		row.LastError, row.Failures = err.Error(), row.Failures+1
		f.log().Warn("security knowledge: refresh failed", "source", s.ID, "err", err)
		_ = f.Store.SaveSecuritySource(context.WithoutCancel(ctx), row)
		return false
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	body, lastMod, err := Fetch(ctx, client, s, row.LastModified)
	switch {
	case errors.Is(err, ErrNotModified):
		// Unchanged is a successful refresh: the data is as current as the source.
	case err != nil:
		return fail(err)
	default:
		version, count, err := f.store(ctx, s, body)
		if err != nil {
			return fail(err)
		}
		row.Version, row.Entries, row.LastModified = version, count, lastMod
	}
	row.LastSuccess, row.LastError, row.Failures = &now, "", 0
	if err := f.Store.SaveSecuritySource(context.WithoutCancel(ctx), row); err != nil {
		f.log().Warn("security knowledge: could not record a refresh", "source", s.ID, "err", err)
	}
	return true
}

func (f *Refresher) store(ctx context.Context, s Source, body []byte) (string, int, error) {
	switch s.ID {
	case "cwe":
		es, v, err := ParseCWE(body)
		if err != nil {
			return "", 0, err
		}
		return v, len(es), f.Store.ReplaceSecurityEntries(ctx, s.ID, v, es)
	case "kev":
		ks, v, err := ParseKEV(body)
		if err != nil {
			return "", 0, err
		}
		return v, len(ks), f.Store.ReplaceKEV(ctx, v, ks)
	}
	return "", 0, errors.New("no parser for source " + s.ID)
}

// Run refreshes on a timer until ctx ends: once at start, then hourly, each
// time doing only what is due.
func (f *Refresher) Run(ctx context.Context) {
	f.RunOnce(ctx)
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.RunOnce(ctx)
		}
	}
}

func (f *Refresher) now() time.Time {
	if f.Now != nil {
		return f.Now().UTC()
	}
	return time.Now().UTC()
}

func (f *Refresher) log() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.Default()
}
