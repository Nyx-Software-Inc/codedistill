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

// codedistill is the single-binary entry point.
// Phase 1 commands: init, classify, serve, version, help.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	osuser "os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"codedistill/internal/agent"
	"codedistill/internal/api"
	"codedistill/internal/blobstore"
	"codedistill/internal/codeanalysis"
	"codedistill/internal/domain"
	embedpkg "codedistill/internal/embed"
	"codedistill/internal/events"
	"codedistill/internal/features"
	codegit "codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/licensing"
	mcppkg "codedistill/internal/mcp"
	"codedistill/internal/mcpworker"
	"codedistill/internal/ollama"
	"codedistill/internal/power"
	"codedistill/internal/storage"
	"codedistill/internal/storage/sqlite"
	"codedistill/internal/throttle"
	"codedistill/internal/verify"
	"codedistill/internal/webui"
)

// Build-time injected via -ldflags by build.sh. Defaults are dev-friendly so
// `go build ./cmd/codedistill` (no ldflags) still produces a runnable binary
// that clearly identifies itself as a non-release build.
var (
	version   = "0.1.0-dev"
	gitSHA    = "unknown"
	buildDate = "unknown"
)

const (
	defaultProjectID    = "default"
	defaultScratchpadID = "default"
)

func main() {
	dbPath := flag.String("db", "codedistill.db", "path to SQLite database file (single-user). Ignored when -database / $DATABASE_URL selects Postgres.")
	database := flag.String("database", "", "database DSN: postgres://… selects the Postgres backend (multi-user/server). Empty = SQLite from -db. Env: $DATABASE_URL")
	model := flag.String("model", ollama.DefaultModel, "Ollama model name")
	endpoint := flag.String("endpoint", ollama.DefaultEndpoint, "model server endpoint (Ollama, or a self-hosted OpenAI-compatible server)")
	modelAPI := flag.String("model-api", string(ollama.ProtocolOllama), "model server wire API: ollama | openai (openai = self-hosted OpenAI-compatible endpoint, e.g. vLLM/LM Studio — local-only, no cloud)")
	content := flag.String("content", "", "content to classify (used by 'classify')")
	override := flag.String("override", "", "optional Classification_Override for 'classify': "+domain.ClassificationOverrideList())
	distillFile := flag.String("file", "", "document to decompose (used by 'distill')")
	distillScratchpad := flag.String("scratchpad", "", "scratchpad the document and its items land in (used by 'distill')")
	distillAccept := flag.Bool("accept", false, "accept every proposal, creating real items (used by 'distill')")
	distillAgain := flag.Bool("again", false, "decompose even if this exact document was already read (used by 'distill')")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address (used by 'serve'). Defaults to localhost; binding a public interface requires write-auth (and you should add TLS).")
	appWindow := flag.String("app", "auto", "desktop app window for 'serve': auto|on|off. Opens the UI in a Chromium-family app window whose lifetime is tied to the server (close the window -> server stops; stop the server -> window closes). auto = on for interactive desktop sessions only, so services/headless never open windows.")
	noAuth := flag.Bool("no-auth", false, "disable the API write-auth token gate (DANGEROUS — trusted localhost only; refused on a public bind)")
	ollamaAutostart := flag.Bool("ollama-autostart", true, "auto-start ollama if not already running (serve)")
	ollamaBin := flag.String("ollama-bin", "", "explicit path to ollama binary (default: $PATH + common install locations)")
	force := flag.Bool("force", false, "required acknowledgement for destructive commands (reset, restore)")
	out := flag.String("out", "", "output path for 'backup' (default: <db>.backup-<timestamp>.db)")
	licensePath := flag.String("license", "", "explicit license file path (default: $CODEDISTILL_LICENSE, then <db dir>/"+licensing.FileName+", then user config dir)")
	secureCookiesFlag := flag.Bool("secure-cookies", false, "set the Secure attribute on auth/session cookies (enable when serving over HTTPS, including behind a TLS-terminating proxy). Env: $CODEDISTILL_SECURE_COOKIES=1")

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprint(out, `codedistill - scratchpad → auto-classified todos, bugs, and knowledge

usage: codedistill [flags] <command>

run:
  serve            start the HTTP API server + background classification agent
                   (the normal way to run CodeDistill)
  mcp              run as an MCP stdio server for agents (Claude Desktop /
                   Claude Code); safe alongside serve

setup:
  init             create or migrate the local SQLite database
  embed-pull       download the embedding model via the Ollama HTTP API
                   (run once while serve is up; no ollama CLI needed)

data:
  backup           write a consistent snapshot of the database
                   (-out sets the path; default <db>.backup-<timestamp>.db)
  restore <file>   replace the database with a snapshot (requires -force)
  reset            DELETE the database and start fresh (requires -force;
                   refuses if multi-user signals are detected)

documents:
  scratchpads      list projects and scratchpad ids (what -scratchpad wants)
  distill          decompose -file <doc.md> into proposed work items; prints
                   what it found and what it could not find. Add -accept to
                   create them, -scratchpad to choose where, -again to re-read
                   a document already decomposed. Reads .md/.txt/.odt/.docx.

maintenance:
  embed-backfill   embed any items with missing/stale embeddings (idempotent)
  index-code       chunk + embed every project's git repo for smarter
                   auto-anchoring (idempotent; re-run after big code changes)
  classify         classify one -content string end-to-end (smoke test;
                   requires Ollama running)

license:
  license status | install <file> | fingerprint
                   paid features (MCP, code anchors, multi-user, S3 blobs)
                   unlock with a signed license file; without one the binary
                   runs the free tier. Reads are never gated.

info:
  version          print the binary version
  help             show this help

flags:
`)
		flag.PrintDefaults()
		fmt.Fprint(out, `
examples:
  # One-time setup
  codedistill init

  # Run the full server on :8080 (HTTP API + agent)
  codedistill serve

  # Run on a custom port with a specific model
  codedistill -addr :9090 -model qwen2.5:14b serve

  # Classify one item from the CLI (smoke test)
  codedistill -content "clicking login throws 500" classify

  # Force a category (skips the model entirely)
  codedistill -content "update Go to 1.23 before Friday" -override bug classify

  # Snapshot the database before anything risky
  codedistill backup

  # Wipe the local DB and start over (single-user only; no recovery)
  codedistill -force reset

  # Run as an MCP stdio server (e.g. for Claude Desktop or Claude Code).
  # Add to ~/.config/claude/mcp.json or via "claude mcp add":
  #   "codedistill": { "command": "/path/to/codedistill", "args": ["-db", "/path/to/codedistill.db", "mcp"] }
  codedistill -db /path/to/codedistill.db mcp
`)
	}

	for _, a := range os.Args[1:] {
		if a == "-h" || a == "--help" || a == "help" {
			flag.CommandLine.SetOutput(os.Stdout)
			flag.Usage()
			os.Exit(0)
		}
	}

	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// Effective database target: -database flag, else $DATABASE_URL, else the
	// -db SQLite file. A postgres:// value routes to the Postgres backend
	// (OpenDSN dispatches); anything else is a SQLite path.
	dbArg := *dbPath
	if *database != "" {
		dbArg = *database
	} else if env := os.Getenv("DATABASE_URL"); env != "" {
		dbArg = env
	}

	switch flag.Arg(0) {
	case "version":
		fmt.Printf("%s (%s, %s)\n", version, gitSHA, buildDate)
	case "init":
		if err := cmdInit(dbArg); err != nil {
			die("init failed: %v", err)
		}
		fmt.Printf("initialized database at %s\n", dbArg)
	case "classify":
		if *content == "" {
			die("classify requires -content (try: codedistill help)")
		}
		if err := cmdClassify(dbArg, *model, *endpoint, *modelAPI, *content, *override); err != nil {
			die("classify failed: %v", err)
		}
	case "scratchpads":
		if err := cmdScratchpads(dbArg); err != nil {
			die("scratchpads failed: %v", err)
		}
	case "distill":
		if *distillFile == "" {
			die("distill requires -file <document.md> (try: codedistill help)")
		}
		if err := cmdDistill(dbArg, *model, *endpoint, *modelAPI, *distillFile, *distillScratchpad, *distillAccept, *distillAgain); err != nil {
			die("distill failed: %v", err)
		}
	case "serve":
		if err := cmdServe(dbArg, *model, *endpoint, *modelAPI, *addr, *appWindow, *ollamaAutostart, *ollamaBin, *licensePath, *noAuth, *secureCookiesFlag || os.Getenv("CODEDISTILL_SECURE_COOKIES") == "1"); err != nil {
			die("serve failed: %v", err)
		}
	case "mcp":
		if err := cmdMCP(dbArg, *licensePath); err != nil {
			die("mcp failed: %v", err)
		}
	case "license":
		if err := cmdLicense(dbArg, *licensePath, flag.Args()[1:]); err != nil {
			// No caller-side prefix: cmdLicense's errors are all self-describing,
			// and the two usage strings already name the command. The old
			// "license %s failed:" interpolated flag.Arg(1), which is EMPTY for a
			// bare `codedistill license` — producing "license  failed: usage: …",
			// i.e. a doubled space, the word "failed" in front of a help message,
			// and "license" twice in one line. This is the first command a paying
			// customer types (CE-review item 25).
			die("%v", err)
		}
	case "embed-backfill":
		if err := cmdEmbedBackfill(dbArg, *endpoint); err != nil {
			die("embed-backfill failed: %v", err)
		}
	case "embed-pull":
		if err := cmdEmbedPull(*endpoint); err != nil {
			die("embed-pull failed: %v", err)
		}
	case "index-code":
		if err := cmdIndexCode(dbArg, *endpoint); err != nil {
			die("index-code failed: %v", err)
		}
	case "reset":
		// dbArg, not *dbPath. These two destructive commands were the only ones
		// reading the raw -db value instead of the effective database, so on a
		// Postgres install they silently operated on whatever codedistill.db
		// happened to be in the working directory — deleting it and reporting
		// success while Postgres went untouched (CE-review item 14).
		if !*force {
			die("reset is destructive and requires -force; would delete %q", dbArg)
		}
		removed, err := cmdReset(dbArg)
		if err != nil {
			die("reset failed: %v", err)
		}
		if removed {
			fmt.Printf("reset: removed %s\n", dbArg)
		} else {
			fmt.Printf("reset: no database at %s (nothing to do)\n", dbArg)
		}
	case "backup":
		dest, err := cmdBackup(dbArg, *out)
		if err != nil {
			die("backup failed: %v", err)
		}
		fmt.Printf("backup: wrote %s\n", dest)
	case "restore":
		// dbArg for the same reason as reset above: restore would otherwise
		// overwrite a local SQLite file while the operator believed they were
		// restoring the Postgres database they selected.
		if err := cmdRestore(dbArg, flag.Arg(1), *force); err != nil {
			die("restore failed: %v", err)
		}
		fmt.Printf("restore: restored %s from %s\n", dbArg, flag.Arg(1))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
}

