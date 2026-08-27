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

package exportbundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// ImportResult summarizes what landed during an import. Returned to the API
// caller so the UI can switch to the new project + show a "imported N
// scratchpads, M items..." confirmation.
type ImportResult struct {
	Project *domain.Project `json:"project"`
	Counts  Counts          `json:"counts"`
}

// ErrSchemaTooNew is returned when a bundle's manifest declares a schema
// version newer than this binary supports. Surface as a 400 — re-exporting
// from an older binary won't help; the user needs to upgrade.
type ErrSchemaTooNew struct {
	Bundle  int
	Current int
}

func (e *ErrSchemaTooNew) Error() string {
	return fmt.Sprintf("bundle schema_version %d is newer than this binary supports (current: %d) — upgrade CodeDistill",
		e.Bundle, e.Current)
}

// Import reads a zip bundle and creates a new project + all child entities
// in fresh-IDs / always-new-project mode. Per the locked design:
//   - Every entity gets a fresh ID via newID(). Cross-references inside the
//     bundle (scratchpad_id on items, source_item_id on derived items,
//     owner_id on anchors, etc.) are remapped to the new IDs.
//   - The new project is named "<original> (imported)".
//   - repo_root is blanked — paths from the source machine likely don't
//     resolve here. User reconfigures after.
//   - manifest.schema_version > SchemaVersion is rejected with ErrSchemaTooNew.
//
// Two-pass design: first pass assigns fresh IDs and builds the remap; second
// pass writes records with remapped references. Avoids needing UPDATE calls
// after CREATE for forward references like ScratchpadItem.DerivedItemID.
func Import(
	ctx context.Context,
	store storage.Storage,
	data []byte,
	newID func() string,
	now time.Time,
) (*ImportResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	files, err := readAllFiles(zr)
	if err != nil {
		return nil, err
	}

	manifestData, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("invalid bundle: missing manifest.json")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.SchemaVersion > SchemaVersion {
		return nil, &ErrSchemaTooNew{Bundle: manifest.SchemaVersion, Current: SchemaVersion}
	}

	projectData, ok := files["project.json"]
	if !ok {
		return nil, fmt.Errorf("invalid bundle: missing project.json")
	}
	var origProject domain.Project
	if err := json.Unmarshal(projectData, &origProject); err != nil {
		return nil, fmt.Errorf("parse project: %w", err)
	}

	scratchpads, items, todos, bugs, kbs, useCases, anchors, err := parseEntities(files)
	if err != nil {
		return nil, err
	}

	// Pass 1: assign fresh IDs to every entity, build the remap.
	idMap := make(map[string]string)
	newProjectID := newID()
	idMap[origProject.ID] = newProjectID
	for _, sp := range scratchpads {
		old := sp.ID
		sp.ID = newID()
		idMap[old] = sp.ID
	}
	for _, item := range items {
		old := item.ID
		item.ID = newID()
		idMap[old] = item.ID
	}
	for _, t := range todos {
		old := t.ID
		t.ID = newID()
		idMap[old] = t.ID
	}
	for _, bg := range bugs {
		old := bg.ID
		bg.ID = newID()
		idMap[old] = bg.ID
	}
	for _, k := range kbs {
		old := k.ID
		k.ID = newID()
		idMap[old] = k.ID
	}
	for _, u := range useCases {
		old := u.ID
		u.ID = newID()
		idMap[old] = u.ID
	}

	// Pass 2: remap references and write. New project gets CreatedAt=now;
	// child entities preserve their original timestamps so item history
	// stays meaningful in the imported copy.
	workspaceID := pickWorkspaceID(origProject.WorkspaceID)
	projectName, err := uniqueImportedProjectName(ctx, store, workspaceID, origProject.Name)
	if err != nil {
		return nil, err
	}
	newProject := &domain.Project{
		ID:          newProjectID,
		WorkspaceID: workspaceID,
		Name:        projectName,
		// RepoRoot intentionally blank — paths from the source machine
		// likely don't resolve here. User reconfigures after.
		CreatedAt: now,
	}
	if err := store.CreateProject(ctx, newProject); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	for _, sp := range scratchpads {
		sp.ProjectID = newProjectID
		if err := store.CreateScratchpad(ctx, sp); err != nil {
			return nil, fmt.Errorf("create scratchpad %s: %w", sp.ID, err)
		}
	}

	for _, item := range items {
		newSpID, ok := idMap[item.ScratchpadID]
		if !ok {
			return nil, fmt.Errorf("item %s references unknown scratchpad %s", item.ID, item.ScratchpadID)
		}
		item.ScratchpadID = newSpID
		// derived_item_id may point to a derived row that's also in this
		// bundle. If it is, remap; if not (orphaned bundle, partial export),
		// blank it so the FK-style constraint isn't violated.
		if item.DerivedItemID != "" {
			if newDerivedID, ok := idMap[item.DerivedItemID]; ok {
				item.DerivedItemID = newDerivedID
			} else {
				item.DerivedItemID = ""
			}
		}
		if err := store.CreateScratchpadItem(ctx, item); err != nil {
			return nil, fmt.Errorf("create item %s: %w", item.ID, err)
		}
	}

	// Sort derived items by CreatedAt before insert. Two reasons:
	//   - parseEntities iterates a map, so the bundle's slices arrive in
	//     non-deterministic order
	//   - For pre-v0.7.x bundles (no `number` field), Number defaults to 0
	//     and CreateXItem auto-assigns the next per-project number; sorting
	//     by CreatedAt makes the resulting sequence reproducible
	// New bundles preserve their Number values verbatim (they survive JSON
	// round-trip) so the imported project's numbering matches the source's.
	sort.SliceStable(todos, func(i, j int) bool { return todos[i].CreatedAt.Before(todos[j].CreatedAt) })
	sort.SliceStable(bugs, func(i, j int) bool { return bugs[i].CreatedAt.Before(bugs[j].CreatedAt) })
	sort.SliceStable(kbs, func(i, j int) bool { return kbs[i].CreatedAt.Before(kbs[j].CreatedAt) })
	sort.SliceStable(useCases, func(i, j int) bool { return useCases[i].CreatedAt.Before(useCases[j].CreatedAt) })

	for _, t := range todos {
		t.ProjectID = newProjectID
		t.SourceItemID = remapOrBlank(idMap, t.SourceItemID)
		if err := store.CreateTodoItem(ctx, t); err != nil {
			return nil, fmt.Errorf("create todo %s: %w", t.ID, err)
		}
	}
	for _, bg := range bugs {
		bg.ProjectID = newProjectID
		bg.SourceItemID = remapOrBlank(idMap, bg.SourceItemID)
		if err := store.CreateBugItem(ctx, bg); err != nil {
			return nil, fmt.Errorf("create bug %s: %w", bg.ID, err)
		}
	}
	for _, k := range kbs {
		k.ProjectID = newProjectID
		k.SourceItemID = remapOrBlank(idMap, k.SourceItemID)
		if err := store.CreateKnowledgeEntry(ctx, k); err != nil {
			return nil, fmt.Errorf("create kb %s: %w", k.ID, err)
		}
	}
	for _, u := range useCases {
		u.ProjectID = newProjectID
		u.SourceItemID = remapOrBlank(idMap, u.SourceItemID)
		if err := store.CreateUseCaseItem(ctx, u); err != nil {
			return nil, fmt.Errorf("create use_case %s: %w", u.ID, err)
		}
	}

	// Anchors — owner_id must remap. If the owner isn't in this bundle, skip
	// the anchor (it points at something that doesn't exist locally).
	for _, a := range anchors {
		newOwnerID, ok := idMap[a.OwnerID]
		if !ok {
			continue
		}
		a.ID = newID()
		a.OwnerID = newOwnerID
		if err := store.CreateCodeAnchor(ctx, a); err != nil {
			return nil, fmt.Errorf("create anchor: %w", err)
		}
	}

	return &ImportResult{
		Project: newProject,
		Counts: Counts{
			Scratchpads: len(scratchpads),
			Items:       len(items),
			Todos:       len(todos),
			Bugs:        len(bugs),
			KB:          len(kbs),
			UseCases:    len(useCases),
			Anchors:     len(anchors),
		},
	}, nil
}

