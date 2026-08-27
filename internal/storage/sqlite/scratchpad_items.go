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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const scratchpadItemColumns = `
	id, scratchpad_id, name, content_type, content,
	classification_state, skipped_reason, classification_override,
	proposed_category, classification_confidence, classification_reasoning,
	proposed_role, proposed_want, proposed_why,
	derived_item_id, hidden, tags,
	grid_col, grid_row, grid_w, grid_h,
	similar_to_id, similarity_score,
	blob_sha, mime_type, file_name, byte_size, width, height,
	og_title, og_description, og_image_sha, og_fetched_at,
	group_id, collapsed,
	created_at, updated_at, archived_at`

// ListScratchpadItemIDsByState returns the IDs of scratchpad items whose
// classification_state is any of `states`, oldest first. The reconciliation
// sweep uses it to find stranded items (unprocessed / failed / orphaned
// processing) and re-enqueue them.
func (s *Store) ListScratchpadItemIDsByState(ctx context.Context, states ...string) ([]string, error) {
	if len(states) == 0 {
		return nil, nil
	}
	ph := make([]string, len(states))
	args := make([]any, len(states))
	for i, st := range states {
		ph[i] = "?"
		args[i] = st
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM scratchpad_items WHERE classification_state IN (`+strings.Join(ph, ", ")+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("list scratchpad items by state: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanScratchpadItem(scan func(...any) error) (*domain.ScratchpadItem, error) {
	item := &domain.ScratchpadItem{}
	var (
		skippedReason, override, proposedCategory, reasoning, derivedItemID, similarTo sql.NullString
		blobSHA, mimeType, fileName                                                    sql.NullString
		byteSize, width, height                                                        sql.NullInt64
		ogTitle, ogDescription, ogImageSHA                                             sql.NullString
		ogFetchedAt                                                                    flexTime
		createdAt, updatedAt, archivedAt                                               flexTime
		groupID                                                                        sql.NullString
		confidence, similarity                                                         sql.NullFloat64
		tagsRaw                                                                        string
	)
	err := scan(
		&item.ID, &item.ScratchpadID, &item.Name, &item.ContentType, &item.Content,
		&item.ClassificationState, &skippedReason, &override,
		&proposedCategory, &confidence, &reasoning,
		&item.ProposedRole, &item.ProposedWant, &item.ProposedWhy,
		&derivedItemID, &item.Hidden, &tagsRaw,
		&item.GridCol, &item.GridRow, &item.GridW, &item.GridH,
		&similarTo, &similarity,
		&blobSHA, &mimeType, &fileName, &byteSize, &width, &height,
		&ogTitle, &ogDescription, &ogImageSHA, &ogFetchedAt,
		&groupID, &item.Collapsed,
		&createdAt, &updatedAt, &archivedAt,
	)
	if err != nil {
		return nil, err
	}
	item.SkippedReason = stringOrEmpty(skippedReason)
	item.ClassificationOverride = stringOrEmpty(override)
	item.ProposedCategory = stringOrEmpty(proposedCategory)
	item.ClassificationReasoning = stringOrEmpty(reasoning)
	item.DerivedItemID = stringOrEmpty(derivedItemID)
	item.ClassificationConfidence = floatOrZero(confidence)
	item.SimilarToID = stringOrEmpty(similarTo)
	item.SimilarityScore = floatOrZero(similarity)
	item.BlobSHA = stringOrEmpty(blobSHA)
	item.MimeType = stringOrEmpty(mimeType)
	item.FileName = stringOrEmpty(fileName)
	item.ByteSize = intOrZero(byteSize)
	item.Width = int(intOrZero(width))
	item.Height = int(intOrZero(height))
	item.OGTitle = stringOrEmpty(ogTitle)
	item.OGDescription = stringOrEmpty(ogDescription)
	item.OGImageSHA = stringOrEmpty(ogImageSHA)
	item.OGFetchedAt = ogFetchedAt.ptr()
	item.CreatedAt = createdAt.t
	item.UpdatedAt = updatedAt.t
	item.ArchivedAt = archivedAt.ptr()
	item.GroupID = stringOrEmpty(groupID)
	item.Tags = unmarshalTags(tagsRaw)
	return item, nil
}

func (s *Store) CreateScratchpadItem(ctx context.Context, i *domain.ScratchpadItem) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO scratchpad_items (`+scratchpadItemColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.ScratchpadID, i.Name, i.ContentType, i.Content,
		i.ClassificationState, nullString(i.SkippedReason), nullString(i.ClassificationOverride),
		nullString(i.ProposedCategory), i.ClassificationConfidence, nullString(i.ClassificationReasoning),
		i.ProposedRole, i.ProposedWant, i.ProposedWhy,
		nullString(i.DerivedItemID), i.Hidden, marshalTags(i.Tags),
		i.GridCol, i.GridRow, i.GridW, i.GridH,
		nullString(i.SimilarToID), nullFloatZero(i.SimilarityScore),
		nullString(i.BlobSHA), nullString(i.MimeType), nullString(i.FileName),
		nullInt64Zero(i.ByteSize), nullIntZero(i.Width), nullIntZero(i.Height),
		nullString(i.OGTitle), nullString(i.OGDescription), nullString(i.OGImageSHA),
		nullTimePtr(i.OGFetchedAt),
		nullString(i.GroupID), i.Collapsed,
		i.CreatedAt, i.UpdatedAt, nullTimePtr(i.ArchivedAt),
	)
	return err
}

// SetScratchpadItemArchived sets or clears an item's archived state. A targeted
// update so editing an item elsewhere never disturbs it.
func (s *Store) SetScratchpadItemArchived(ctx context.Context, id string, at *time.Time) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET archived_at = ? WHERE id = ?`, nullTimePtr(at), id)
	if err != nil {
		return fmt.Errorf("set archived: %w", err)
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("scratchpad item %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) GetScratchpadItem(ctx context.Context, id string) (*domain.ScratchpadItem, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+scratchpadItemColumns+` FROM scratchpad_items WHERE id = ?`, id,
	)
	item, err := scanScratchpadItem(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("scratchpad item %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	// Read-through: a group frame's note lives in group_notes (canvas rework
	// C2), authoritative over the legacy annotations blob until that column
	// drops.
	if item.ContentType == "group" {
		if note, err := s.GetGroupNote(ctx, item.ID); err == nil && note != "" {
			item.Annotations = note
		}
	}
	return item, nil
}

func (s *Store) ListScratchpadItems(ctx context.Context, scratchpadID string) ([]*domain.ScratchpadItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+scratchpadItemColumns+`
		 FROM scratchpad_items WHERE scratchpad_id = ? ORDER BY created_at`, scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.ScratchpadItem
	hasGroup := false
	for rows.Next() {
		item, err := scanScratchpadItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		hasGroup = hasGroup || item.ContentType == "group"
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Read-through: group notes come from group_notes (canvas rework C2),
	// authoritative over the legacy annotations blob until that column drops.
	if hasGroup {
		notes, err := s.GroupNotesByScratchpad(ctx, scratchpadID)
		if err != nil {
			return nil, err
		}
		for _, item := range out {
			if item.ContentType != "group" {
				continue
			}
			if note, ok := notes[item.ID]; ok && note != "" {
				item.Annotations = note
			}
		}
	}
	return out, nil
}

// MoveScratchpadItem changes an item's owning scratchpad. Separate from
// UpdateScratchpadItem so the move intent is explicit at the call site —
// item metadata (classification, content, anchors) is untouched. Caller
// should refetch grid layout for the destination since the new pad's
// next-row computation uses MAX(grid_row + grid_h) which the moved
// item now influences.
func (s *Store) MoveScratchpadItem(ctx context.Context, itemID, dstScratchpadID string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET scratchpad_id = ?, updated_at = ? WHERE id = ?`,
		dstScratchpadID, time.Now().UTC(), itemID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", itemID, storage.ErrNotFound)
	}
	return nil
}

