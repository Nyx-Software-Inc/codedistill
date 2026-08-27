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

// Package agent implements the background classification pipeline.
//
// Phase 1 policy (locked by Phase 0 spike results):
//   - Every agent-classified item lands in pending-review (Inbox). No
//     confidence-based auto-commit; the model's self-reported confidence
//     is ~0.9 on almost everything and is unreliable. Revisit in Phase 2
//     with logprobs or multi-sample voting (Req 4.24).
//   - Classification_Override short-circuits the model: "skip" marks the
//     item skipped; "todo"/"bug"/"kb" auto-commits the derived item.
//   - Classification_Mode "off" on the owning Scratchpad suppresses processing
//     unless overridden at the item level.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/embed"
	"codedistill/internal/events"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// ProjectFileTreeFunc returns the project's repo file list. Used by the
// auto-anchor extension so the classifier prompt can include candidate
// paths. Production wires this through internal/git; tests can leave it
// nil (auto-anchor degrades silently to no suggestions).
type ProjectFileTreeFunc func(ctx context.Context, projectID string) ([]string, error)

// Embedder produces a vector representation of input text. Production
// wires the Ollama nomic-embed-text client; tests can leave it nil
// (embedding degrades silently to no-op). Per-call failure is logged
// and treated as non-fatal — classification still completes.
type Embedder interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

type Agent struct {
	store      storage.Storage
	classifier Classifier
	criteria   CriteriaDrafter // optional; nil = no AI criteria drafting
	embedder   Embedder        // optional; nil = no embeddings written
	newID      func() string
	now        func() time.Time
	log        *slog.Logger
	fileTree   ProjectFileTreeFunc // optional; nil = no auto-anchor
	// grouper is the paid duplicate-intelligence impl (internal/dedup).
	// Optional; nil = no dedup banner / no auto-grouping (free build,
	// or commercial build without the Dedup feature). See dedup_iface.go.
	grouper DuplicateGrouper
	// bus is optional. When wired, the agent fans out change events
	// after every successful Process so SSE subscribers refetch
	// without waiting for the fallback poll. nil-safe (events.Bus
	// has a nil-receiver Publish).
	bus *events.Bus

	// exportNotify, when wired, pushes a freshly classifier-derived item to any
	// configured MCP-export destination — items the agent creates would
	// otherwise bypass the API's notifyExport and silently never sync. Optional;
	// nil = no export side channel (free build, or no destination configured).
	exportNotify ExportNotifyFunc

	jobs chan string
	wg   sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once

	// Reconciliation sweep (opt-in via WithReconcile). Re-enqueues stranded
	// items — unprocessed / failed / orphaned processing — on startup and every
	// reconcileInterval, so a lost enqueue or a transient model outage never
	// leaves a capture stuck forever. Disabled (interval 0) by default so tests
	// keep the pure event-driven behavior.
	reconcileInterval time.Duration
	reachable         func(ctx context.Context) bool // optional gate; nil = assume up
	inflight          sync.Map                       // itemID -> struct{}: queued/in-flight, for dedup
	attemptsMu        sync.Mutex
	attempts          map[string]int // failed-retry counts (in-memory; reset on manual reprocess)
	quit              chan struct{}  // closed on Stop to halt the sweeper before jobs closes
	sweepWg           sync.WaitGroup
}

type Option func(*Agent)

func WithLogger(l *slog.Logger) Option    { return func(a *Agent) { a.log = l } }
func WithIDGen(f func() string) Option    { return func(a *Agent) { a.newID = f } }