// readAllFiles slurps every entry into memory. Acceptable for the bundle
// sizes we expect (single-digit MB even for power users); if that changes,
// move to a streaming approach with two passes over the zip.
func readAllFiles(zr *zip.Reader) (map[string][]byte, error) {
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
		out[f.Name] = body
	}
	return out, nil
}

func parseEntities(files map[string][]byte) (
	scratchpads []*domain.Scratchpad,
	items []*domain.ScratchpadItem,
	todos []*domain.TodoItem,
	bugs []*domain.BugItem,
	kbs []*domain.KnowledgeEntry,
	useCases []*domain.UseCaseItem,
	anchors []*domain.CodeAnchor,
	err error,
) {
	for name, data := range files {
		switch {
		case name == "manifest.json" || name == "project.json":
			// handled by caller
			continue
		case strings.HasPrefix(name, "scratchpads/") &&
			strings.HasSuffix(name, ".json") &&
			!strings.Contains(name, "/items/"):
			var sp domain.Scratchpad
			if uerr := json.Unmarshal(data, &sp); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			scratchpads = append(scratchpads, &sp)
		case strings.HasPrefix(name, "scratchpads/") &&
			strings.Contains(name, "/items/") &&
			strings.HasSuffix(name, ".json"):
			var item domain.ScratchpadItem
			if uerr := json.Unmarshal(data, &item); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			// Content lives in the .md sibling — populate it.
			mdName := strings.TrimSuffix(name, ".json") + ".md"
			if mdData, ok := files[mdName]; ok {
				item.Content = string(mdData)
			}
			items = append(items, &item)
		case strings.HasPrefix(name, "derived/todos/") && strings.HasSuffix(name, ".json"):
			var t domain.TodoItem
			if uerr := json.Unmarshal(data, &t); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			todos = append(todos, &t)
		case strings.HasPrefix(name, "derived/bugs/") && strings.HasSuffix(name, ".json"):
			var b domain.BugItem
			if uerr := json.Unmarshal(data, &b); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			bugs = append(bugs, &b)
		case strings.HasPrefix(name, "derived/kb/") && strings.HasSuffix(name, ".json"):
			var k domain.KnowledgeEntry
			if uerr := json.Unmarshal(data, &k); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			kbs = append(kbs, &k)
		case strings.HasPrefix(name, "derived/use_cases/") && strings.HasSuffix(name, ".json"):
			var u domain.UseCaseItem
			if uerr := json.Unmarshal(data, &u); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
			useCases = append(useCases, &u)
		case name == "code-anchors.json":
			if uerr := json.Unmarshal(data, &anchors); uerr != nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("parse %s: %w", name, uerr)
			}
		default:
			// Silently ignore unknown entries — forward-compatible. Future
			// versions may add binary/<id>/... or other paths; older binaries
			// just drop them on import. Manifest schema_version check above
			// keeps us from silently losing data when the format diverges.
		}
	}
	return scratchpads, items, todos, bugs, kbs, useCases, anchors, nil
}

