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

package codeanalysis

// Watched-directory producer: a project can point `analysis.watch_dir` at a
// folder its CI drops SARIF files into, and the watcher ingests them. It's a
// simple poll loop (no fsnotify dependency) — a file is (re)ingested whenever
// its mtime advances past the last time we processed it. Runs only when the
// Enterprise Analysis feature is licensed (gated at startup in cmd/codedistill).

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codedistill/internal/domain"
)

// WatchDirSettingKey is the per-project setting holding the watched directory.
const WatchDirSettingKey = "analysis.watch_dir"

// WatchStore is the storage slice the watcher needs (satisfied by storage.Storage).
type WatchStore interface {
	IngestStore
	ListProjects(ctx context.Context) ([]*domain.Project, error)
	GetProjectSetting(ctx context.Context, projectID, key string) (*domain.ProjectSetting, error)
}

// Watcher polls each project's watch directory for *.sarif files and ingests
// changed ones (source "watched").
type Watcher struct {
	store    WatchStore
	log      *slog.Logger
	interval time.Duration
	notify   func() // optional: called after an ingest (e.g. SSE nudge)
	seen     map[string]time.Time
}

// NewWatcher builds a watcher polling every 30s. notify may be nil.
func NewWatcher(store WatchStore, log *slog.Logger, notify func()) *Watcher {
	return &Watcher{store: store, log: log, interval: 30 * time.Second, notify: notify, seen: map[string]time.Time{}}
}

// Run polls until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	w.sweep(ctx) // once at startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.sweep(ctx)
		}
	}
}

// sweep ingests any new/changed SARIF file across all projects' watch dirs.
func (w *Watcher) sweep(ctx context.Context) {
	projects, err := w.store.ListProjects(ctx)
	if err != nil {
		w.log.Warn("analysis watcher: list projects failed", "err", err)
		return
	}
	for _, p := range projects {
		dir := w.watchDir(ctx, p.ID)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			w.log.Warn("analysis watcher: read dir failed", "project", p.ID, "dir", dir, "err", err)
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".sarif") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			abs := filepath.Join(dir, e.Name())
			if last, ok := w.seen[abs]; ok && !info.ModTime().After(last) {
				continue // already ingested this version
			}
			if w.ingestFile(ctx, p.ID, abs) {
				w.seen[abs] = info.ModTime()
			}
		}
	}
}

func (w *Watcher) ingestFile(ctx context.Context, projectID, path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		w.log.Warn("analysis watcher: read file failed", "path", path, "err", err)
		return false
	}
	findings, err := ParseSARIF(data, "")
	if err != nil {
		w.log.Warn("analysis watcher: parse sarif failed", "path", path, "err", err)
		return false
	}
	newCount, resolved, err := Ingest(ctx, w.store, projectID, "watched", "ingest", time.Now().UTC(), findings, 0, nil, nil)
	if err != nil {
		w.log.Warn("analysis watcher: ingest failed", "path", path, "err", err)
		// still mark seen — a persistent bad file shouldn't loop forever
	}
	w.log.Info("analysis watcher: ingested", "project", projectID, "file", filepath.Base(path),
		"findings", len(findings), "new", newCount, "resolved", resolved)
	if w.notify != nil {
		w.notify()
	}
	return true
}

// watchDir reads the project's configured watch directory (empty when unset).
func (w *Watcher) watchDir(ctx context.Context, projectID string) string {
	st, err := w.store.GetProjectSetting(ctx, projectID, WatchDirSettingKey)
	if err != nil || st == nil {
		return ""
	}
	var dir string
	if json.Unmarshal(st.Value, &dir) != nil {
		return ""
	}
	return strings.TrimSpace(dir)
}
