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

// Package mcpworker drains the mcp_export_queue: connects to each
// pending item's user-configured destination MCP server, dispatches
// the create / update / status_change / delete via mcpclient, and
// updates per-item sync state.
//
// Lives on `serve` (per the v0.8.2 design — the worker needs a long-
// lived process and the user_settings table). Async fire-and-forget:
// API hooks enqueue rows; the worker's Run loop processes them in the
// background. Failed pushes get exponential-backoff retries; a
// configurable max-attempts cap moves items to sync_status='failed' so
// the per-item indicator surfaces them.
//
// Connection caching is intentionally absent in v1: each tick connects,
// dispatches, closes. The cost is one MCP initialize handshake per
// queued op; the simplicity is worth it until profiling says otherwise.
package mcpworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
	"codedistill/internal/storage"
)

// Defaults strike a "responsive enough for human-scale work, gentle on
// remote APIs" balance. PollEvery=2s means a created todo shows up in
// Linear within ~2-3s. Backoff caps at 1h so a sustained outage doesn't
// hammer the destination.
const (
	defaultPollEvery   = 2 * time.Second
	defaultMaxBatch    = 10
	defaultBackoffInit = 30 * time.Second
	defaultBackoffMax  = 1 * time.Hour
	defaultBackoffMul  = 2.0
	defaultMaxAttempts = 8 // 30s, 1m, 2m, 4m, 8m, 16m, 32m, 1h ≈ 2.1h ceiling
)

// BackoffPolicy controls retry timing. Next attempt at:
//
//	min(Initial * Multiplier^attempts, Max)
//
// MaxAttempts is the hard cap: after this many failed attempts the
// item is marked sync_status='failed' and the queue row is deleted.
type BackoffPolicy struct {
	Initial     time.Duration
	Max         time.Duration
	Multiplier  float64
	MaxAttempts int
}

// nextDelay returns the delay before attempt #(attempts+1).
func (p BackoffPolicy) nextDelay(attempts int) time.Duration {
	mul := math.Pow(p.Multiplier, float64(attempts))
	d := time.Duration(float64(p.Initial) * mul)
	if d > p.Max || d < 0 {
		return p.Max
	}
	return d
}

// Config is what a Worker needs to talk to one user's destination for
// one item type: where to connect (Endpoint) and how to translate
// CodeDistill operations to that destination's tools (Mapping).
type Config struct {
	Endpoint mcpclient.Endpoint
	Mapping  mcpmap.Mapping
}

// ConfigLookup is how the worker reads a user's per-item-type export
// configuration. Defined as an interface so this package doesn't depend
// on the user_settings JSON schema (which step 5 / 7 finalize) and so
// tests can inject deterministic configs.
//
// shortType is one of "todo" | "bug" | "kb" | "use_case" — the user-
// facing short form. The worker translates ownerType → shortType
// internally via OwnerTypeToShort.
//
// Returns (nil, nil) when no destination is configured for that
// (user, type) — the worker treats that as "drop the queue row, mark
// the item sync_status='local-only'": the user un-configured the
// destination after the row was enqueued.
type ConfigLookup interface {
	Get(ctx context.Context, userID, shortType string) (*Config, error)
}

// OwnerTypeToShort translates the polymorphic owner_type column value
// (matching code_anchors / queue) to the user-friendly short form used
// in user_settings.mcp.export.<short>.
func OwnerTypeToShort(ownerType string) (string, error) {
	switch ownerType {
	case "todo_item":
		return "todo", nil
	case "bug_item":
		return "bug", nil
	case "knowledge_entry":
		return "kb", nil
	case "use_case_item":
		return "use_case", nil
	default:
		return "", fmt.Errorf("unknown owner_type %q", ownerType)
	}
}

// Options configures a Worker. Zero values fall back to defaults.
type Options struct {
	PollEvery time.Duration
	MaxBatch  int
	Backoff   BackoffPolicy
	Lookup    ConfigLookup
	Logger    *slog.Logger
	// Now is injectable for tests. Defaults to time.Now.
	Now func() time.Time
	// Connect is the constructor for an mcpclient.Client. Injectable
	// for tests so we can use httptest servers without round-tripping
	// through real DNS / sockets in a non-test context. Defaults to
	// mcpclient.New.
	Connect func(ctx context.Context, ep mcpclient.Endpoint) (Client, error)
}

// Client is the subset of *mcpclient.Client the worker uses. Defined
// here so Options.Connect can return a mockable type. *mcpclient.Client
// satisfies it.
type Client interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpclient.CallResult, error)
	Close() error
}

// Worker drains the export queue. Construct with New; call Run from a
// goroutine to start processing. Cancel ctx to stop.
type Worker struct {
	store   storage.Storage
	opts    Options
	log     *slog.Logger
	now     func() time.Time
	connect func(ctx context.Context, ep mcpclient.Endpoint) (Client, error)
}