// derivedTableForCategory maps an effective category to its work-item
// table, or "" for categories with no derived row (e.g. skip).
func derivedTableForCategory(category string) string {
	switch category {
	case "todo":
		return "todo_items"
	case "bug":
		return "bug_items"
	case "kb":
		return "knowledge_entries"
	case "use_case":
		return "use_case_items"
	}
	return ""
}

// MoveItemToProject atomically moves a scratchpad item to dstScratchpadID
// AND re-homes its derived work item to dstProjectID, in a single
// transaction. The derived row gets a FRESH per-project number (next in
// the destination) because all four derived tables enforce
// UNIQUE(project_id, number) — carrying the old number would collide with
// an existing item in the destination. Either both writes land or neither
// does, so a cross-project move can never leave a split-brain (item in the
// new project, derived row in the old).
//
// derivedID/category may be empty (an unclassified item) — then only the
// item moves.
func (s *Store) MoveItemToProject(ctx context.Context, itemID, dstScratchpadID, category, derivedID, dstProjectID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE scratchpad_items SET scratchpad_id = ?, updated_at = ? WHERE id = ?`,
		dstScratchpadID, time.Now().UTC(), itemID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", itemID, storage.ErrNotFound)
	}

	if table := derivedTableForCategory(category); table != "" && derivedID != "" {
		// table is from a closed switch, never user input — safe to
		// interpolate. The subquery computes the next number over the
		// destination project's existing rows (this row is still in the
		// source project at this point, so it isn't double-counted).
		if _, err := tx.ExecContext(ctx,
			"UPDATE "+table+" SET project_id = ?, "+
				"number = (SELECT COALESCE(MAX(number)+1, 1) FROM "+table+" WHERE project_id = ?) "+
				"WHERE id = ?",
			dstProjectID, dstProjectID, derivedID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateScratchpadItem(ctx context.Context, i *domain.ScratchpadItem) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET
			name = ?, content_type = ?, content = ?,
			classification_state = ?, skipped_reason = ?, classification_override = ?,
			proposed_category = ?, classification_confidence = ?, classification_reasoning = ?,
			proposed_role = ?, proposed_want = ?, proposed_why = ?,
			derived_item_id = ?, hidden = ?, tags = ?,
			grid_col = ?, grid_row = ?, grid_w = ?, grid_h = ?,
			blob_sha = ?, mime_type = ?, file_name = ?,
			byte_size = ?, width = ?, height = ?,
			og_title = ?, og_description = ?, og_image_sha = ?, og_fetched_at = ?,
			group_id = ?, collapsed = ?,
			updated_at = ?
		 WHERE id = ?`,
		i.Name, i.ContentType, i.Content,
		i.ClassificationState, nullString(i.SkippedReason), nullString(i.ClassificationOverride),
		nullString(i.ProposedCategory), i.ClassificationConfidence, nullString(i.ClassificationReasoning),
		i.ProposedRole, i.ProposedWant, i.ProposedWhy,
		nullString(i.DerivedItemID), i.Hidden, marshalTags(i.Tags),
		i.GridCol, i.GridRow, i.GridW, i.GridH,
		nullString(i.BlobSHA), nullString(i.MimeType), nullString(i.FileName),
		nullInt64Zero(i.ByteSize), nullIntZero(i.Width), nullIntZero(i.Height),
		nullString(i.OGTitle), nullString(i.OGDescription), nullString(i.OGImageSHA),
		nullTimePtr(i.OGFetchedAt),
		nullString(i.GroupID), i.Collapsed,
		i.UpdatedAt,
		i.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", i.ID, storage.ErrNotFound)
	}
	return nil
}

