#!/usr/bin/env bash
# =============================================================================
#  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
#
#  CodeDistill
#
#  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
#  Public License v3.0 (see the LICENSE file) and, separately, a commercial
#  license available from Nyx Software, Inc. Use outside the terms of one of those
#  licenses is prohibited.
#
#  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
# =============================================================================

# CodeDistill — first-run setup for Linux + macOS.
#
# What it does:
#   1. Detects the OS (Linux / macOS).
#   2. Finds or installs Ollama (https://ollama.com).
#   3. Verifies Ollama is reachable on http://localhost:11434.
#   4. Pulls the two models CodeDistill needs:
#        qwen2.5:7b        (~4.7 GB) — classifier + Q&A generator
#        nomic-embed-text  (~270 MB) — embeddings (search, dedup, anchors)
#   5. Initializes the local CodeDistill database (codedistill.db).
#
# Idempotent. Safe to re-run if a step failed partway through.
#
# Usage:
#   ./install.sh                # default — interactive prompts on installs
#   ./install.sh --yes          # non-interactive; assume yes on Ollama install
#   ./install.sh --skip-ollama  # assume Ollama already set up; just pull models + init
#   ./install.sh --skip-models  # only set up Ollama + init; pull models later
#   ./install.sh -h | --help
#
# Run from the directory containing the codedistill binary (the zip
# unpacks the script alongside the binary), or pass --bin <path>.

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────
# Colors (only when stdout is a TTY).
# ──────────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_DIM=$'\033[2m'
  C_RED=$'\033[31m'
  C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'
  C_CYAN=$'\033[36m'
else
  C_RESET=''; C_BOLD=''; C_DIM=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_CYAN=''
fi

step()  { printf '%s==>%s %s\n' "$C_CYAN$C_BOLD" "$C_RESET" "$*"; }
info()  { printf '    %s\n' "$*"; }
warn()  { printf '%s    warn:%s %s\n' "$C_YELLOW" "$C_RESET" "$*"; }
ok()    { printf '%s    ok:%s   %s\n' "$C_GREEN" "$C_RESET" "$*"; }
die()   { printf '%s\nerror: %s%s\n' "$C_RED$C_BOLD" "$*" "$C_RESET" >&2; exit 1; }

trap 's=$?; [[ $s -ne 0 ]] && printf "%s\n%sfailed at line %s (exit %s)%s\n" "" "$C_RED$C_BOLD" "$LINENO" "$s" "$C_RESET" >&2' ERR

# ──────────────────────────────────────────────────────────────────────
# Flags.
# ──────────────────────────────────────────────────────────────────────
ASSUME_YES=0
SKIP_OLLAMA=0
SKIP_MODELS=0
BIN_PATH=""
DB_PATH="codedistill.db"

usage() {
  sed -n '2,21p' "$0" | sed 's/^#\s\?//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes|-y)         ASSUME_YES=1 ;;
    --skip-ollama)    SKIP_OLLAMA=1 ;;
    --skip-models)    SKIP_MODELS=1 ;;
    --bin)            shift; BIN_PATH="$1" ;;
    --db)             shift; DB_PATH="$1" ;;
    -h|--help)        usage; exit 0 ;;
    *)                die "unknown argument: $1 (try --help)" ;;
  esac
  shift
done

# ──────────────────────────────────────────────────────────────────────
# 1. OS detection.
# ──────────────────────────────────────────────────────────────────────
step "detecting OS"
case "$(uname -s)" in
  Linux*)  OS=linux ;;
  Darwin*) OS=macos ;;
  *)       die "unsupported OS: $(uname -s) — only Linux and macOS are supported by this script" ;;
esac
info "$(uname -srm)  → $OS"

