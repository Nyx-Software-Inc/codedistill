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
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"codedistill/internal/features"
)

// What the installation IS, as opposed to what is in it.
//
// Deliberately separate from the usage stats: that answers "how many KB entries
// do I have", this answers "what am I running, where, against which models, and
// how much disk is it using". They are different questions asked at different
// moments — the second one is asked when something is wrong.

// RuntimeInfo is the handful of facts only the process that parsed the flags
// knows. Passed in like BuildInfo rather than discovered, because a handler
// guessing at its own listen address or database path would be guessing.
type RuntimeInfo struct {
	// DBPath is the SQLite file. MUST be empty on Postgres: the DSN carries a
	// password, and this endpoint is read by a browser.
	DBPath string
	// BlobRoot is the resolved blob directory. Passed in rather than derived as
	// <db>.blobs, because $CODEDISTILL_BLOBS overrides it and a Postgres install
	// has no sibling directory at all.
	BlobRoot string
	// Addr is the listen address as configured, e.g. "127.0.0.1:8080".
	Addr string
	// Dialect is "sqlite" or "postgres".
	Dialect string
	// StartedAt is process start, for uptime.
	StartedAt time.Time
}

func (s *Server) WithRuntimeInfo(ri RuntimeInfo) *Server {
	if ri.StartedAt.IsZero() {
		ri.StartedAt = time.Now().UTC()
	}
	s.rt = ri
	return s
}

type systemBuild struct {
	Version   string   `json:"version"`
	GitSHA    string   `json:"git_sha"`
	BuildDate string   `json:"build_date"`
	Edition   string   `json:"edition"`
	Features  []string `json:"features"`
}

type systemRuntime struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"go_version"`
	CPUs          int    `json:"cpus"`
	PID           int    `json:"pid"`
	Addr          string `json:"addr"`
	StartedAt     string `json:"started_at"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

type systemStorage struct {
	Dialect       string `json:"dialect"`
	DatabasePath  string `json:"database_path,omitempty"`
	DatabaseBytes int64  `json:"database_bytes"`
	BlobPath      string `json:"blob_path,omitempty"`
	BlobBytes     int64  `json:"blob_bytes"`
	BlobFiles     int    `json:"blob_files"`
	DataDir       string `json:"data_dir,omitempty"`
}

type systemModels struct {
	Endpoint     string `json:"endpoint,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	EmbedModel   string `json:"embed_model,omitempty"`
	// Installed is what the configured endpoint reports, when it answers.
	Installed []string `json:"installed,omitempty"`
	// Reachable is false when the endpoint did not answer; the panel says so
	// rather than showing an empty list that looks like "no models".
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

// systemInfo reports the running installation.
//
// ADMIN ONLY, and gated as a whole rather than per-field. It exposes the
// database path, the listen address and the model endpoint — on a desktop
// install that is all yours (the 'local' user is the seeded workspace owner, so
// this is never in the way there), but on a shared server it is the filesystem
// layout, which is the same class of thing the file browser is gated for.
//
// If a member ever legitimately needs the version or the edition, that is a
// small public subset to split out — not a reason to leave the paths open.
func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeMsg(w, http.StatusForbidden, "system details are available to workspace owners and admins")
		return
	}

	now := time.Now().UTC()
	out := map[string]any{
		"build": systemBuild{
			Version:   s.build.Version,
			GitSHA:    s.build.GitSHA,
			BuildDate: s.build.BuildDate,
			Edition:   editionName(),
			Features:  features.EnabledNames(),
		},
		"runtime": systemRuntime{
			OS:            runtime.GOOS,
			Arch:          runtime.GOARCH,
			GoVersion:     runtime.Version(),
			CPUs:          runtime.NumCPU(),
			PID:           os.Getpid(),
			Addr:          s.rt.Addr,
			StartedAt:     s.rt.StartedAt.Format(time.RFC3339),
			UptimeSeconds: int64(now.Sub(s.rt.StartedAt).Seconds()),
		},
		"storage": s.storageInfo(),
		"models":  s.modelInfo(r),
	}
	writeJSON(w, http.StatusOK, out)
}

// editionName reads the edition off the live license status. "community" when
// there is no license at all, which is the honest label for a free install
// rather than leaving the field blank and making the panel look broken.
func editionName() string {
	st := features.LicenseStatus()
	if st == nil || st.License == nil || st.License.Edition == "" {
		return "community"
	}
	return st.License.Edition
}

func (s *Server) storageInfo() systemStorage {
	st := systemStorage{Dialect: s.rt.Dialect}
	if st.Dialect == "" {
		st.Dialect = "sqlite"
	}
	if s.setup != nil {
		st.DataDir = s.setup.DataDir
	}
	if s.rt.DBPath != "" {
		st.DatabasePath = s.rt.DBPath
		// The main file plus -wal and -shm: a WAL-mode database keeps a
		// write-ahead log that is routinely tens of megabytes, so reporting only
		// the .db file would understate what is actually on disk.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if fi, err := os.Stat(s.rt.DBPath + suffix); err == nil {
				st.DatabaseBytes += fi.Size()
			}
		}
	}
	// Postgres reports no size: pg_database_size would describe a database that
	// may be shared with other things, and the DSN is never echoed because it
	// carries a password.

	// Walked live, not cached: measured at 0.03s over a real store, so a cache
	// would trade a staleness bug — a wrong total right after an import — for
	// thirty milliseconds.
	if fi, err := os.Stat(s.rt.BlobRoot); s.rt.BlobRoot != "" && err == nil && fi.IsDir() {
		blobDir := s.rt.BlobRoot
		st.BlobPath = blobDir
		_ = filepath.WalkDir(blobDir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // unreadable entry: skip it rather than abandon the total
			}
			if info, e := d.Info(); e == nil {
				st.BlobBytes += info.Size()
				st.BlobFiles++
			}
			return nil
		})
	}
	return st
}

func (s *Server) modelInfo(r *http.Request) systemModels {
	m := systemModels{}
	if s.setup != nil {
		m.Endpoint = s.setup.Endpoint
		m.DefaultModel = s.setup.DefaultModel
		m.EmbedModel = s.setup.EmbedModel
	}
	if s.models == nil {
		return m
	}
	// Bounded: the panel must open even when the endpoint is a LAN box that is
	// switched off. Without a deadline this request would hang for the client's
	// whole timeout and the whole panel would appear broken.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	names, err := s.models(ctx)
	if err != nil {
		m.Error = strings.TrimSpace(err.Error())
		return m
	}
	m.Reachable = true
	m.Installed = names
	return m
}