// cmdBackup writes a consistent SQLite snapshot (VACUUM INTO) to out, or a
// timestamped default beside the DB. Safe to run while `serve` is up.
func cmdBackup(dbArg, out string) (string, error) {
	if strings.HasPrefix(dbArg, "postgres://") || strings.HasPrefix(dbArg, "postgresql://") {
		return "", fmt.Errorf("backup is SQLite-only; for the Postgres backend use pg_dump")
	}
	if out == "" {
		out = dbArg + ".backup-" + time.Now().Format("20060102-150405") + ".db"
	}
	store, err := sqlite.OpenDSN(dbArg)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer store.Close()
	if err := store.Backup(context.Background(), out); err != nil {
		return "", err
	}
	return out, nil
}

// cmdRestore replaces the DB at dbPath with a backup file. It first moves the
// current DB aside (so the restore is itself reversible) and clears WAL/journal
// sidecars. Stop `serve` before restoring. Destructive → requires -force.
func cmdRestore(dbPath, backup string, force bool) error {
	if strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
		return fmt.Errorf("restore is SQLite-only")
	}
	if backup == "" {
		return fmt.Errorf("restore requires a backup file: codedistill -db <db> restore <backup>")
	}
	if !force {
		return fmt.Errorf("restore overwrites %q and requires -force (stop `serve` first)", dbPath)
	}
	if !isSQLiteFile(backup) {
		return fmt.Errorf("%s is not a SQLite database", backup)
	}
	// Move the current DB aside so this restore can itself be undone.
	if _, err := os.Stat(dbPath); err == nil {
		aside := dbPath + ".pre-restore-" + time.Now().Format("20060102-150405")
		if err := os.Rename(dbPath, aside); err != nil {
			return fmt.Errorf("save current database aside: %w", err)
		}
		fmt.Printf("restore: current database saved to %s\n", aside)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dbPath, err)
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove sidecar %s: %w", dbPath+suffix, err)
		}
	}
	return copyFile(backup, dbPath)
}

// isSQLiteFile checks the 16-byte SQLite header without opening (and thus
// mutating) the file.
func isSQLiteFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return false
	}
	return string(hdr[:15]) == "SQLite format 3"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstF, in); err != nil {
		dstF.Close()
		return err
	}
	return dstF.Close()
}