// WithCriteriaDrafter wires the AI acceptance-criteria drafter (glass-box
// Phase 2). When set, classifying a drop into a use_case/bug also drafts
// proposed criteria; it also backs the on-demand "Draft with AI" action.
func WithCriteriaDrafter(d CriteriaDrafter) Option { return func(a *Agent) { a.criteria = d } }
func WithClock(f func() time.Time) Option { return func(a *Agent) { a.now = f } }
func WithQueueSize(n int) Option          { return func(a *Agent) { a.jobs = make(chan string, n) } }
func WithProjectFileTree(f ProjectFileTreeFunc) Option {
	return func(a *Agent) { a.fileTree = f }
}
func WithEmbedder(e Embedder) Option {
	return func(a *Agent) { a.embedder = e }
}
func WithEventBus(b *events.Bus) Option {
	return func(a *Agent) { a.bus = b }
}
func WithDuplicateGrouper(g DuplicateGrouper) Option {
	return func(a *Agent) { a.grouper = g }
}

// WithReconcile enables the reconciliation sweep: on startup and every
// `interval`, stranded scratchpad items (unprocessed / failed / orphaned
// processing) are re-enqueued so nothing stays stuck. `reachable` (optional)
// gates the sweep so it does no work while the model server is down; nil =
// always attempt.
func WithReconcile(interval time.Duration, reachable func(ctx context.Context) bool) Option {
	return func(a *Agent) {
		a.reconcileInterval = interval
		a.reachable = reachable
	}
}

// ExportNotifyFunc mirrors the API server's notifyExport: push an item change
// to any configured MCP-export destination. op is "create"/"update"/etc.
type ExportNotifyFunc func(ctx context.Context, ownerType, ownerID, op string, payload any)

// WithExportNotify wires the MCP-export side channel so classifier-derived
// items sync to a configured destination just like API-created ones do.
func WithExportNotify(fn ExportNotifyFunc) Option {
	return func(a *Agent) { a.exportNotify = fn }
}

// Grouper exposes the injected duplicate-intelligence impl (or nil) so
// the API server can drive the on-demand scan without a second
// injection point. nil in the free build / unlicensed commercial build.
func (a *Agent) Grouper() DuplicateGrouper { return a.grouper }

// New constructs an Agent. Start it before enqueueing work.
func New(store storage.Storage, c Classifier, opts ...Option) *Agent {
	a := &Agent{
		store:      store,
		classifier: c,
		newID:      id.New,
		now:        time.Now,
		log:        slog.Default(),
		jobs:       make(chan string, 64),
		attempts:   map[string]int{},
		quit:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Start launches the worker goroutine (and, when WithReconcile is set, the
// reconciliation sweeper). Idempotent.
func (a *Agent) Start(ctx context.Context) {
	a.startOnce.Do(func() {
		a.wg.Add(1)
		go a.run(ctx)
		if a.reconcileInterval > 0 {
			a.sweepWg.Add(1)
			go a.runSweeper(ctx)
		}
	})
}

// Enqueue adds a Scratchpad_Item ID to the classification queue. Non-blocking
// and shutdown-safe (audit H10): a blocking send could wedge a capture handler
// forever when the classifier is backed up, and panic on send-to-closed if it
// raced Stop. Either way the item is persisted as "unprocessed", so the
// reconcile sweep reclaims it — nothing is lost.
func (a *Agent) Enqueue(itemID string) {
	a.inflight.Store(itemID, struct{}{})
	// Recover the send-on-closed panic if this raced Stop's close(jobs).
	defer func() {
		if recover() != nil {
			a.inflight.Delete(itemID)
		}
	}()
	select {
	case a.jobs <- itemID:
	default:
		// Queue full: don't block the capture path — the sweep (which dedups
		// on inflight) will pick the persisted item up, so clear inflight.
		a.inflight.Delete(itemID)
	}
}

// Stop halts the sweeper (so it can't enqueue after the queue closes), then
// closes the queue and waits for the worker to drain and exit. After Stop,
// Enqueue must not be called.
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		close(a.quit)    // tell the sweeper to stop enqueueing
		a.sweepWg.Wait() // ensure it has fully exited before we close jobs
		close(a.jobs)
	})
	a.wg.Wait()
}

