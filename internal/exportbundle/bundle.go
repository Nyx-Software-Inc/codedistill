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

// Package exportbundle marshals a Project (or single Scratchpad) into a
// portable zip archive. The format is hybrid Obsidian-style:
//
//	manifest.json                          — bundle metadata + counts
//	project.json                           — domain.Project record
//	scratchpads/<id>.json                  — domain.Scratchpad record
//	scratchpads/<id>/items/<item>.md       — raw content body (human-readable)
//	scratchpads/<id>/items/<item>.json     — item metadata; Content field is
//	                                         left empty in the JSON since the
//	                                         body lives in the .md sibling.
//	derived/todos/<id>.json                — full domain.TodoItem
//	derived/bugs/<id>.json                 — full domain.BugItem
//	derived/kb/<id>.json                   — full domain.KnowledgeEntry
//	derived/use_cases/<id>.json            — full domain.UseCaseItem
//	code-anchors.json                      — flat list of all anchors
//
// The format is forward-compatible with binary content: a future binary/<id>/
// directory holds blob payloads referenced from item JSON. Today's exports
// don't populate it because scratchpad_items.content_type is restricted to
// text|code_snippet|link.
package exportbundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// SchemaVersion is the bundle layout version. Bumped when the format changes
// in a way that would break older importers. v0.6.0 ships with 1.
const SchemaVersion = 1