// cmdReset deletes the SQLite DB file at dbPath, defending against
// accidental multi-user data loss. The single-user invariant: at most one
// workspace exists, and if present its id is the implicit "local" seeded by
// migration 0006. Any deviation refuses the reset.
//
// Returns (removed, err). removed=false with err=nil means the file was
// already absent — nothing to do, not a failure.
func cmdReset(dbPath string) (bool, error) {
	// Mirrors cmdBackup. Without it a DSN fell through to os.Stat, missed, and
	// reported "nothing to do" — or, when the caller passed the raw -db default
	// instead of the effective database, deleted an unrelated SQLite file and
	// called it success.
	if strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
		return false, fmt.Errorf("reset is SQLite-only; for the Postgres backend drop and recreate the database with psql")
	}
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat: %w", err)
	}

	// Open + migrate so we can introspect the workspaces table even on
	// older DBs that pre-date migration 0006. After migration the seeded
	// 'local' workspace will be present on a previously-pristine DB; on
	// upgraded DBs whatever was there before is preserved and counted.
	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		return false, fmt.Errorf("migrate: %w", err)
	}
	workspaces, err := store.ListWorkspaces(ctx)
	if err != nil {
		store.Close()
		return false, fmt.Errorf("list workspaces: %w", err)
	}
	if err := store.Close(); err != nil {
		return false, fmt.Errorf("close: %w", err)
	}

	if len(workspaces) > 1 {
		return false, fmt.Errorf("refused: %d workspaces present (single-user mode only)", len(workspaces))
	}
	if len(workspaces) == 1 && workspaces[0].ID != "local" {
		return false, fmt.Errorf("refused: workspace %q is not the implicit single-user 'local' (multi-user mode detected)", workspaces[0].ID)
	}

	// Remove the main DB file plus any SQLite sidecars that may exist
	// (-journal from rollback-journal mode, -wal/-shm from WAL mode). We
	// don't enable WAL today, but the sidecars are cheap to handle and a
	// future PRAGMA change shouldn't strand stale files behind a reset.
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		p := dbPath + suffix
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove %s: %w", p, err)
		}
	}
	return true, nil
}

func cmdInit(dbPath string) error {
	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Migrate(context.Background())
}

func cmdClassify(dbPath, model, endpoint, modelAPI, content, override string) error {
	// Before opening storage, so a typo'd flag neither reaches the INSERT nor
	// creates a database as a side effect. Unvalidated, this surfaced as a raw
	// two-line "CHECK constraint failed … (275)" from SQLite — on the command
	// --help calls the smoke test (CE-review item 24).
	if !domain.ValidClassificationOverride(override) {
		return fmt.Errorf("invalid -override %q (want %s)", override, domain.ClassificationOverrideList())
	}

	ctx := context.Background()

	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if err := ensureDefaults(ctx, store); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}

	itemID := id.New()
	now := time.Now().UTC()
	item := &domain.ScratchpadItem{
		ID: itemID, ScratchpadID: defaultScratchpadID,
		ContentType: "text", Content: content,
		ClassificationState:    "unprocessed",
		ClassificationOverride: override,
		CreatedAt:              now, UpdatedAt: now,
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		return err
	}
	fmt.Printf("created scratchpad item %s\n", itemID)

	// This must build the SAME model stack as serve (:527-536). `classify` is
	// advertised in --help as a smoke test, so a stack that differs from the
	// server's is worse than no smoke test — it gives false confidence when it
	// passes and a false alarm when it diverges. Two pieces have to match:
	// WithProtocol, so -model-api openai reaches an OpenAI-compatible endpoint
	// instead of 404ing against Ollama's wire format; and GenerateModel+ModelFor,
	// so the per-project model.classifier setting is honoured rather than
	// silently falling back to the model-less path (CE-review item 12).
	cl := ollama.New(ollama.WithModel(model), ollama.WithEndpoint(endpoint),
		ollama.WithProtocol(ollama.ParseProtocol(modelAPI)))
	classifier := &agent.OllamaClassifier{
		GenerateModel: cl.GenerateJSONWithModel,
		ModelFor:      projectModel(store, "classifier"),
	}
	ag := agent.New(store, classifier)

	start := time.Now()
	if err := ag.Process(ctx, itemID); err != nil {
		return err
	}
	elapsed := time.Since(start)

	result, err := store.GetScratchpadItem(ctx, itemID)
	if err != nil {
		return err
	}
	printClassifyResult(result, elapsed)
	if result.DerivedItemID != "" {
		printDerived(ctx, store, result)
	}
	return nil
}

// cmdServe boots the HTTP API server and the background classification agent,
// seeds default Project/Scratchpad if missing, and shuts down cleanly on
// SIGINT / SIGTERM (HTTP drained first, then the agent queue, max 10s grace).
//
// When ollamaAutostart is true, a Supervisor probes the endpoint and spawns
// `ollama serve` if nothing's there. The child is killed during shutdown.
// ensureOllamaRunning is a seam. The supervisor starts a real child process, so
// tests that need to exercise cmdServe's cleanup paths replace this rather than
// spawning Ollama on the developer's machine.
var ensureOllamaRunning = func(ctx context.Context, sup *ollama.Supervisor) (func(), error) {
	return sup.EnsureRunning(ctx)
}