func (a *Agent) run(ctx context.Context) {
	defer a.wg.Done()
	for itemID := range a.jobs {
		if ctx.Err() != nil {
			return
		}
		err := a.Process(ctx, itemID)
		a.inflight.Delete(itemID) // no longer queued/in-flight (success or fail)
		if err != nil {
			a.log.Error("agent process failed", "item_id", itemID, "err", err)
			continue
		}
		// Successful classification touches the source item and may
		// have created a derived todo/bug/kb/use_case. Fan out coarse
		// events so SSE subscribers refetch instead of waiting for
		// the fallback poll.
		a.bus.Publish(events.ItemsChanged)
		a.bus.Publish(events.TodosChanged)
		a.bus.Publish(events.BugsChanged)
		a.bus.Publish(events.KBChanged)
		a.bus.Publish(events.UseCasesChanged)
		a.bus.Publish(events.InboxChanged)
	}
}

// Process handles a single Scratchpad_Item through the classification
// pipeline. Exposed so callers can drive it synchronously in tests or CLI.
func (a *Agent) Process(ctx context.Context, itemID string) error {
	item, err := a.store.GetScratchpadItem(ctx, itemID)
	if err != nil {
		return fmt.Errorf("load item: %w", err)
	}
	sp, err := a.store.GetScratchpad(ctx, item.ScratchpadID)
	if err != nil {
		return fmt.Errorf("load scratchpad: %w", err)
	}

	// Classification_Override beats Classification_Mode (including off).
	override := strings.ToLower(strings.TrimSpace(item.ClassificationOverride))
	switch override {
	case "skip":
		return a.markSkipped(ctx, item, "override_skip")
	case "todo", "bug", "kb", "use_case":
		// User-forced category: skip the LLM. Role/Want/Why on the derived
		// use_case stay empty since we never extracted them — the user can
		// fill them in by hand if they care, or re-classify to trigger the
		// agent path that does the extraction.
		return a.commitClassified(ctx, item, sp.ProjectID,
			Classification{
				Category:  strings.ToUpper(override),
				Reasoning: "user-forced via Classification_Override",
			},
			1.0,
		)
	}

	// No override: respect Classification_Mode.
	if sp.ClassificationMode == "off" {
		return nil
	}

	item.ClassificationState = "processing"
	item.UpdatedAt = a.now()
	if err := a.store.UpdateClassification(ctx, item); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	// Embed the source content BEFORE classify so the vector can drive
	// auto-anchor's chunk-search pre-filter (4c) and dedup-at-classify
	// (stage 3) without re-embedding. Failure is non-fatal — we just
	// classify with the alphabetical-truncation fallback.
	var sourceVec []float32
	if a.embedder != nil && strings.TrimSpace(item.Content) != "" {
		vec, err := a.embedder.Embed(ctx, embed.NormalizeItemText(item.Content))
		if err != nil {
			a.log.Warn("embed failed",
				"table", "scratchpad_items", "id", item.ID, "err", err)
		} else if len(vec) > 0 {
			sourceVec = vec
		}
	}

	// Auto-anchor: pick file paths to feed the classifier. With embedding
	// + indexed chunks, this is a curated top-N from semantic search.
	// Without, falls back to the alphabetical-truncation file tree.
	in := ClassifyInput{
		Content:   item.Content,
		FileTree:  a.deriveAutoAnchorPaths(ctx, sp.ProjectID, sourceVec),
		ProjectID: sp.ProjectID,
	}

	result, err := a.classifier.Classify(ctx, in)
	if err != nil {
		item.ClassificationState = "failed"
		item.UpdatedAt = a.now()
		if uErr := a.store.UpdateClassification(ctx, item); uErr != nil {
			a.log.Error("failed to record failed state", "err", uErr)
		}
		return fmt.Errorf("classify: %w", err)
	}

	// Auto-anchor enforcement (defense in depth — every classifier impl
	// gets the same guarantees regardless of what it returned):
	//   - KB never carries anchors (non-code category)
	//   - Paths are intersected with the file tree we passed in (drops
	//     hallucinations and the no-tree case)
	//   - At most maxSuggestedFiles per item (defensive trim)
	// Persists as agent-suggested anchors on the source scratchpad_item
	// BEFORE moving to pending-review; CopyCodeAnchors will propagate
	// them to the derived item on Accept. Per-anchor failure is non-fatal.
	if strings.ToUpper(result.Category) != "KB" {
		validPaths := filterToTree(result.SuggestedFiles, in.FileTree)
		if len(validPaths) > 0 {
			now := a.now()
			for _, p := range validPaths {
				anchor := &domain.CodeAnchor{
					ID:         a.newID(),
					OwnerType:  "scratchpad_item",
					OwnerID:    item.ID,
					Kind:       "file",
					Path:       p,
					Provenance: "agent-suggested",
					CreatedAt:  now,
					UpdatedAt:  now,
				}
				if err := a.store.CreateCodeAnchor(ctx, anchor); err != nil {
					a.log.Warn("auto-anchor: create anchor failed",
						"item_id", item.ID, "path", p, "err", err)
				}
			}
		}
	}

	// Phase 1: always route to Inbox. No auto-commit on confidence.
	// Role/Want/Why are stashed as proposed_* shadow fields so the Inbox
	// Accept path can populate the derived use_case_item without re-running
	// the LLM. They survive Reject/Reclassify too — harmless when the user
	// reclassifies to a non-use-case category since they're use-case-only
	// fields on the derived side.
	item.ClassificationState = "pending-review"
	item.ProposedCategory = strings.ToLower(result.Category)
	item.ClassificationReasoning = result.Reasoning
	item.ClassificationConfidence = 0 // unreliable; deferred to Phase 2 per Req 4.24
	item.ProposedRole = result.Role
	item.ProposedWant = result.Want
	item.ProposedWhy = result.Why
	item.UpdatedAt = a.now()
	if err := a.store.UpdateClassification(ctx, item); err != nil {
		return err
	}
	// Persist the source-content embedding (computed before classify so
	// auto-anchor's chunk-search pre-filter could use it). Then run the
	// dedup-at-classify pass with the same vector — but only when the
	// paid duplicate-intelligence grouper is wired (nil in the free
	// build). UpdateScratchpadItem above bumped updated_at; the
	// embedding write here advances embedded_at after it, preserving the
	// embedded_at >= updated_at invariant the backfill predicate uses.
	if sourceVec != nil {
		blob := embed.EncodeFloat32(sourceVec)
		if err := a.store.UpdateEmbedding(
			ctx, storage.TableScratchpadItems, item.ID, blob, a.now(),
		); err != nil {
			a.log.Warn("update embedding failed",
				"table", "scratchpad_items", "id", item.ID, "err", err)
		}
		if a.grouper != nil {
			a.grouper.OnClassified(ctx, sp.ProjectID, item.ID, sourceVec)
		}
	}
	return nil
}

