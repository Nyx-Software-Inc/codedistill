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

// Package seckb is CodeDistill's security knowledge base: published,
// citable sources kept locally and refreshed on a schedule, so the security
// agent reasons from them rather than from what a model remembers
// (docs/design/security-knowledge-base.md).
//
// This package fetches and parses the sources and says how fresh each is.
// Storing them, and the tools the agents search them with, live elsewhere.
package seckb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Source is one published source and its refresh schedule.
type Source struct {
	ID       string        // "cwe", "kev"
	Name     string        // as a person reads it
	URL      string        // what is downloaded
	Every    time.Duration // the schedule
	Grace    time.Duration // how far past due it may run before it is stale
	MaxBytes int64         // a download larger than this is refused, not read
}

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// Sources are the ones built so far, in the design's build order: CWE and
// CISA KEV first, the two simplest, to prove refresh, failure and staleness
// end to end.
var Sources = []Source{
	{ID: "cwe", Name: "MITRE CWE", URL: "https://cwe.mitre.org/data/xml/cwec_latest.xml.zip",
		Every: week, Grace: 3 * day, MaxBytes: 64 << 20},
	{ID: "kev", Name: "CISA Known Exploited Vulnerabilities", URL: "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json",
		Every: day, Grace: day, MaxBytes: 64 << 20},
}

// SourceByID finds a source.
func SourceByID(id string) (Source, bool) {
	for _, s := range Sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// States a source can be in.
const (
	StateCurrent  = "current"      // refreshed on schedule
	StateRetrying = "retrying"     // the last refresh failed; still within its grace
	StateStale    = "stale"        // overdue past its grace: flagged everywhere
	StateNever    = "never_synced" // not available yet
)

// Status is what is known about a source's refreshes.
type Status struct {
	LastAttempt *time.Time
	LastSuccess *time.Time
	LastError   string
}

// State says how fresh a source is. A source is stale once its last success
// is older than its schedule plus its grace, whatever the reason: failing,
// switched off, or the machine asleep. It is retrying when the last attempt
// failed but the data it has is not yet too old to use.
func State(s Source, st Status, now time.Time) string {
	if st.LastSuccess == nil {
		return StateNever
	}
	if now.Sub(*st.LastSuccess) > s.Every+s.Grace {
		return StateStale
	}
	if st.LastError != "" && st.LastAttempt != nil && st.LastAttempt.After(*st.LastSuccess) {
		return StateRetrying
	}
	return StateCurrent
}

// Due says whether a source should be refreshed now. A failed attempt is
// retried with back-off (an hour, doubling, at most the schedule), so a
// source that is down is not hammered and one that recovers is picked up.
func Due(s Source, st Status, now time.Time, failures int) bool {
	if st.LastSuccess == nil && st.LastAttempt == nil {
		return true
	}
	if st.LastError != "" && st.LastAttempt != nil {
		wait := time.Hour << min(failures, 10)
		if wait > s.Every {
			wait = s.Every
		}
		return now.Sub(*st.LastAttempt) >= wait
	}
	if st.LastSuccess != nil {
		return now.Sub(*st.LastSuccess) >= s.Every
	}
	return true
}

// ErrNotModified: the source has not changed since the last download.
var ErrNotModified = errors.New("not modified since the last download")

// Fetch downloads a source, or reports ErrNotModified when it has not
// changed since `since` (its Last-Modified from the last download, sent as
// If-Modified-Since). It returns the body and the source's Last-Modified.
func Fetch(ctx context.Context, client *http.Client, s Source, since string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "CodeDistill security knowledge refresh")
	if since != "" {
		req.Header.Set("If-Modified-Since", since)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, since, ErrNotModified
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("%s answered %s", s.URL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, s.MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > s.MaxBytes {
		return nil, "", fmt.Errorf("%s is larger than %d bytes; refused", s.URL, s.MaxBytes)
	}
	return b, resp.Header.Get("Last-Modified"), nil
}