func cmdServe(dbPath, model, endpoint, modelAPI, addr, appWindow string, ollamaAutostart bool, ollamaBin, licensePath string, noAuth, secureCookies bool) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if err := ensureDefaults(ctx, store); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}

	// Jobs are persisted, so a run that was going when the process died is
	// still marked running. Clear those to `interrupted` BEFORE anything can
	// start a new one — otherwise a killed architecture draft holds its project
	// forever and the panel never lets you draft again. Never fatal: failing to
	// tidy up must not stop the server from serving.
	if n, err := store.InterruptStaleJobs(ctx, time.Now().UTC()); err != nil {
		slog.Warn("could not clear interrupted jobs", "err", err)
	} else if n > 0 {
		slog.Info("cleared jobs interrupted by a restart", "count", n)
	}

	logger := slog.Default()

	// License: verified once at startup. Degrade-to-free semantics — a
	// missing/expired/invalid license never stops serve, it only flips
	// paid features off (reads of existing data are never gated).
	// Installing a license (`codedistill license install`) requires a
	// restart to take effect.
	lic := licensing.Load(licensePath, dbPath, version)
	features.Init(lic)
	logLicense(logger, lic)
	// Attribution (multi-user groundwork): name the implicit local user after the
	// license holder, so the UI + timeline show "Rich" instead of "Local User".
	syncLocalUserDisplayName(ctx, store, lic)

	// Auto-start Ollama before the agent so the first classification attempt
	// doesn't hit a dead socket. A no-op if Ollama is already running.
	stopOllama := func() {}
	// Cleanup must run on EVERY exit, not just the clean one. Five paths below
	// returned while leaving the child we started alive — each orphan holds its
	// model resident, which since desktop provisioning landed means ~9GB for
	// qwen2.5:14b (CE-review item 16).
	//
	// sync.Once matters because the happy path still calls this explicitly at the
	// end, where the ORDER is load-bearing: Ollama must outlive the agent drain so
	// in-flight classifications can finish. This defer is the backstop for
	// abnormal exits; on the clean path it is a no-op because the explicit call
	// already ran.
	var stopOnce sync.Once
	stopOllamaOnce := func() { stopOnce.Do(func() { stopOllama() }) }
	defer stopOllamaOnce()
	// Only autostart the local Ollama binary when we're actually talking to
	// Ollama — an OpenAI-compatible endpoint is the user's own running server.
	if ollamaAutostart && ollama.ParseProtocol(modelAPI) == ollama.ProtocolOllama {
		sup := &ollama.Supervisor{Endpoint: endpoint, BinPath: ollamaBin, Log: logger}
		stop, err := ensureOllamaRunning(ctx, sup)
		if err != nil {
			return fmt.Errorf("ollama supervisor: %w", err)
		}
		stopOllama = stop
	}

	cl := ollama.New(ollama.WithModel(model), ollama.WithEndpoint(endpoint),
		ollama.WithProtocol(ollama.ParseProtocol(modelAPI)))
	// Per-role model resolution (the model-provider arc): each LLM role runs
	// against the project's configured model.<role> setting, falling back to the
	// global default. Local-only — models come from the machine's Ollama.
	classifier := &agent.OllamaClassifier{
		GenerateModel: cl.GenerateJSONWithModel,
		ModelFor:      projectModel(store, "classifier"),
	}
	// Separate Ollama client for the embedding model — the API takes a
	// model per call but the client wraps a single configured one, so a
	// second instance is the simplest split. Pulled with `ollama pull
	// nomic-embed-text` once; failure to pull surfaces as a logged warn
	// per item, classification still completes.
	embedCl := ollama.New(ollama.WithModel(ollama.DefaultEmbedModel), ollama.WithEndpoint(endpoint))

	// Event bus drives the SSE change feed. Created before agent +
	// API server so both can publish through the same instance.
	bus := events.NewBus()

	// AI classification is free-tier; the file-tree option that powers
	// auto-anchor during classification is part of the paid anchors
	// feature, so it's only wired when licensed.
	// MCP outbound export (#9 client, v0.8.2). The hook is wired into every
	// item-write API handler AND the classifier agent (so agent-derived items
	// sync too); the worker drains the queue in the background. All share one
	// SettingsLookup so config changes (via PUT
	// /users/local/settings/mcp.export.<short>) take effect on the next call
	// without restart. Paid (mcp feature): a nil hook is a no-op everywhere.
	var exportHook *mcpworker.Hook
	if features.Enabled(features.MCPServer) {
		exportLookup := mcpworker.NewSettingsLookup(store)
		exportHook = mcpworker.NewHook(store, exportLookup, logger)
		exportWorker := mcpworker.New(store, mcpworker.Options{Lookup: exportLookup, Logger: logger})
		go func() { _ = exportWorker.Run(ctx) }()
	}

	agentOpts := []agent.Option{
		agent.WithEmbedder(embedCl),
		agent.WithEventBus(bus),
		// Reconciliation sweep: hourly (+ on startup), re-enqueue any stranded
		// captures (unprocessed / failed / orphaned processing) so a lost enqueue
		// or a transient Ollama outage never leaves an item stuck. Gated on model
		// reachability so it does no work while Ollama is down.
		agent.WithReconcile(time.Hour, cl.Reachable),
		// Acceptance-criteria drafting (glass-box Phase 2) — free local-Ollama
		// assist; resolves the project's criteria model (model-provider arc).
		agent.WithCriteriaDrafter(&agent.OllamaCriteriaDrafter{
			GenerateModel: cl.GenerateJSONWithModel,
			ModelFor:      projectModel(store, "criteria"),
		}),
	}
	if exportHook != nil {
		agentOpts = append(agentOpts, agent.WithExportNotify(exportHook.Notify))
	}
	// Auto-anchoring is paid. WithCodeAnchors is the authoritative gate — it also
	// covers the embedded-chunk search, which WithProjectFileTree does not reach
	// and which used to auto-anchor unlicensed whenever a database still carried
	// chunks from an earlier licensed run (CE-review item 28).
	agentOpts = append(agentOpts, agent.WithCodeAnchors(features.Enabled(features.CodeAnchors)))
	if features.Enabled(features.CodeAnchors) {
		agentOpts = append(agentOpts, agent.WithProjectFileTree(projectFileTreeLookup(store)))
	}
	// Duplicate intelligence is paid (features.Dedup) and lives in
	// internal/dedup, which the public AGPL mirror strips entirely. The
	// constructor is provided by a //go:build !oss seam file
	// (dedup_full.go); the oss build's stub returns nil. So in the free
	// edition there is no dedup impl to enable, and in the commercial
	// edition it's gated on the license. The same grouper instance backs
	// the agent's incremental pass and the server's on-demand scan.
	var grouper agent.DuplicateGrouper
	if features.Enabled(features.Dedup) {
		grouper = newDuplicateGrouper(store, logger)
	}
	if grouper != nil {
		agentOpts = append(agentOpts, agent.WithDuplicateGrouper(grouper))
		// One-time backfill (UC-38): tidy legacy groups whose members were
		// tagged but never colocated, so existing scattered groups get packed
		// on first run after upgrade. Guarded per-project inside; background.
		if rt, ok := grouper.(interface{ RetidyGroups(context.Context) }); ok {
			go rt.RetidyGroups(ctx)
		}
	}
	ag := agent.New(store, classifier, agentOpts...)
	ag.Start(ctx)

	// Watched-directory analysis producer (Enterprise): ingest SARIF files that
	// CI drops into each project's configured analysis.watch_dir. Gated on the
	// paid Analysis feature; a background poll loop that stops with the context.
	if features.Enabled(features.Analysis) {
		go codeanalysis.NewWatcher(store, logger, func() { bus.Publish(events.ItemsChanged) }).Run(ctx)
	}

	// Power-source watcher (battery-aware throttle, v0.8.3). Polls every
	// 30s in the background. On machines with no battery hardware
	// (desktops, servers) it reports AC permanently and the throttle is
	// a no-op — no mode flag needed.
	powerW := power.NewWatcher(0, logger)
	go powerW.Run(ctx)

	// Battery-aware throttle reader, shared across every background
	// watcher. On AC it returns the zero policy and consumers behave
	// as if throttling didn't exist. On battery it reads the user's
	// stored Pause + Multiplier and applies them via ScaleInterval +
	// ScalePace at each consumer's natural injection points.
	throttleReader := throttle.NewReader(store, "local", powerW.Current, logger)

	// Code-chunk indexer watcher (4b): periodic IndexProject for every
	// project with repo_root configured. Idempotent — mtime fast-path
	// skips unchanged files. First-time projects get an initial pass on
	// boot; subsequent ticks (60s default) catch up to file edits.
	// Paid (anchors feature): the index exists to power auto-anchor.
	if features.Enabled(features.CodeAnchors) {
		startCodeIndexWatcher(ctx, store, embedCl, throttleReader, bus, logger)
	}

	// Automatic embedding backfill (free-tier infra): a transient embed-model
	// outage when an item was classified leaves it permanently unembedded —
	// invisible to dedup AND semantic search — since embed failures are
	// non-fatal. This background sweep re-embeds any unembedded rows so that
	// self-heals instead of needing a manual `embed-backfill` run.
	startEmbedBackfillWatcher(ctx, store, embedCl, logger)

	// (The implementation matcher was retired in v0.11.0 — guess-after-the-
	// fact commit↔item association was noisy and is replaced by agent-
	// authored + user-drawn anchors. See docs/design/glass-box-plan.md #1.)

	// Compose: /api/v1/* → REST handlers; /mcp → Streamable-HTTP MCP
	// transport sharing the same store; everything else → embedded SPA.
	// Go 1.22+ ServeMux picks the longer matching prefix, so the API and
	// MCP routes win over the catch-all "/" that serves static files.
	root := http.NewServeMux()
	build := api.BuildInfo{Version: version, GitSHA: gitSHA, BuildDate: buildDate}

	// Rich-canvas blob store. Two backends behind the same
	// interface (per docs/design/rich-canvas-design.md, Slice 5):
	//
	//   - LocalStore (default): content-addressed FS rooted as a
	//     sibling to the DB file (`<db>.blobs/`). Used by single-
	//     user installs and self-hosted deployments that don't need
	//     horizontal scale.
	//   - S3Store: any S3-compatible endpoint (AWS S3, R2, GCS,
	//     MinIO). Selected when BLOB_S3_ENDPOINT is set in the env.
	//     Credentials come from env vars (never the DB) so secret
	//     management is a standard ops concern, not application
	//     state.
	//
	// The GC sweeper (which walks the FS) only runs against
	// LocalStore. S3 deployments use bucket lifecycle policies for
	// the same job.
	var blobs blobstore.BlobStore
	var localBlobs *blobstore.LocalStore
	if endpoint := os.Getenv("BLOB_S3_ENDPOINT"); endpoint != "" {
		// Refuse rather than silently fall back to the local store —
		// splitting blobs across two backends mid-deployment would be
		// far worse than a clear startup error.
		if !features.Enabled(features.S3Blob) {
			return fmt.Errorf("BLOB_S3_ENDPOINT is set but the S3 backend requires an enterprise license (unset the env var to use local blob storage, or run `codedistill license status`)")
		}
		s3, err := newS3Blob(endpoint)
		if err != nil {
			return fmt.Errorf("s3 blobstore init: %w", err)
		}
		blobs = s3
		logger.Info("blobstore: S3", "endpoint", endpoint, "bucket", os.Getenv("BLOB_S3_BUCKET"))
	} else {
		// Local blob root. SQLite installs default to a sibling of the DB file
		// (`<db>.blobs/`). A Postgres DSN has no sibling dir, so the blob dir is
		// taken from $CODEDISTILL_BLOBS (set by the server package); fall back to
		// a cwd-relative dir rather than a nonsense DSN-derived path.
		blobRoot := os.Getenv("CODEDISTILL_BLOBS")
		if blobRoot == "" {
			if strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
				blobRoot = "codedistill.blobs"
			} else {
				blobRoot = dbPath + ".blobs"
			}
		}
		local, err := blobstore.NewLocalStore(blobRoot)
		if err != nil {
			return fmt.Errorf("blobstore init at %s: %w", blobRoot, err)
		}
		blobs = local
		localBlobs = local
		logger.Info("blobstore: local", "root", blobRoot)
	}

	// Deterministic verification layer (glass-box Phase 3). One instance shared
	// by the API manual-trigger endpoint and MCP record_implementation's auto-run
	// so both respect the same per-item in-flight guard. Serve-only — it hosts
	// the async runs.
	verifier := verify.NewService(store, func(f string, a ...any) { logger.Warn(fmt.Sprintf(f, a...)) }).
		WithReviewer(verify.NewOllamaReviewer(cl.GenerateJSONWithModel))
	// Clear any verification rows left at "running" by a prior crash/restart
	// so they don't show "running…" forever (audit H9).
	verifier.RecoverStaleRuns(context.Background())

	// Prune expired login sessions on startup + hourly. GetValidSession already
	// rejects them; this just stops the rows accumulating (audit M22).
	pruneSessions := func() {
		if n, err := store.DeleteExpiredSessions(context.Background(), time.Now().UTC()); err == nil && n > 0 {
			logger.Info("pruned expired sessions", "count", n)
		}
	}
	pruneSessions()
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pruneSessions()
			}
		}
	}()

	// In-app license redemption (activation service slice 2). The dest is
	// wherever this install resolves its license file; the service URL is
	// overridable for sandbox/dev testing.
	licenseDest, _ := licensing.ResolvePath(licensePath, dbPath)
	activateURL := os.Getenv("CODEDISTILL_ACTIVATE_URL")
	if activateURL == "" {
		activateURL = "https://activate.codedistill.dev"
	}
	apiSrv := api.NewServer(store, ag, build, logger, exportHook, cl, embedCl, cl).
		WithLicenseRedeem(licenseDest, activateURL).
		WithPowerSource(powerW.Current).
		WithBlobStore(blobs, api.DefaultBlobConfig()).
		WithEventBus(bus).
		WithVerifier(verifier).
		WithModelLister(cl.ListModels).
		WithSecureCookies(secureCookies)
	if grouper != nil {
		apiSrv = apiSrv.WithDuplicateGrouper(grouper)
	}
	// OIDC login (multi-user). Configured via env; with no issuer set the server
	// stays single-user (currentUser = 'local'). A misconfig/unreachable IdP is
	// logged and auth is disabled rather than failing serve.
	if issuer := os.Getenv("CODEDISTILL_OIDC_ISSUER"); issuer != "" {
		clientID := os.Getenv("CODEDISTILL_OIDC_CLIENT_ID")
		redirect := os.Getenv("CODEDISTILL_OIDC_REDIRECT_URL")
		if clientID == "" || redirect == "" {
			logger.Error("OIDC misconfigured: CODEDISTILL_OIDC_ISSUER set but CLIENT_ID/REDIRECT_URL missing — auth disabled")
		} else if auth, err := api.NewOIDCAuth(ctx, issuer, clientID, os.Getenv("CODEDISTILL_OIDC_CLIENT_SECRET"), redirect); err != nil {
			logger.Error("OIDC provider init failed — auth disabled (server stays single-user)", "issuer", issuer, "err", err)
		} else {
			apiSrv = apiSrv.WithAuth(auth)
			// Bootstrap admin: explicit env, else the license holder's email.
			adminEmail := os.Getenv("CODEDISTILL_ADMIN_EMAIL")
			if adminEmail == "" && lic != nil && lic.License != nil {
				adminEmail = lic.License.Email
			}
			var domains []string
			if d := os.Getenv("CODEDISTILL_ALLOWED_DOMAINS"); d != "" {
				domains = strings.Split(d, ",")
			}
			apiSrv = apiSrv.WithMembershipPolicy(adminEmail, domains)
			logger.Info("OIDC login enabled", "issuer", issuer, "bootstrap_admin", adminEmail != "", "allowlist", len(domains) > 0)
		}
	}
	// Write-auth: gate state-changing API requests behind a token (reads
	// stay open). A localhost-bound desktop install is protected from the
	// LAN by the bind; the token stops anonymous *writes* and authorizes
	// agents. The first-party SPA gets the token via an HttpOnly cookie
	// (below); programmatic callers send it as a bearer header.
	apiToken := ""
	if !noAuth {
		t, tokPath, err := loadOrCreateAPIToken()
		if err != nil {
			logger.Warn("api write-auth: could not load/create token; writes UNGATED", "err", err)
		} else {
			apiToken = t
			logger.Info("api write-auth enabled", "token_file", tokPath)
		}
	}
	if !isLocalBind(addr) {
		if apiToken == "" {
			// Returns rather than os.Exit: deferred cleanup never runs on
			// os.Exit, so this guard used to orphan Ollama unstoppably — and it
			// is the guard a user trips repeatedly while getting -addr right.
			// main's dispatch still exits 1 via die().
			return fmt.Errorf("refusing to serve on a non-localhost address (%s) without write-auth — bind 127.0.0.1, or drop -no-auth", addr)
		}
		logger.Warn("serving on a non-localhost address: reads are UNAUTHENTICATED — front it with TLS + a reverse proxy", "addr", addr)
	}
	apiSrv = apiSrv.WithAuthToken(apiToken, persistAPIToken)
	root.Handle("/api/v1/", apiSrv.Handler())
	// MCP transport: mounted on EVERY install — "MCP read free" is the
	// funnel hook, so any agent can pull intent. Write tools are registered
	// only when the mcp feature is licensed (includeWrites) and additionally
	// require the API token at call time over HTTP (stdio is trusted-local).
	mcpWrites := features.Enabled(features.MCPServer)
	mcpSrv := mcppkg.NewServer(store, version, ag, verifier, blobs, api.DefaultBlobConfig().URLTTL, mcpWrites, func() { bus.Publish(events.ItemsChanged) })
	// apiSrv.TokenValue (the METHOD, not its result) so /mcp resolves the token
	// per request — a captured copy survives a rotation and keeps honouring the
	// revoked token while refusing the new one.
	root.Handle("/mcp", mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithHTTPContextFunc(mcppkg.AuthContextFunc(apiSrv.TokenValue))))
	// .webmanifest isn't in Go's built-in MIME table; without this the
	// PWA manifest serves as text/plain and Chrome refuses to install.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	var spa http.Handler = http.FileServer(http.FS(webui.FS()))
	if apiToken != "" {
		// Set the first-party auth cookie when serving the app shell so the
		// SPA can make write requests without the token ever touching JS.
		spa = withAuthCookie(apiSrv.TokenValue, secureCookies, spa)
	}
	root.Handle("/", spa)

	// Background blob GC for the local-FS impl only. S3 deployments
	// rely on bucket lifecycle policies for the same job (configure
	// expiration on the bucket; the application doesn't enumerate
	// keys).
	if localBlobs != nil {
		blobGC := blobstore.NewGCWatcher(store, localBlobs, time.Hour, 24*time.Hour, logger)
		go blobGC.Run(ctx)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Appropriate Legal Notice on startup. The binding obligation for a
	// network-served UI is AGPL-3.0 §13 (offer the Corresponding Source to
	// remote users), which the web UI's About dialog carries; this is the
	// console-side counterpart for anyone running `serve` headless, where that
	// dialog is never seen.
	if features.OSSBuild {
		logger.Info("CodeDistill Community Edition — Copyright (c) 2026 Nyx Software, Inc. " +
			"Free software under the GNU AGPL-3.0, with ABSOLUTELY NO WARRANTY. " +
			"Source: https://github.com/Nyx-Software-Inc/codedistill")
	} else {
		logger.Info("CodeDistill — Copyright (c) 2026 Nyx Software, Inc. " +
			"Licensed commercially; also published as free software under the GNU AGPL-3.0. " +
			"Source: https://github.com/Nyx-Software-Inc/codedistill")
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("codedistill listening", "addr", addr, "model", model, "db", dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	// Desktop app window: lifetime-coupled by process supervision. A nil
	// windowClosed channel (app mode off, or no Chromium-family browser)
	// blocks forever in the select — serve then behaves exactly as before.
	var windowClosed <-chan struct{}
	stopWindow := func() {}
	if resolveAppMode(appWindow) {
		windowClosed, stopWindow = launchAppWindow(ctx, addr, logger)
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case <-windowClosed:
		logger.Info("app window closed — shutting down")
	case err := <-serveErr:
		if err != nil {
			stopWindow()
			return err
		}
	}

	// Close the window first so the UI disappears the moment shutdown starts.
	stopWindow()

	// Graceful shutdown: stop accepting HTTP first, then drain the agent
	// queue (max 10s grace per Req 14.3).
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "err", err)
	}
	ag.Stop()
	// Stop Ollama only after the agent drains — in-flight classifications need it.
	// Via the Once so the backstop defer above doesn't stop it a second time.
	stopOllamaOnce()
	logger.Info("stopped cleanly")
	return nil
}