// AcceptPending moves a pending-review item to classified by creating the
// derived item. If overrideCategory is non-empty it replaces the agent's
// proposed category (Inbox Reclassify); otherwise the proposed category is used.
func (a *Agent) AcceptPending(ctx context.Context, itemID, overrideCategory string) error {
	item, err := a.store.GetScratchpadItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.ClassificationState != "pending-review" {
		return fmt.Errorf("item %s not pending-review (state=%q)", itemID, item.ClassificationState)
	}
	sp, err := a.store.GetScratchpad(ctx, item.ScratchpadID)
	if err != nil {
		return err
	}
	category := strings.ToUpper(overrideCategory)
	if category == "" {
		category = strings.ToUpper(item.ProposedCategory)
	}
	// If the user reclassifies, record that as a Classification_Override
	// to match the unified-implementation rule (Req 21 criterion 8).
	if overrideCategory != "" {
		item.ClassificationOverride = strings.ToLower(category)
	}
	// Carry the agent-extracted role/want/why through to derive. They're only
	// meaningful when the destination is USE_CASE; commitClassified ignores
	// them for other categories.
	result := Classification{
		Category:  category,
		Reasoning: item.ClassificationReasoning,
		Role:      item.ProposedRole,
		Want:      item.ProposedWant,
		Why:       item.ProposedWhy,
	}
	return a.commitClassified(ctx, item, sp.ProjectID, result, item.ClassificationConfidence)
}