// UpdateClassification writes ONLY the classification columns — see the
// interface doc. Field-scoped so a stale classifier copy can't clobber og_*
// (or content/grid/blobs) that another worker set concurrently.
func (s *Store) UpdateClassification(ctx context.Context, i *domain.ScratchpadItem) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET
			classification_state = ?, skipped_reason = ?, classification_override = ?,
			proposed_category = ?, classification_confidence = ?, classification_reasoning = ?,
			proposed_role = ?, proposed_want = ?, proposed_why = ?,
			derived_item_id = ?, updated_at = ?
		 WHERE id = ?`,
		i.ClassificationState, nullString(i.SkippedReason), nullString(i.ClassificationOverride),
		nullString(i.ProposedCategory), i.ClassificationConfidence, nullString(i.ClassificationReasoning),
		i.ProposedRole, i.ProposedWant, i.ProposedWhy,
		nullString(i.DerivedItemID), i.UpdatedAt,
		i.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", i.ID, storage.ErrNotFound)
	}
	return nil
}

// UpdateOGMetadata writes ONLY the OpenGraph columns — see the interface doc.
// Disjoint from UpdateClassification's column set, so the concurrent OG worker
// and classifier never clobber each other (only updated_at is shared, benign).
func (s *Store) UpdateOGMetadata(ctx context.Context, i *domain.ScratchpadItem) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET
			og_title = ?, og_description = ?, og_image_sha = ?, og_fetched_at = ?,
			updated_at = ?
		 WHERE id = ?`,
		nullString(i.OGTitle), nullString(i.OGDescription), nullString(i.OGImageSHA),
		nullTimePtr(i.OGFetchedAt),
		i.UpdatedAt,
		i.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", i.ID, storage.ErrNotFound)
	}
	return nil
}

// RestackScratchpadItems rewrites grid_col + grid_row (and optionally
// grid_w when GridW > 0) for each entry in positions, in a single
// transaction. grid_h / hidden / content are never touched. Two
// prepared statements — one with grid_w, one without — so the common
// "preserve widths" path stays a 3-column update and only the
// auto-resize path pays for the 4-column variant.
func (s *Store) RestackScratchpadItems(ctx context.Context, positions []storage.ItemGridPosition) error {
	if len(positions) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// One statement covers all variants: width/height rewrite only when
	// the position carries a non-zero value (fit / column modes), else
	// the existing dimension is kept.
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE scratchpad_items
		    SET grid_col = ?, grid_row = ?,
		        grid_w = CASE WHEN ? > 0 THEN ? ELSE grid_w END,
		        grid_h = CASE WHEN ? > 0 THEN ? ELSE grid_h END,
		        updated_at = ?
		  WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC()
	for _, p := range positions {
		if _, err := stmt.ExecContext(ctx,
			p.GridCol, p.GridRow, p.GridW, p.GridW, p.GridH, p.GridH, now, p.ID); err != nil {
			return fmt.Errorf("restack item %s: %w", p.ID, err)
		}
	}
	return tx.Commit()
}