func pickWorkspaceID(orig string) string {
	if orig == "" {
		return "local"
	}
	return orig
}

// uniqueImportedProjectName returns "<base> (imported)" when that name is
// free in the target workspace, otherwise "<base> (imported 2)",
// "<base> (imported 3)", … up to a sane cap. Needed because migration
// 0012 enforces UNIQUE(workspace_id, name) on projects: importing the
// same bundle twice (a real flow when moving a project between machines
// and re-importing for a refresh) would otherwise hit the unique
// constraint and surface as a 500.
func uniqueImportedProjectName(ctx context.Context, store storage.Storage, workspaceID, baseName string) (string, error) {
	existing, err := store.ListProjects(ctx)
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	used := make(map[string]bool, len(existing))
	for _, p := range existing {
		if p.WorkspaceID == workspaceID {
			used[p.Name] = true
		}
	}
	candidate := baseName + " (imported)"
	if !used[candidate] {
		return candidate, nil
	}
	// Cap arbitrary but generous; if the user has 1000 imports of the same
	// bundle, the bundle is the wrong tool.
	for i := 2; i < 1000; i++ {
		c := fmt.Sprintf("%s (imported %d)", baseName, i)
		if !used[c] {
			return c, nil
		}
	}
	return "", fmt.Errorf("too many imported copies of %q in workspace %q", baseName, workspaceID)
}

func remapOrBlank(idMap map[string]string, oldID string) string {
	if oldID == "" {
		return ""
	}
	if newID, ok := idMap[oldID]; ok {
		return newID
	}
	// Reference to an entity not in the bundle (e.g. orphaned source_item_id
	// after the user deleted the scratchpad item but kept the derived todo).
	// Blank it so we don't carry a dangling pointer.
	return ""
}