# ──────────────────────────────────────────────────────────────────────
# 2. Locate the codedistill binary.
# ──────────────────────────────────────────────────────────────────────
step "locating codedistill binary"
if [[ -z "$BIN_PATH" ]]; then
  # Search the typical locations: flat Linux distribution, macOS .app
  # bundle layout (zip layout has CodeDistill.app/ next to install.sh),
  # already-installed-on-PATH.
  candidates=(
    "./codedistill"
    "./codedistill-bin"
    "./CodeDistill.app/Contents/MacOS/codedistill-bin"
    "./CodeDistill.app/Contents/MacOS/CodeDistill"
  )
  for c in "${candidates[@]}"; do
    if [[ -x "$c" ]]; then
      BIN_PATH="$c"
      break
    fi
  done
  if [[ -z "$BIN_PATH" ]] && command -v codedistill >/dev/null 2>&1; then
    BIN_PATH="$(command -v codedistill)"
  fi
  if [[ -z "$BIN_PATH" ]]; then
    die "couldn't find the codedistill binary (looked for ./codedistill, ./codedistill-bin, ./CodeDistill.app/..., and \$PATH). Pass --bin <path>."
  fi
fi
info "binary: $BIN_PATH"
"$BIN_PATH" version >/dev/null 2>&1 || die "binary at $BIN_PATH didn't respond to 'version' — wrong file or wrong arch?"
ok "$("$BIN_PATH" version)"

# ──────────────────────────────────────────────────────────────────────
# 3. Ollama — find or install.
# ──────────────────────────────────────────────────────────────────────
OLLAMA_BIN=""
find_ollama() {
  # PATH first.
  if command -v ollama >/dev/null 2>&1; then
    OLLAMA_BIN="$(command -v ollama)"
    return 0
  fi
  # Common install locations — same list as CodeDistill's supervisor.
  local candidates=(
    "/usr/local/bin/ollama"
    "/opt/homebrew/bin/ollama"
    "$HOME/.local/ollama/bin/ollama"
    "$HOME/.ollama/bin/ollama"
    "/Applications/Ollama.app/Contents/Resources/ollama"
    "/Applications/Ollama.app/Contents/MacOS/ollama"
  )
  for c in "${candidates[@]}"; do
    if [[ -x "$c" ]]; then
      OLLAMA_BIN="$c"
      return 0
    fi
  done
  return 1
}

if [[ $SKIP_OLLAMA -eq 1 ]]; then
  step "skipping Ollama setup (--skip-ollama)"
else
  step "looking for Ollama"
  if find_ollama; then
    info "found at: $OLLAMA_BIN"
  else
    info "not found"
    if [[ $ASSUME_YES -eq 0 ]]; then
      printf '    install Ollama now via the official script? [Y/n] '
      read -r ans
      ans="${ans:-y}"
      case "$ans" in
        y|Y|yes|YES) : ;;
        *) die "Ollama is required. Install manually from https://ollama.com/download then re-run." ;;
      esac
    fi
    info "running: curl -fsSL https://ollama.com/install.sh | sh"
    curl -fsSL https://ollama.com/install.sh | sh
    if ! find_ollama; then
      die "Ollama install script ran but the binary still wasn't found. Check https://ollama.com/download for manual steps."
    fi
    ok "installed: $OLLAMA_BIN"
  fi
fi

# ──────────────────────────────────────────────────────────────────────
# 4. Ollama running?
# ──────────────────────────────────────────────────────────────────────
step "checking Ollama server"
if curl -fsS --max-time 2 http://localhost:11434/api/tags >/dev/null 2>&1; then
  ok "running at http://localhost:11434"
else
  if [[ $SKIP_OLLAMA -eq 1 || -z "$OLLAMA_BIN" ]]; then
    die "Ollama isn't running on :11434 and --skip-ollama prevents starting it. Run 'ollama serve' in another terminal, then re-run this script."
  fi
  info "not running — launching in background"
  # Start Ollama as a backgrounded process. Output goes to a log so the
  # user can inspect if startup hangs.
  LOG_DIR="${TMPDIR:-/tmp}/codedistill"
  mkdir -p "$LOG_DIR"
  OLLAMA_LOG="$LOG_DIR/ollama.log"
  ( nohup "$OLLAMA_BIN" serve >"$OLLAMA_LOG" 2>&1 & )
  # Poll until the API answers (or 30s timeout).
  for _ in $(seq 1 30); do
    sleep 1
    if curl -fsS --max-time 1 http://localhost:11434/api/tags >/dev/null 2>&1; then
      ok "started — log at $OLLAMA_LOG"
      break
    fi
  done
  if ! curl -fsS --max-time 1 http://localhost:11434/api/tags >/dev/null 2>&1; then
    die "Ollama didn't start within 30s. Check $OLLAMA_LOG."
  fi