// RejectPending transitions a pending-review item to skipped
// (skipped_reason = user_rejected_from_inbox).
func (a *Agent) RejectPending(ctx context.Context, itemID string) error {
	item, err := a.store.GetScratchpadItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.ClassificationState != "pending-review" {
		return fmt.Errorf("item %s not pending-review (state=%q)", itemID, item.ClassificationState)
	}
	return a.markSkipped(ctx, item, "user_rejected_from_inbox")
}

// Auto-anchor pre-filter knobs. With chunks indexed, we hand the
// classifier the file paths from the top-N chunk-search hits instead of
// the alphabetical first-500 truncation — far better signal-to-noise on
// large repos, no scaling cliff.
const (
	autoAnchorChunkLimit    = 50   // top chunks to consider before path-dedup
	autoAnchorPathLimit     = 20   // unique file paths sent to the classifier
	autoAnchorChunkMinScore = 0.30 // floor — same as project-wide search
)

// deriveAutoAnchorPaths returns the file paths for the classifier prompt.
// Strategy:
//  1. With an embedding + indexed code chunks → top hits' unique file
//     paths, capped at autoAnchorPathLimit. Tight, semantically relevant.
//  2. Otherwise → the project's full file tree (capped alphabetically
//     inside the classifier prompt). Covers no-embedder, embed-failure,
//     and the "haven't run index-code on this project yet" case.
//
// Returns nil when neither path yields anything (no auto-anchor for
// this classification — KB-style behavior).
func (a *Agent) deriveAutoAnchorPaths(
	ctx context.Context, projectID string, vec []float32,
) []string {
	if len(vec) > 0 {
		hits, err := a.store.SearchCodeChunks(
			ctx, projectID, vec, autoAnchorChunkMinScore, autoAnchorChunkLimit,
		)
		if err != nil {
			a.log.Warn("auto-anchor: chunk search failed",
				"project_id", projectID, "err", err)
		} else if len(hits) > 0 {
			paths := make([]string, 0, autoAnchorPathLimit)
			seen := map[string]struct{}{}
			for _, h := range hits {
				if _, dup := seen[h.FilePath]; dup {
					continue
				}
				seen[h.FilePath] = struct{}{}
				paths = append(paths, h.FilePath)
				if len(paths) >= autoAnchorPathLimit {
					break
				}
			}
			return paths
		}
	}
	if a.fileTree != nil {
		paths, err := a.fileTree(ctx, projectID)
		if err != nil {
			a.log.Warn("auto-anchor: file tree fetch failed",
				"project_id", projectID, "err", err)
			return nil
		}
		return paths
	}
	return nil
}

// derivedTable maps a classification category to the table its derived
// item lives in, for embedding lookups. Empty return for unknown values
// (defensive — should only be the four canonical categories).
func derivedTable(category string) storage.EmbeddableTable {
	switch strings.ToUpper(category) {
	case "TODO":
		return storage.TableTodoItems
	case "BUG":
		return storage.TableBugItems
	case "KB":
		return storage.TableKnowledgeEntries
	case "USE_CASE":
		return storage.TableUseCaseItems
	}
	return ""
}