// Manifest is the top-level descriptor. Importers read this first to verify
// schema version and decide how to interpret the rest of the bundle.
type Manifest struct {
	SchemaVersion   int       `json:"schema_version"`
	BundleKind      string    `json:"bundle_kind"` // "project" | "scratchpad"
	ExportedAt      time.Time `json:"exported_at"`
	ExporterVersion string    `json:"exporter_version,omitempty"`
	ExporterSHA     string    `json:"exporter_sha,omitempty"`
	ProjectID       string    `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	// Only populated when BundleKind == "scratchpad" — identifies the single
	// scratchpad whose contents this bundle holds.
	ScratchpadID   string `json:"scratchpad_id,omitempty"`
	ScratchpadName string `json:"scratchpad_name,omitempty"`
	Counts         Counts `json:"counts"`
}

// Counts is denormalized so a UI can show "Importing 3 scratchpads, 47 items,
// 12 todos, 5 bugs, 8 KB entries, 14 anchors" without unpacking everything.
type Counts struct {
	Scratchpads int `json:"scratchpads"`
	Items       int `json:"items"`
	Todos       int `json:"todos"`
	Bugs        int `json:"bugs"`
	KB          int `json:"kb"`
	UseCases    int `json:"use_cases,omitempty"`
	Anchors     int `json:"anchors"`
}

// Bundler walks the storage layer and writes a zip. Reusable across requests.
type Bundler struct {
	Store           storage.Storage
	ExporterVersion string // e.g. "0.6.0-dev" — recorded in the manifest
	ExporterSHA     string // e.g. "9e9958c"
	Now             func() time.Time
}

// Project bundles every scratchpad in the project plus all project-scoped
// derived items (manual + agent-derived) and every code anchor whose owner
// lives in this project. Returns the zip bytes.
func (b *Bundler) Project(ctx context.Context, projectID string) ([]byte, error) {
	project, err := b.Store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	scratchpads, err := b.Store.ListScratchpads(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list scratchpads: %w", err)
	}
	var allItems []*domain.ScratchpadItem
	for _, sp := range scratchpads {
		items, err := b.Store.ListScratchpadItems(ctx, sp.ID)
		if err != nil {
			return nil, fmt.Errorf("list items for %s: %w", sp.ID, err)
		}
		allItems = append(allItems, items...)
	}
	todos, err := b.Store.ListTodoItems(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	bugs, err := b.Store.ListBugItems(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list bugs: %w", err)
	}
	kb, err := b.Store.ListKnowledgeEntries(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list kb: %w", err)
	}
	useCases, err := b.Store.ListUseCaseItems(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list use_cases: %w", err)
	}
	anchors, err := b.collectAnchors(ctx, allItems, todos, bugs, kb, useCases)
	if err != nil {
		return nil, err
	}

	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		BundleKind:      "project",
		ExportedAt:      b.now(),
		ExporterVersion: b.ExporterVersion,
		ExporterSHA:     b.ExporterSHA,
		ProjectID:       project.ID,
		ProjectName:     project.Name,
		Counts: Counts{
			Scratchpads: len(scratchpads),
			Items:       len(allItems),
			Todos:       len(todos),
			Bugs:        len(bugs),
			KB:          len(kb),
			UseCases:    len(useCases),
			Anchors:     len(anchors),
		},
	}
	return writeBundle(manifest, project, scratchpads, allItems, todos, bugs, kb, useCases, anchors)
}

// Scratchpad bundles a single scratchpad: the scratchpad row, all of its
// items, only the derived items whose source_item_id lives in this
// scratchpad, and anchors for any of those owners. The originating Project
// record is included so the importer can name the new project something
// reasonable ("<original> (imported)") even when only one scratchpad lands.
func (b *Bundler) Scratchpad(ctx context.Context, scratchpadID string) ([]byte, error) {
	sp, err := b.Store.GetScratchpad(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("get scratchpad: %w", err)
	}
	project, err := b.Store.GetProject(ctx, sp.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	items, err := b.Store.ListScratchpadItems(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	todos, err := b.Store.ListTodoItemsByScratchpad(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	bugs, err := b.Store.ListBugItemsByScratchpad(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("list bugs: %w", err)
	}
	kb, err := b.Store.ListKnowledgeEntriesByScratchpad(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("list kb: %w", err)
	}
	useCases, err := b.Store.ListUseCaseItemsByScratchpad(ctx, scratchpadID)
	if err != nil {
		return nil, fmt.Errorf("list use_cases: %w", err)
	}
	anchors, err := b.collectAnchors(ctx, items, todos, bugs, kb, useCases)
	if err != nil {
		return nil, err
	}

	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		BundleKind:      "scratchpad",
		ExportedAt:      b.now(),
		ExporterVersion: b.ExporterVersion,
		ExporterSHA:     b.ExporterSHA,
		ProjectID:       project.ID,
		ProjectName:     project.Name,
		ScratchpadID:    sp.ID,
		ScratchpadName:  sp.Name,
		Counts: Counts{
			Scratchpads: 1,
			Items:       len(items),
			Todos:       len(todos),
			Bugs:        len(bugs),
			KB:          len(kb),
			UseCases:    len(useCases),
			Anchors:     len(anchors),
		},
	}
	return writeBundle(manifest, project, []*domain.Scratchpad{sp}, items, todos, bugs, kb, useCases, anchors)
}

func (b *Bundler) collectAnchors(
	ctx context.Context,
	items []*domain.ScratchpadItem,
	todos []*domain.TodoItem,
	bugs []*domain.BugItem,
	kb []*domain.KnowledgeEntry,
	useCases []*domain.UseCaseItem,
) ([]*domain.CodeAnchor, error) {
	var out []*domain.CodeAnchor
	add := func(ownerType, ownerID string) error {
		anchors, err := b.Store.ListCodeAnchors(ctx, ownerType, ownerID)
		if err != nil {
			return fmt.Errorf("list anchors %s %s: %w", ownerType, ownerID, err)
		}
		out = append(out, anchors...)
		return nil
	}
	for _, i := range items {
		if err := add("scratchpad_item", i.ID); err != nil {
			return nil, err
		}
	}
	for _, t := range todos {
		if err := add("todo_item", t.ID); err != nil {
			return nil, err
		}
	}
	for _, bg := range bugs {
		if err := add("bug_item", bg.ID); err != nil {
			return nil, err
		}
	}
	for _, k := range kb {
		if err := add("knowledge_entry", k.ID); err != nil {
			return nil, err
		}
	}
	for _, u := range useCases {
		if err := add("use_case_item", u.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (b *Bundler) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// writeBundle is the format-defining function — the layout it produces is
// what import (item 8) is built to consume. Keep the two in lockstep.
func writeBundle(
	manifest Manifest,
	project *domain.Project,
	scratchpads []*domain.Scratchpad,
	items []*domain.ScratchpadItem,
	todos []*domain.TodoItem,
	bugs []*domain.BugItem,
	kb []*domain.KnowledgeEntry,
	useCases []*domain.UseCaseItem,
	anchors []*domain.CodeAnchor,
) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	if err := writeJSON(zw, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := writeJSON(zw, "project.json", project); err != nil {
		return nil, err
	}
	for _, sp := range scratchpads {
		if err := writeJSON(zw, "scratchpads/"+sp.ID+".json", sp); err != nil {
			return nil, err
		}
	}
	for _, item := range items {
		base := "scratchpads/" + item.ScratchpadID + "/items/" + item.ID
		// .md holds the raw content body (human-readable, Obsidian-friendly).
		if err := writeFile(zw, base+".md", []byte(item.Content)); err != nil {
			return nil, err
		}
		// .json holds metadata. Content field is blanked so the body lives
		// in exactly one place; on import we read from .md and ignore the
		// JSON content field.
		sidecar := *item
		sidecar.Content = ""
		if err := writeJSON(zw, base+".json", &sidecar); err != nil {
			return nil, err
		}
	}
	for _, t := range todos {
		if err := writeJSON(zw, "derived/todos/"+t.ID+".json", t); err != nil {
			return nil, err
		}
	}
	for _, bg := range bugs {
		if err := writeJSON(zw, "derived/bugs/"+bg.ID+".json", bg); err != nil {
			return nil, err
		}
	}
	for _, k := range kb {
		if err := writeJSON(zw, "derived/kb/"+k.ID+".json", k); err != nil {
			return nil, err
		}
	}
	for _, u := range useCases {
		if err := writeJSON(zw, "derived/use_cases/"+u.ID+".json", u); err != nil {
			return nil, err
		}
	}
	if anchors == nil {
		anchors = []*domain.CodeAnchor{}
	}
	if err := writeJSON(zw, "code-anchors.json", anchors); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	return writeFile(zw, name, data)
}

func writeFile(zw *zip.Writer, name string, data []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