// cmdMCP runs codedistill as an MCP stdio server. The process speaks
// JSON-RPC over stdin/stdout; clients (Claude Desktop, Claude Code,
// the Inspector) launch this as a subprocess and pipe through it.
//
// Read-mostly v1 surface — see internal/mcp/tools.go. No HTTP, no
// classification agent: this process exists purely to expose the
// existing DB to an LLM client. Safe to run alongside `codedistill
// serve` thanks to WAL mode (PRAGMA journal_mode=WAL is set in
// sqlite.Open). All writes go through Storage so the same business
// rules and CHECK constraints apply.
//
// Logging routes to stderr only — anything on stdout corrupts the
// JSON-RPC stream. slog.Default() writes to stderr, but we set it
// explicitly here as a guard in case something earlier flipped it.
func cmdMCP(dbPath, licensePath string) error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// MCP reads are the free funnel hook — same as the HTTP /mcp transport
	// (any agent can pull intent). MCP *writes* are the paid `mcp` feature. So
	// stdio serves on every build and gates only writes on the license; it no
	// longer refuses outright (that broke the read-free funnel over stdio).
	lic := licensing.Load(licensePath, dbPath, version)
	features.Init(lic)
	includeWrites := features.Enabled(features.MCPServer)
	if !includeWrites {
		// stdout is the JSON-RPC stream — diagnostics go to stderr.
		fmt.Fprintln(os.Stderr, "codedistill mcp: read-only mode (MCP write tools require a CodeDistill Pro license; run `codedistill license status`)")
	}

	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}

	// No enqueuer in stdio mode — no agent runs here. create_scratchpad_item
	// is still registered; items created via stdio land in 'unprocessed'
	// and get classified the next time `codedistill serve` runs.
	// No blob store either — stdio MCP runs without an HTTP server, so
	// any blob URL would be unreachable; read_item simply omits it.
	// No verifier in stdio mode — no long-lived process to host async runs.
	srv := mcppkg.NewServer(store, version, nil, nil, nil, 0, includeWrites, nil)
	// ServeStdio blocks until stdin closes (client disconnect) or the
	// process is signaled. Returns nil on clean shutdown.
	return mcpserver.ServeStdio(srv)
}