// embedAndStore is the agent's single embedding call site. Failures are
// logged and swallowed — embedding is a best-effort enrichment, not a
// gate on classification. Empty/whitespace text is skipped to avoid
// persisting meaningless vectors.
//
// Returns the embedding vector when one was successfully written, or
// nil. Caller can use the vector for follow-on work (dedup-at-classify)
// without re-embedding.
func (a *Agent) embedAndStore(
	ctx context.Context, table storage.EmbeddableTable, id, text string,
) []float32 {
	if a.embedder == nil {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	vec, err := a.embedder.Embed(ctx, embed.NormalizeItemText(text))
	if err != nil {
		a.log.Warn("embed failed", "table", string(table), "id", id, "err", err)
		return nil
	}
	if len(vec) == 0 {
		return nil
	}
	blob := embed.EncodeFloat32(vec)
	if err := a.store.UpdateEmbedding(ctx, table, id, blob, a.now()); err != nil {
		a.log.Warn("update embedding failed", "table", string(table), "id", id, "err", err)
		return nil
	}
	return vec
}

// deleteDerived removes a superseded derived work item. category is the
// PRIOR proposed_category (todo|bug|kb|use_case); unknown/empty is a
// no-op. Failures are logged, not returned.
func (a *Agent) deleteDerived(ctx context.Context, category, id string) {
	var err error
	switch category {
	case "todo":
		err = a.store.DeleteTodoItem(ctx, id)
	case "bug":
		err = a.store.DeleteBugItem(ctx, id)
	case "kb":
		err = a.store.DeleteKnowledgeEntry(ctx, id)
	case "use_case":
		err = a.store.DeleteUseCaseItem(ctx, id)
	default:
		return
	}
	if err != nil {
		a.log.Warn("reclassify: delete prior derived item failed",
			"category", category, "id", id, "err", err)
	}
}

// carryOverLog re-homes a superseded derived item's activity log onto the fresh
// one (slice 4) and, when the type actually changed, records a "reclassified"
// entry on the new item's timeline. Best-effort — history carry-over must never
// fail the (successful) re-classification.
func (a *Agent) carryOverLog(ctx context.Context, oldCat, oldID, newCat, newID string) {
	// derivedOwnerType expects the agent's UPPERCASE category; the categories we
	// receive are lowercased (ProposedCategory), so normalize before mapping.
	oldOwner := derivedOwnerType(strings.ToUpper(oldCat))
	newOwner := derivedOwnerType(strings.ToUpper(newCat))
	if oldOwner == "" || newOwner == "" || oldID == "" || newID == "" {
		return
	}
	if err := a.store.ReparentItemEvents(ctx, oldOwner, oldID, newOwner, newID); err != nil {
		a.log.Warn("reclassify: carry over log failed", "old", oldID, "new", newID, "err", err)
	}
	if !strings.EqualFold(oldCat, newCat) {
		ev := &domain.ItemEvent{
			ID: a.newID(), OwnerType: newOwner, OwnerID: newID,
			Kind: "reclassified", Summary: "Reclassified from " + oldCat + " to " + newCat,
			Source: "agent", CreatedAt: a.now(),
		}
		if err := a.store.RecordItemEvent(ctx, ev); err != nil {
			a.log.Warn("reclassify: record event failed", "id", newID, "err", err)
		}
	}
}

// notifyDerivedExport pushes a classifier-created item (TODO/BUG/KB/USE_CASE)
// to any configured MCP-export destination. Best-effort + nil-safe: export is a
// side channel that must never fail (or block) the classification.
func (a *Agent) notifyDerivedExport(ctx context.Context, category, derivedID string) {
	if a.exportNotify == nil || derivedID == "" {
		return
	}
	ownerType := derivedOwnerType(category)
	var payload any
	switch ownerType {
	case "todo_item":
		if it, err := a.store.GetTodoItem(ctx, derivedID); err == nil {
			payload = it
		}
	case "bug_item":
		if it, err := a.store.GetBugItem(ctx, derivedID); err == nil {
			payload = it
		}
	case "knowledge_entry":
		if it, err := a.store.GetKnowledgeEntry(ctx, derivedID); err == nil {
			payload = it
		}
	case "use_case_item":
		if it, err := a.store.GetUseCaseItem(ctx, derivedID); err == nil {
			payload = it
		}
	default:
		return
	}
	if payload == nil {
		a.log.Warn("export: fetch derived item failed", "owner_type", ownerType, "id", derivedID)
		return
	}
	a.exportNotify(ctx, ownerType, derivedID, "create", payload)
}

func (a *Agent) markSkipped(ctx context.Context, item *domain.ScratchpadItem, reason string) error {
	item.ClassificationState = "skipped"
	item.SkippedReason = reason
	item.UpdatedAt = a.now()
	return a.store.UpdateClassification(ctx, item)
}

func (a *Agent) commitClassified(ctx context.Context, item *domain.ScratchpadItem, projectID string, result Classification, confidence float64) error {
	// Capture the item's PRIOR derivation before we overwrite it. A
	// re-classification (or any re-derive) mints a fresh derived row; the
	// old one would otherwise linger with its source_item_id still set,
	// double-counting in the header and leaving a stale work item
	// (CodeDestill_imports bug #1).
	priorDerivedID := item.DerivedItemID
	priorCategory := item.ProposedCategory

	derivedID, err := deriveItem(ctx, a.store, item, projectID, result, a.newID, a.now())
	if err != nil {
		return fmt.Errorf("derive: %w", err)
	}
	item.ClassificationState = "classified"
	item.ProposedCategory = strings.ToLower(result.Category)
	item.ClassificationReasoning = result.Reasoning
	item.ClassificationConfidence = confidence
	item.DerivedItemID = derivedID
	item.SkippedReason = "" // clear any prior skip
	item.UpdatedAt = a.now()
	if err := a.store.UpdateClassification(ctx, item); err != nil {
		// Roll back the derived row we just created: if the source still looks
		// unclassified (DerivedItemID never persisted), the next Process/Accept
		// mints a SECOND derived item and orphans this one (audit H14).
		a.deleteDerived(ctx, item.ProposedCategory, derivedID)
		return err
	}
	// Lineage (UC-5): the classifier deriving a work item is a notable
	// step in the item's history. Best-effort.
	if ev := (&domain.ItemEvent{
		ID: a.newID(), OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "classified", Summary: "Classified as " + item.ProposedCategory,
		Source: "agent", CreatedAt: a.now(),
	}); a.store.RecordItemEvent(ctx, ev) != nil {
		a.log.Warn("lineage: record classified failed", "id", item.ID)
	}
	// Now that the new derivation is committed and the source item points
	// at it, retire the superseded one. Best-effort — a stale leftover is
	// a count bug, not worth failing the (successful) re-classification.
	if priorDerivedID != "" && priorDerivedID != derivedID {
		// Slice 4: carry the item's activity log (notes + history) over to the
		// fresh derived item so a reclassification doesn't orphan it, then retire
		// the old item. item.ProposedCategory is the NEW (lowercased) category.
		a.carryOverLog(ctx, priorCategory, priorDerivedID, item.ProposedCategory, derivedID)
		a.deleteDerived(ctx, priorCategory, priorDerivedID)
	}
	// Glass-box Phase 2: draft acceptance criteria for the fresh use_case/bug.
	// Best-effort + only when re-deriving (not on a no-op re-classify to the
	// same item) so we don't pile duplicate proposals.
	if derivedID != "" && derivedID != priorDerivedID {
		a.autoDraftCriteria(ctx, projectID, result.Category, derivedOwnerType(result.Category), derivedID, item.Content, result)
		// Sync the fresh item to any configured MCP-export destination — the
		// agent creates it directly, so the API's notifyExport never fires.
		a.notifyDerivedExport(ctx, result.Category, derivedID)
	}
	// Embed the source — handles the override path (Process never reached
	// the post-classify embed there). Normal path also re-embeds here on
	// Accept; trivially redundant since the content is unchanged.
	a.embedAndStore(ctx, storage.TableScratchpadItems, item.ID, item.Content)
	// Embed the freshly-derived item so it's searchable immediately.
	if dst := derivedTable(result.Category); dst != "" && derivedID != "" {
		text, err := a.store.GetEmbeddableText(ctx, dst, derivedID)
		if err != nil {
			a.log.Warn("derived embed text fetch failed",
				"table", string(dst), "id", derivedID, "err", err)
		} else {
			a.embedAndStore(ctx, dst, derivedID, text)
		}
	}
	return nil
}