func New(store storage.Storage, opts Options) *Worker {
	if opts.PollEvery <= 0 {
		opts.PollEvery = defaultPollEvery
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = defaultMaxBatch
	}
	if opts.Backoff.Initial <= 0 {
		opts.Backoff.Initial = defaultBackoffInit
	}
	if opts.Backoff.Max <= 0 {
		opts.Backoff.Max = defaultBackoffMax
	}
	if opts.Backoff.Multiplier <= 1 {
		opts.Backoff.Multiplier = defaultBackoffMul
	}
	if opts.Backoff.MaxAttempts <= 0 {
		opts.Backoff.MaxAttempts = defaultMaxAttempts
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	connect := opts.Connect
	if connect == nil {
		connect = func(ctx context.Context, ep mcpclient.Endpoint) (Client, error) {
			return mcpclient.New(ctx, ep)
		}
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default().With("component", "mcpworker")
	}
	return &Worker{
		store:   store,
		opts:    opts,
		log:     log,
		now:     now,
		connect: connect,
	}
}

// Run blocks until ctx is canceled. Each tick processes up to MaxBatch
// due items. Errors are logged but never propagate up — the loop is a
// long-lived background concern and a transient DB blip shouldn't kill
// it. A canceled ctx returns nil (clean shutdown).
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("starting", "poll_every", w.opts.PollEvery, "max_batch", w.opts.MaxBatch, "max_attempts", w.opts.Backoff.MaxAttempts)
	tick := time.NewTicker(w.opts.PollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			w.log.Info("stopping", "reason", ctx.Err())
			return nil
		case <-tick.C:
			if err := w.Tick(ctx); err != nil {
				w.log.Error("tick failed", "err", err)
			}
		}
	}
}

// Tick processes one batch of due items. Exported for tests.
func (w *Worker) Tick(ctx context.Context) error {
	items, err := w.store.ListDueExportItems(ctx, w.now(), w.opts.MaxBatch)
	if err != nil {
		return fmt.Errorf("list due: %w", err)
	}
	for _, it := range items {
		w.processOne(ctx, it)
	}
	return nil
}

// processOne handles a single queue row. Always runs to completion;
// any error is recorded as retry/failed state on the row + item rather
// than returned, so a bad row can't poison the rest of the batch.
func (w *Worker) processOne(ctx context.Context, item *domain.ExportQueueItem) {
	log := w.log.With("queue_id", item.ID, "owner_type", item.OwnerType, "owner_id", item.OwnerID, "op", item.Op)

	shortType, err := OwnerTypeToShort(item.OwnerType)
	if err != nil {
		// Unknown owner_type means a corrupt row — fail the item and
		// drop the row so it doesn't block the queue forever.
		log.Error("unknown owner_type; dropping", "err", err)
		w.permanentFail(ctx, item, fmt.Sprintf("unknown owner_type: %v", err))
		return
	}

	cfg, err := w.opts.Lookup.Get(ctx, item.UserID, shortType)
	if err != nil {
		// Lookup failure is treated as transient (e.g. DB blip): retry.
		w.scheduleRetry(ctx, item, fmt.Sprintf("config lookup: %v", err))
		return
	}
	if cfg == nil {
		// User unconfigured the destination after the row was enqueued.
		// Drop the row. We don't change the item's sync_status — it may
		// have legitimately been changed elsewhere.
		log.Info("no config for user/type; dropping row")
		_ = w.store.DeleteExportQueueItem(ctx, item.ID)
		return
	}

	toolName := cfg.Mapping[mcpmap.Operation(item.Op)]
	if toolName == "" && mcpmap.Operation(item.Op) == mcpmap.OpStatusChange {
		// status_change is a refinement of update: a destination that didn't
		// map it still wants the change, just via its update tool. Fall back
		// rather than dropping a status transition on the floor.
		toolName = cfg.Mapping[mcpmap.OpUpdate]
	}
	if toolName == "" {
		// The op isn't mapped on this destination. Permanent fail —
		// no point retrying until the user updates the mapping.
		log.Warn("op unavailable on destination; failing without retry")
		w.permanentFail(ctx, item, fmt.Sprintf("operation %q is not mapped to a destination tool", item.Op))
		return
	}

	args, err := decodeArgs(item.Payload)
	if err != nil {
		// Malformed payload → permanent fail. Retrying won't help.
		log.Error("decode payload", "err", err)
		w.permanentFail(ctx, item, fmt.Sprintf("decode payload: %v", err))
		return
	}

	cli, err := w.connect(ctx, cfg.Endpoint)
	if err != nil {
		w.scheduleRetry(ctx, item, fmt.Sprintf("connect: %v", err))
		return
	}
	defer cli.Close()

	res, err := cli.CallTool(ctx, toolName, args)
	if err != nil {
		// Transport / protocol error — retry.
		w.scheduleRetry(ctx, item, fmt.Sprintf("call %s: %v", toolName, err))
		return
	}
	if res.IsError {
		// Server-side rejection. Don't retry — the destination has
		// authoritatively refused. Surface via the per-item indicator.
		errMsg := joinTextContent(res.Content)
		if errMsg == "" {
			errMsg = "destination returned an error response"
		}
		log.Warn("destination returned IsError; failing without retry", "err", errMsg)
		w.permanentFail(ctx, item, errMsg)
		return
	}

	// Success. For 'create' ops, try to extract a remote_id from the
	// response so subsequent updates/deletes can target the right
	// remote item. Best-effort — if the response shape is opaque,
	// remote_id stays empty and the item is still marked synced.
	remoteID := ""
	if item.Op == string(mcpmap.OpCreate) {
		remoteID = extractRemoteID(res)
	}
	if err := w.store.MarkItemSynced(ctx, item.OwnerType, item.OwnerID, remoteID, w.now()); err != nil {
		// Item was likely deleted locally between enqueue and now. The
		// remote create succeeded but we can't record it — log loudly,
		// drop the row, and move on.
		log.Warn("mark synced failed (item may be gone locally)", "err", err)
	}
	if err := w.store.DeleteExportQueueItem(ctx, item.ID); err != nil {
		log.Error("delete queue row after success", "err", err)
	}
}