// logLicense emits the one startup line that says what commercial
// state the binary is running in.
func logLicense(logger *slog.Logger, lic *licensing.Status) {
	switch lic.State {
	case licensing.StateValid:
		logger.Info("license: valid",
			"edition", lic.License.Edition, "customer", lic.License.Customer,
			"features", features.EnabledNames())
	case licensing.StateGrace:
		logger.Warn("license: EXPIRED — in grace period, renew now",
			"grace_until", lic.GraceUntil.Format("2006-01-02"),
			"customer", lic.License.Customer)
	case licensing.StateExpired:
		logger.Warn("license: expired past grace — running free tier", "reason", lic.Reason)
	case licensing.StateInvalid:
		logger.Warn("license: INVALID — running free tier", "reason", lic.Reason)
	default:
		logger.Info("license: none — running free tier (paid features off)")
	}
}

// cmdLicense implements `codedistill license <status|install|fingerprint>`.
func cmdLicense(dbPath, licensePath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codedistill license <status|install <file>|fingerprint>")
	}
	switch args[0] {
	case "status":
		lic := licensing.Load(licensePath, dbPath, version)
		features.Init(lic)
		fmt.Printf("state:    %s\n", lic.State)
		if lic.Reason != "" {
			fmt.Printf("reason:   %s\n", lic.Reason)
		}
		if l := lic.License; l != nil {
			fmt.Printf("customer: %s\nedition:  %s (seats: %d)\n", l.Customer, l.Edition, l.Seats)
			if !l.ExpiresAt.IsZero() {
				fmt.Printf("expires:  %s\n", l.ExpiresAt.Format("2006-01-02"))
			}
			if !lic.GraceUntil.IsZero() && lic.State != licensing.StateValid {
				fmt.Printf("grace:    until %s\n", lic.GraceUntil.Format("2006-01-02"))
			}
		}
		fmt.Printf("features: %v\n", features.EnabledNames())
		return nil
	case "install":
		if len(args) != 2 {
			return fmt.Errorf("usage: codedistill license install <file>")
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			// Context belongs here now that the dispatch site adds no prefix —
			// otherwise this surfaced as a bare "open /x: no such file or
			// directory" with nothing saying it was a license install.
			return fmt.Errorf("read license file: %w", err)
		}
		// Verify before installing — a typo'd or corrupt file should
		// fail here, not silently downgrade the next serve. VerifyAny checks
		// the full trust list (master + self-serve) — the single-key path
		// rejected every purchased self-serve license (audit C3).
		fp, _ := licensing.Fingerprint()
		st := licensing.VerifyAny(raw, licensing.PublicKeys(), time.Now().UTC(), fp, version)
		if !st.Active() {
			return fmt.Errorf("refusing to install: license is %s (%s)", st.State, st.Reason)
		}
		dest, _ := licensing.ResolvePath(licensePath, dbPath)
		// 0600: least-privilege — only the owner running serve needs to read it.
		if err := os.WriteFile(dest, raw, 0o600); err != nil {
			return fmt.Errorf("install license: %w", err)
		}
		fmt.Printf("installed %s license for %q at %s\n", st.License.Edition, st.License.Customer, dest)
		fmt.Println("restart `codedistill serve` to apply")
		return nil
	case "fingerprint":
		fp, err := licensing.Fingerprint()
		if err != nil {
			return fmt.Errorf("could not compute machine fingerprint: %w", err)
		}
		fmt.Println(fp)
		fmt.Fprintln(os.Stderr, "send this with your order for a node-locked license")
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q (want status, install, or fingerprint)", args[0])
	}
}

