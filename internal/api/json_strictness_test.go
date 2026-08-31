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
	"net/http/httptest"
	"testing"
	"time"

	"codedistill/internal/agent"
	"codedistill/internal/domain"
	"codedistill/internal/storage"
	"codedistill/internal/storage/sqlite"
)

// appendNote was the ONE handler out of 49 that decoded with a bare
// json.NewDecoder instead of readJSON, so it alone never set
// DisallowUnknownFields. It backs four routes — todos, bugs, use-cases, kb.
//
// Two user-visible consequences: an unknown field is accepted and silently
// dropped where every sibling endpoint 400s, and a client that sends the wrong
// key gets "note text is required" instead of being told which field is wrong,
// sending them after the wrong bug (CE-review item 22).
func TestAppendNote_RejectsUnknownFields(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	var td domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/p1/todos", map[string]string{"subject": "x"}, 201, &td)
	var bug domain.BugItem
	doJSON(t, srv, "POST", "/api/v1/projects/p1/bugs", map[string]string{"subject": "b"}, 201, &bug)
	// Use cases have no direct POST route — they arrive via the classifier — so
	// seed one through the store.
	uc := &domain.UseCaseItem{ID: "uc1", ProjectID: "p1", Subject: "u", Number: 1,
		Origin: "manual", CreatedAt: time.Now()}
	if err := store.CreateUseCaseItem(ctx, uc); err != nil {
		t.Fatal(err)
	}
	var kb domain.KnowledgeEntry
	doJSON(t, srv, "POST", "/api/v1/projects/p1/kb",
		map[string]string{"title": "k", "content": "c"}, 201, &kb)

	for _, tc := range []struct{ name, path string }{
		{"todos", "/api/v1/todos/" + td.ID + "/notes"},
		{"bugs", "/api/v1/bugs/" + bug.ID + "/notes"},
		{"use-cases", "/api/v1/use-cases/" + uc.ID + "/notes"},
		{"kb", "/api/v1/kb/" + kb.ID + "/notes"},
	} {
		t.Run(tc.name+"/unknown field rejected", func(t *testing.T) {
			doJSON(t, srv, "POST", tc.path,
				map[string]string{"text": "hello", "author": "rich"}, 400, nil)
		})
		t.Run(tc.name+"/valid note still accepted", func(t *testing.T) {
			doJSON(t, srv, "POST", tc.path, map[string]string{"text": "hello"}, 201, nil)
		})
	}
}

// dupOnCreate embeds storage.Storage so it inherits every method, and overrides
// only the seven create calls under test to report a duplicate. That is the
// point of the exercise: storage.ErrDuplicate is currently produced by
// wrapDupErr alone, whose non-test callers are the four entities that already
// answer 409 — so no real input reaches these seven handlers with it, and the
// bug is invisible until someone adds a UNIQUE constraint.
type dupOnCreate struct{ storage.Storage }

func (d dupOnCreate) CreateTodoItem(context.Context, *domain.TodoItem) error {
	return storage.ErrDuplicate
}
func (d dupOnCreate) CreateBugItem(context.Context, *domain.BugItem) error {
	return storage.ErrDuplicate
}
func (d dupOnCreate) CreateKnowledgeEntry(context.Context, *domain.KnowledgeEntry) error {
	return storage.ErrDuplicate
}
func (d dupOnCreate) CreateSkill(context.Context, *domain.Skill) error { return storage.ErrDuplicate }
func (d dupOnCreate) CreateCustomFieldDef(context.Context, *domain.CustomFieldDef) error {
	return storage.ErrDuplicate
}

// A duplicate is the caller's fault, so it must be 409. Answering 500 tells the
// client we broke, which sends them to our logs instead of their payload — and
// 500s are what retry loops and alerting react to (CE-review item 32).
func TestCreateHandlers_MapDuplicateTo409(t *testing.T) {
	base, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	if err := base.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	ag := agent.New(base, &stubClassifier{category: "BUG"}, agent.WithClock(time.Now))
	apiServer := NewServer(dupOnCreate{base}, ag, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil)
	srv := httptest.NewServer(apiServer.Handler())
	t.Cleanup(srv.Close)

	ctx := context.Background()
	if err := base.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path string
		body       map[string]string
	}{
		{"todos", "/api/v1/projects/p1/todos", map[string]string{"subject": "x"}},
		{"bugs", "/api/v1/projects/p1/bugs", map[string]string{"subject": "x"}},
		{"kb", "/api/v1/projects/p1/kb", map[string]string{"title": "x", "content": "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doJSON(t, srv, "POST", tc.path, tc.body, 409, nil)
		})
	}
}