fi

# ──────────────────────────────────────────────────────────────────────
# 5. Pull the two models.
# ──────────────────────────────────────────────────────────────────────
pull_model() {
  local model="$1" size_hint="$2"
  step "pulling model: $model  ($size_hint)"
  info "this can take a few minutes on the first run; progress streams below"
  local pull_rc=0
  if [[ -n "$OLLAMA_BIN" ]]; then
    "$OLLAMA_BIN" pull "$model" || pull_rc=$?
  else
    # Fall back to API streaming. We need to know whether the stream ended
    # with a "success" status; bash pipefail catches HTTP errors and we
    # post-check the body for the success marker. Without that, a 404
    # from the registry (typo, model gone) would silently no-op.
    local pull_log
    pull_log="$(mktemp)"
    if ! curl -fsS -X POST http://localhost:11434/api/pull \
        -H 'Content-Type: application/json' \
        -d "{\"model\":\"$model\"}" > "$pull_log"; then
      pull_rc=$?
    fi
    grep -E '"status":"(pulling|verifying|success)' "$pull_log" || true
    if ! grep -q '"status":"success"' "$pull_log"; then
      pull_rc=1
    fi
    rm -f "$pull_log"
  fi
  if [[ $pull_rc -ne 0 ]]; then
    die "pull failed for $model (rc=$pull_rc). Check your network + 'ollama list', then re-run this script or run 'ollama pull $model' manually."
  fi
  # Verify the model actually landed — pull commands have been known to
  # report success and still leave nothing on disk under odd network
  # conditions. Tag-prefix match handles ':latest' vs explicit tag.
  if [[ -n "$OLLAMA_BIN" ]]; then
    if ! "$OLLAMA_BIN" list 2>/dev/null | awk 'NR>1 {print $1}' | grep -Fxq "$model" \
       && ! "$OLLAMA_BIN" list 2>/dev/null | awk 'NR>1 {print $1}' | grep -Eq "^${model}:"; then
      die "$model not visible in 'ollama list' after pull. Try: 'ollama pull $model' manually."
    fi
  fi
  ok "$model ready"
}

if [[ $SKIP_MODELS -eq 1 ]]; then
  step "skipping model pulls (--skip-models)"
else
  pull_model "qwen2.5:7b"        "~4.7 GB"
  pull_model "nomic-embed-text"  "~270 MB"
fi

# ──────────────────────────────────────────────────────────────────────
# 6. Initialize the local DB.
# ──────────────────────────────────────────────────────────────────────
step "initializing database at $DB_PATH"
"$BIN_PATH" -db "$DB_PATH" init
ok "database ready"

# ──────────────────────────────────────────────────────────────────────
# Done.
# ──────────────────────────────────────────────────────────────────────
printf '\n%s%sdone — start CodeDistill:%s\n' "$C_GREEN" "$C_BOLD" "$C_RESET"
printf '  %s%s -db %s serve%s\n\n' "$C_BOLD" "$BIN_PATH" "$DB_PATH" "$C_RESET"
printf '%sthen open%s http://localhost:8080\n\n' "$C_DIM" "$C_RESET"
printf '%snext steps (optional):%s\n' "$C_BOLD" "$C_RESET"
printf '  - configure a project repo_root via the Project tab in the drawer\n'
printf '  - run %scodedistill index-code%s once to build the code-chunk index\n' "$C_DIM" "$C_RESET"
printf '\n'