// syncLocalUserDisplayName sets the implicit local user's display name from the
// license customer (falling back to the OS user, else the seeded "Local User"),
// so single-user attribution surfaces a real name. Best-effort: never blocks serve.
func syncLocalUserDisplayName(ctx context.Context, s storage.Storage, lic *licensing.Status) {
	name := ""
	if lic != nil && lic.License != nil {
		name = strings.TrimSpace(lic.License.Customer)
	}
	if name == "" {
		if u, err := osuser.Current(); err == nil {
			if u.Name != "" {
				name = u.Name
			} else {
				name = u.Username
			}
		}
	}
	if name == "" {
		return
	}
	u, err := s.GetUser(ctx, "local")
	if err != nil || u.DisplayName == name {
		return
	}
	// Don't clobber a name the user customized (profile → Edit name); only
	// adopt the license name while it's still the seeded default.
	if u.DisplayName != "" && u.DisplayName != "Local User" {
		return
	}
	u.DisplayName = name
	_ = s.UpdateUser(ctx, u)
}

func ensureDefaults(ctx context.Context, s storage.Storage) error {
	now := time.Now().UTC()
	if _, err := s.GetProject(ctx, defaultProjectID); errors.Is(err, storage.ErrNotFound) {
		if err := s.CreateProject(ctx, &domain.Project{
			ID: defaultProjectID, Name: "Default", CreatedAt: now,
		}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := s.GetScratchpad(ctx, defaultScratchpadID); errors.Is(err, storage.ErrNotFound) {
		return s.CreateScratchpad(ctx, &domain.Scratchpad{
			ID: defaultScratchpadID, ProjectID: defaultProjectID,
			Name: "main", ClassificationMode: "full", CreatedAt: now,
		})
	} else if err != nil {
		return err
	}
	return nil
}

func printClassifyResult(i *domain.ScratchpadItem, elapsed time.Duration) {
	fmt.Printf("\n--- result (%.2fs) ---\n", elapsed.Seconds())
	fmt.Printf("state:     %s\n", i.ClassificationState)
	if i.ProposedCategory != "" {
		fmt.Printf("category:  %s\n", i.ProposedCategory)
	}
	if i.ClassificationReasoning != "" {
		fmt.Printf("reasoning: %s\n", i.ClassificationReasoning)
	}
	if i.SkippedReason != "" {
		fmt.Printf("skipped:   %s\n", i.SkippedReason)
	}
	if i.DerivedItemID != "" {
		fmt.Printf("derived:   %s\n", i.DerivedItemID)
	}
}

func printDerived(ctx context.Context, s storage.Storage, i *domain.ScratchpadItem) {
	switch i.ProposedCategory {
	case "todo":
		t, err := s.GetTodoItem(ctx, i.DerivedItemID)
		if err == nil {
			fmt.Printf("\n--- derived todo ---\nsubject: %s\npriority: %s\nstatus: %s\n", t.Subject, t.Priority, t.Status)
		}
	case "bug":
		b, err := s.GetBugItem(ctx, i.DerivedItemID)
		if err == nil {
			fmt.Printf("\n--- derived bug ---\nsubject: %s\nseverity: %s\nstatus: %s\n", b.Subject, b.Severity, b.Status)
		}
	case "kb":
		k, err := s.GetKnowledgeEntry(ctx, i.DerivedItemID)
		if err == nil {
			fmt.Printf("\n--- derived kb ---\ntitle: %s\n", k.Title)
		}
	}
}

// cmdEmbedPull pulls the embedding model from the Ollama registry via
// the HTTP API — no `ollama` CLI on PATH required (the standalone
// tarball install puts it under ~/.local/ollama/bin which often isn't
// on $PATH). Streams progress lines to stdout. Idempotent: re-pulling
// an already-installed model is fast.
func cmdEmbedPull(endpoint string) error {
	body, err := json.Marshal(map[string]any{"model": ollama.DefaultEmbedModel})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama pull: %w (is `codedistill serve` running?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(b))
	}
	dec := json.NewDecoder(resp.Body)
	var lastStatus string
	for {
		var line struct {
			Status    string `json:"status"`
			Total     int64  `json:"total,omitempty"`
			Completed int64  `json:"completed,omitempty"`
		}
		if err := dec.Decode(&line); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode pull progress: %w", err)
		}
		// Coalesce repeated identical status lines (Ollama emits many during
		// a download); print transitions + the final success line.
		if line.Status != lastStatus {
			if line.Total > 0 {
				fmt.Printf("  %s (%d / %d bytes)\n", line.Status, line.Completed, line.Total)
			} else {
				fmt.Printf("  %s\n", line.Status)
			}
			lastStatus = line.Status
		}
	}
	fmt.Printf("done: %s ready to use\n", ollama.DefaultEmbedModel)
	return nil
}

