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

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codedistill/internal/domain"
)

type fakeWatchStore struct {
	projects []*domain.Project
	dirs     map[string]string // projectID -> watch dir
	upserts  int
}

func (f *fakeWatchStore) ListProjects(context.Context) ([]*domain.Project, error) { return f.projects, nil }
func (f *fakeWatchStore) GetProjectSetting(_ context.Context, pid, key string) (*domain.ProjectSetting, error) {
	if key == WatchDirSettingKey {
		if d, ok := f.dirs[pid]; ok {
			v, _ := json.Marshal(d)
			return &domain.ProjectSetting{ProjectID: pid, Key: key, Value: v}, nil
		}
	}
	return nil, nil
}
func (f *fakeWatchStore) UpsertCodeFinding(context.Context, *domain.CodeFinding) (bool, error) {
	f.upserts++
	return true, nil
}
func (f *fakeWatchStore) ResolveStaleFindings(context.Context, string, string, map[string]bool, time.Time) (int, error) {
	return 0, nil
}
func (f *fakeWatchStore) CreateAnalysisScan(context.Context, *domain.AnalysisScan) error { return nil }

func TestWatcherSweep(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "results.sarif"), []byte(gosecSARIF), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &fakeWatchStore{
		projects: []*domain.Project{{ID: "p1"}},
		dirs:     map[string]string{"p1": dir},
	}
	w := NewWatcher(fs, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	w.sweep(context.Background())
	if fs.upserts != 1 {
		t.Fatalf("first sweep: want 1 upsert, got %d", fs.upserts)
	}
	// An unchanged file is not re-ingested.
	w.sweep(context.Background())
	if fs.upserts != 1 {
		t.Errorf("unchanged file re-ingested: upserts=%d", fs.upserts)
	}
	// A project with no watch dir is skipped (no panic, no ingest).
	fs2 := &fakeWatchStore{projects: []*domain.Project{{ID: "p2"}}, dirs: map[string]string{}}
	NewWatcher(fs2, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).sweep(context.Background())
	if fs2.upserts != 0 {
		t.Errorf("unconfigured project ingested: %d", fs2.upserts)
	}
}