// scheduleRetry increments attempts and reschedules per the backoff
// policy. If MaxAttempts is reached, this becomes a permanent failure.
func (w *Worker) scheduleRetry(ctx context.Context, item *domain.ExportQueueItem, errMsg string) {
	attempts := item.Attempts + 1
	if attempts >= w.opts.Backoff.MaxAttempts {
		w.permanentFail(ctx, item, fmt.Sprintf("%s (gave up after %d attempts)", errMsg, attempts))
		return
	}
	nextAt := w.now().Add(w.opts.Backoff.nextDelay(attempts))
	if err := w.store.RetryExportQueueItem(ctx, item.ID, attempts, nextAt, errMsg); err != nil {
		// If we can't update the row, we'll just see it again on the
		// next tick (next_attempt_at unchanged). Log and move on.
		w.log.Error("schedule retry", "queue_id", item.ID, "err", err)
	}
}

// permanentFail marks the item sync_failed, deletes the queue row.
// Used when retrying won't help (server-side rejection, mapping gap,
// max attempts reached, corrupt row).
func (w *Worker) permanentFail(ctx context.Context, item *domain.ExportQueueItem, errMsg string) {
	if err := w.store.MarkItemSyncFailed(ctx, item.OwnerType, item.OwnerID, errMsg, w.now()); err != nil {
		// ErrNotFound here means the local item is gone — fine, the
		// queue row should still go away.
		if !errors.Is(err, storage.ErrNotFound) {
			w.log.Error("mark item failed", "queue_id", item.ID, "err", err)
		}
	}
	if err := w.store.DeleteExportQueueItem(ctx, item.ID); err != nil {
		w.log.Error("delete queue row after fail", "queue_id", item.ID, "err", err)
	}
}

// decodeArgs unmarshals the queue row's payload into a map suitable
// for mcpclient.CallTool. Empty payload → empty args (some tools take
// no arguments).
func decodeArgs(payload json.RawMessage) (map[string]any, error) {
	if len(payload) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(payload, &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// joinTextContent concatenates text blocks for use as an error message.
// Returns empty if there are no text blocks (caller picks a default).
func joinTextContent(blocks []mcpclient.ContentBlock) string {
	out := ""
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if out != "" {
				out += " "
			}
			out += b.Text
		}
	}
	return out
}

// extractRemoteID pulls a remote id out of a CallTool response, best-
// effort. We try the first text block, parse it as JSON, and look for
// "id" or "remote_id". Many real MCP destinations return their primary
// key under "id"; if not, the worker still marks synced but with an
// empty remote_id (subsequent update/delete ops won't be able to
// target the remote item — a known v1 limitation).
func extractRemoteID(res *mcpclient.CallResult) string {
	for _, b := range res.Content {
		if b.Type != "text" || b.Text == "" {
			continue
		}
		var probe map[string]any
		if err := json.Unmarshal([]byte(b.Text), &probe); err != nil {
			continue
		}
		for _, k := range []string{"id", "remote_id", "ID"} {
			if v, ok := probe[k]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
				if f, ok := v.(float64); ok {
					return fmt.Sprintf("%d", int64(f))
				}
			}
		}
	}
	return ""
}