// startEmbedBackfillWatcher periodically sweeps unembedded rows and embeds them
// in the background, so an item that failed to embed (e.g. the embed model was
// down at classify time) self-heals instead of staying permanently invisible to
// dedup + search. Free-tier infra. Per-item embed failures stop the current
// sweep (the model is likely down) and it retries on the next tick.
func startEmbedBackfillWatcher(ctx context.Context, store storage.Storage, embedCl *ollama.Client, log *slog.Logger) {
	tables := []storage.EmbeddableTable{
		storage.TableScratchpadItems, storage.TableTodoItems, storage.TableBugItems,
		storage.TableKnowledgeEntries, storage.TableUseCaseItems,
	}
	sweep := func() {
		embedded := 0
		for _, table := range tables {
			batch, err := store.ListUnembedded(ctx, table, 50)
			if err != nil {
				log.Warn("embed backfill: list failed", "table", table, "err", err)
				continue
			}
			for _, t := range batch {
				vec, err := embedCl.Embed(ctx, embedpkg.NormalizeItemText(t.Text))
				if err != nil {
					if embedded > 0 {
						log.Info("embed backfill: partial sweep", "embedded", embedded)
					}
					return // model likely down — retry next tick
				}
				if len(vec) == 0 {
					continue
				}
				if err := store.UpdateEmbedding(ctx, table, t.ID, embedpkg.EncodeFloat32(vec), time.Now().UTC()); err != nil {
					log.Warn("embed backfill: update failed", "table", table, "id", t.ID, "err", err)
					continue
				}
				embedded++
			}
		}
		if embedded > 0 {
			log.Info("embed backfill: re-embedded stale rows", "count", embedded)
		}
	}
	go func() {
		tick := time.NewTicker(2 * time.Minute)
		defer tick.Stop()
		sweep() // initial pass shortly after boot
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				sweep()
			}
		}
	}()
}

// cmdEmbedBackfill walks every embeddable table and embeds rows whose
// embedded_at is null or older than the table's freshness column. Idem-
// potent — safe to re-run. Stops at the first per-call failure to make
// transient Ollama issues visible. Each successful embed advances the
// next-run baseline (we only re-embed the stragglers).
func cmdEmbedBackfill(dbPath, endpoint string) error {
	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	embedCl := ollama.New(ollama.WithModel(ollama.DefaultEmbedModel), ollama.WithEndpoint(endpoint))

	tables := []storage.EmbeddableTable{
		storage.TableScratchpadItems,
		storage.TableTodoItems,
		storage.TableBugItems,
		storage.TableKnowledgeEntries,
		storage.TableUseCaseItems,
	}

	totalEmbedded := 0
	for _, table := range tables {
		fmt.Printf("==> %s\n", table)
		var tableEmbedded int
		for {
			batch, err := store.ListUnembedded(ctx, table, 100)
			if err != nil {
				return fmt.Errorf("list %s: %w", table, err)
			}
			if len(batch) == 0 {
				break
			}
			for _, t := range batch {
				vec, err := embedCl.Embed(ctx, embedpkg.NormalizeItemText(t.Text))
				if err != nil {
					return fmt.Errorf("embed %s/%s: %w", table, t.ID, err)
				}
				if len(vec) == 0 {
					continue
				}
				blob := embedpkg.EncodeFloat32(vec)
				if err := store.UpdateEmbedding(ctx, table, t.ID, blob, time.Now().UTC()); err != nil {
					return fmt.Errorf("update %s/%s: %w", table, t.ID, err)
				}
				tableEmbedded++
				totalEmbedded++
				if totalEmbedded%10 == 0 {
					fmt.Printf("    %d embedded...\n", totalEmbedded)
				}
			}
		}
		fmt.Printf("    %s: embedded %d row(s)\n", table, tableEmbedded)
	}
	fmt.Printf("done: %d row(s) embedded\n", totalEmbedded)
	return nil
}

// projectFileTreeLookup returns an agent.ProjectFileTreeFunc that resolves
// the requested project's repo file list. Returns an empty slice when the
// project has no repo_root configured (auto-anchor degrades silently). Any
// other error bubbles up; agent.Process logs and falls back to no auto-
// anchor for that classification.
func projectFileTreeLookup(store storage.Storage) agent.ProjectFileTreeFunc {
	return func(ctx context.Context, projectID string) ([]string, error) {
		p, err := store.GetProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		if p.RepoRoot == "" {
			return nil, nil
		}
		repo, err := codegit.Open(p.RepoRoot)
		if err != nil {
			return nil, fmt.Errorf("open repo at %s: %w", p.RepoRoot, err)
		}
		return repo.Tree()
	}
}

// projectModel returns an agent.ModelForFunc that resolves the per-project
// model.<role> setting (the model-provider arc). Empty result → the role uses
// the global default model. Reads are cheap per-call SQLite lookups; a missing
// setting or unparseable value falls back silently.
func projectModel(store storage.Storage, role string) agent.ModelForFunc {
	key := "model." + role
	return func(projectID string) string {
		if projectID == "" {
			return ""
		}
		st, err := store.GetProjectSetting(context.Background(), projectID, key)
		if err != nil {
			return ""
		}
		var v string
		_ = json.Unmarshal(st.Value, &v)
		return strings.TrimSpace(v)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// apiTokenFilePath returns <user-config>/codedistill/api-token, creating
// the directory (0700) if needed.
func apiTokenFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, "codedistill")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(d, "api-token"), nil
}

// persistAPIToken writes a (rotated) token to the config file (0600). Wired
// as the server's persist callback so Settings → Server access can rotate.
func persistAPIToken(tok string) error {
	path, err := apiTokenFilePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tok+"\n"), 0o600)
}

// loadOrCreateAPIToken reads the API write-auth token from
// $CODEDISTILL_API_TOKEN, else the config file, generating + persisting one
// on first run. Returns (token, displayPath).
func loadOrCreateAPIToken() (string, string, error) {
	if env := strings.TrimSpace(os.Getenv("CODEDISTILL_API_TOKEN")); env != "" {
		return env, "$CODEDISTILL_API_TOKEN", nil
	}
	path, err := apiTokenFilePath()
	if err != nil {
		return "", "", err
	}
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, path, nil
		}
	}
	tok, err := api.GenerateToken()
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", "", err
	}
	return tok, path, nil
}

// isLocalBind reports whether addr binds only the loopback interface.
// Anything else ("" / ":8080" / "0.0.0.0:8080" / a LAN IP) is treated as
// public and triggers the write-auth fail-closed guard.
func isLocalBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// withAuthCookie sets the first-party HttpOnly auth cookie on responses
// from the SPA handler, so the served app can make write requests without
// the token ever being readable by page JS.
//
// token is a getter for the same reason /mcp takes one: stamping a captured
// value meant that after a rotation every app-shell load re-wrote the browser's
// cookie with the REVOKED token, so the UI's own writes started failing until
// the process restarted — "Regenerate token" broke the app that offers it.
func withAuthCookie(token func() string, secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.SetAuthCookie(w, token(), secure || r.TLS != nil)
		next.ServeHTTP(w, r)
	})
}