// compositeBlobRefRE matches a SHA-256 inside the standard
// /api/v1/blobs/<sha> URL pattern used by inline image refs in
// composite-doc content. Anchored on the path prefix to avoid
// matching arbitrary 64-hex strings.
var compositeBlobRefRE = regexp.MustCompile(`/api/v1/blobs/([0-9a-f]{64})`)

// ListCompositeImageShas walks composite items and regex-extracts
// every inline blob reference. Distinct + unordered. Empty slice
// when there are no composite items or no inline refs.
//
// Backed by the idx_items_composite partial index (migration 0026)
// so this isn't a full table scan even on large pads. The regex
// pass itself is fast: one allocation per match, content sizes are
// bounded by reasonable doc length.
func (s *Store) ListCompositeImageShas(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT content FROM scratchpad_items WHERE content_type = 'composite'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		for _, m := range compositeBlobRefRE.FindAllStringSubmatch(content, -1) {
			if len(m) >= 2 {
				seen[m[1]] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for sha := range seen {
		out = append(out, sha)
	}
	return out, nil
}

// ListLiveBlobShas returns every distinct blob SHA currently
// referenced by any scratchpad_item. Two columns participate:
//
//   - blob_sha       — the primary blob of an image/file item (mig 0022)
//   - og_image_sha   — the OpenGraph thumbnail of a link item (mig 0024)
//
// Both are SHAs into the same BlobStore, so the GC sweeper's
// live-set query must UNION them. Backed by partial indexes
// idx_items_blob_sha and idx_items_og_image_sha. Empty slice when
// no items reference blobs.
func (s *Store) ListLiveBlobShas(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT blob_sha FROM scratchpad_items WHERE blob_sha IS NOT NULL
		 UNION
		 SELECT og_image_sha FROM scratchpad_items WHERE og_image_sha IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

// NextAvailableGridRow returns max(grid_row + grid_h) for visible items in the
// scratchpad — i.e., the first empty row where a new item can safely stack.
// Hidden AND archived items are excluded so the canvas compacts when items
// leave it — an archived item's stale coordinates must not push new captures
// down (they'd stack below invisible ghosts).
func (s *Store) NextAvailableGridRow(ctx context.Context, scratchpadID string) (int, error) {
	var row sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT MAX(grid_row + grid_h) FROM scratchpad_items
		 WHERE scratchpad_id = ? AND hidden = `+s.DB.Dialect().BoolFalse()+` AND archived_at IS NULL`,
		scratchpadID,
	).Scan(&row)
	if err != nil {
		return 0, err
	}
	if !row.Valid {
		return 0, nil
	}
	return int(row.Int64), nil
}

// fetchSourceNames returns a map[scratchpad_item_id] => name for the
// given set of ids. Used by ListTodoItems / ListBugItems /
// ListKnowledgeEntries / ListUseCaseItems (and their byScratchpad
// variants) to enrich derived items with their originating scratchpad
// item's user-set name. Empty result for empty input.
func (s *Store) fetchSourceNames(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name FROM scratchpad_items WHERE id IN (`+placeholders+`)`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if name != "" {
			out[id] = name
		}
	}
	return out, rows.Err()
}

// collectSourceIDs is a generic helper for derived list methods: take a
// closure that yields the source_item_id for each item, return the
// unique non-empty set as a slice ready for fetchSourceNames.
func collectSourceIDs[T any](items []T, get func(T) string) []string {
	seen := map[string]struct{}{}
	for _, it := range items {
		s := get(it)
		if s == "" {
			continue
		}
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func (s *Store) DeleteScratchpadItem(ctx context.Context, id string) error {
	// One transaction so a failure can't strip a live item's anchors/note and
	// leave the row behind (audit M24). The item DELETE also clears any child
	// group_id via the ON DELETE SET NULL FK (migration 0027); group_notes has
	// no FK, so its orphan is removed here explicitly.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete scratchpad item: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM code_anchors WHERE owner_type = 'scratchpad_item' AND owner_id = ?`, id); err != nil {
		return fmt.Errorf("delete item anchors: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_notes WHERE group_id = ?`, id); err != nil {
		return fmt.Errorf("delete group note: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM scratchpad_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete scratchpad item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", id, storage.ErrNotFound)
	}
	return tx.Commit()
}
