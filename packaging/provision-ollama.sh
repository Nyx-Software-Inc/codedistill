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

# Idempotent Ollama provisioning for the codedistill-server package, run from
# the RPM %post / deb postinst after provision-postgres.sh. Hands-off like the
# Postgres step: install Ollama, size the model to the machine, pull it — so a
# fresh server can classify without anyone SSHing in.
#
# Model choice: the app default qwen2.5:7b (~4.7GB), or the next size up in the
# same family, qwen2.5:14b (~9GB), when the machine has >=16GB RAM. The pick is
# written to /etc/codedistill/codedistill.env as CODEDISTILL_MODEL — ONLY if not
# already set, so an operator's choice is never clobbered. Edit that line and
# `ollama pull <model>` to change it later.
#
# Air-gap friendly: every network-touching step degrades to a logged skip with
# manual instructions instead of failing the package install. Safe to re-run.
set -uo pipefail

ENV_FILE=/etc/codedistill/codedistill.env
OLLAMA_API=http://127.0.0.1:11434
EMBED_MODEL=nomic-embed-text

log() { echo "codedistill-server: $*"; }

# 1. Pick the model from installed RAM. MemTotal on a nominal 16GB box reads
#    ~15.5GB (kernel/firmware reservations), so the 14b bar sits at 15GB.
MEM_KB="$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
if [ "$MEM_KB" -ge 15728640 ]; then # 15 GiB in kB
  MODEL=qwen2.5:14b
else
  MODEL=qwen2.5:7b
fi
log "detected $((MEM_KB / 1024 / 1024))GB RAM -> model $MODEL"

# 2. Record the pick in the env file the service reads — only if absent.
if [ -f "$ENV_FILE" ] && grep -q '^CODEDISTILL_MODEL=' "$ENV_FILE"; then
  MODEL="$(sed -n 's/^CODEDISTILL_MODEL=//p' "$ENV_FILE" | tail -1)"
  log "$ENV_FILE already sets CODEDISTILL_MODEL=$MODEL — leaving it"
else
  install -d -m 0755 /etc/codedistill
  {
    echo ""
    echo "# Classification model, sized to this machine's RAM at install time"
    echo "# (<16GB -> qwen2.5:7b, >=16GB -> qwen2.5:14b). To change: edit this"
    echo "# line and run: ollama pull <model>"
    echo "CODEDISTILL_MODEL=$MODEL"
  } >> "$ENV_FILE"
  log "wrote CODEDISTILL_MODEL=$MODEL to $ENV_FILE"
fi

# 3. Install Ollama if it isn't already here (official installer: sets up the
#    ollama system user + systemd service). Offline -> skip with instructions.
if ! command -v ollama >/dev/null 2>&1 && [ ! -x /usr/local/bin/ollama ]; then
  if curl -fsS --max-time 10 -o /dev/null https://ollama.com 2>/dev/null; then
    log "installing Ollama (ollama.com/install.sh)"
    if ! curl -fsSL https://ollama.com/install.sh | sh; then
      log "WARNING: Ollama install failed — install it manually (https://ollama.com), then re-run $0"
      exit 0
    fi
  else
    log "WARNING: no network — skipping Ollama install (air-gapped?)."
    log "Install Ollama manually, then: ollama pull $MODEL && ollama pull $EMBED_MODEL"
    exit 0
  fi
fi

# 4. Guarantee a systemd unit. The official installer creates one in most
#    environments but not all (observed: binary installed, no unit) — so we
#    ensure it ourselves rather than depend on installer internals.
OLLAMA_BIN="$(command -v ollama || echo /usr/local/bin/ollama)"
if [ ! -f /etc/systemd/system/ollama.service ] && [ ! -f /usr/lib/systemd/system/ollama.service ]; then
  if ! id ollama >/dev/null 2>&1; then
    useradd -r -s /usr/sbin/nologin -U -m -d /usr/share/ollama ollama 2>/dev/null \
      || useradd -r -s /sbin/nologin -U -m -d /usr/share/ollama ollama
  fi
  cat > /etc/systemd/system/ollama.service <<UNIT
[Unit]
Description=Ollama Service
After=network-online.target

[Service]
ExecStart=$OLLAMA_BIN serve
User=ollama
Group=ollama
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  log "wrote /etc/systemd/system/ollama.service (installer didn't)"
fi

# 5. The service account's home must exist and be writable — ollama serve
#    writes ~/.ollama (keys, models) on startup and exits if it can't. A
#    pre-existing user without a home (observed: created by the official
#    installer sans home dir) crash-loops the service, so ensure it every run.
if id ollama >/dev/null 2>&1; then
  OLLAMA_HOME="$(getent passwd ollama | cut -d: -f6)"
  if [ -n "$OLLAMA_HOME" ] && [ "$OLLAMA_HOME" != "/" ]; then
    install -d -o ollama -g ollama "$OLLAMA_HOME"
  fi
fi

# 6. Make sure the Ollama service is up, then pull the models. Pulls are the
#    slow part (the 14b model is ~9GB) but a server that can't classify until
#    someone intervenes defeats hands-off provisioning. Re-run to resume.
systemctl enable --now ollama >/dev/null 2>&1 || true
for _ in $(seq 1 30); do
  curl -fsS --max-time 2 -o /dev/null "$OLLAMA_API" 2>/dev/null && break
  sleep 1
done
if ! curl -fsS --max-time 2 -o /dev/null "$OLLAMA_API" 2>/dev/null; then
  log "WARNING: Ollama isn't answering on $OLLAMA_API — start it, then: ollama pull $MODEL && ollama pull $EMBED_MODEL"
  exit 0
fi

for m in "$MODEL" "$EMBED_MODEL"; do
  log "pulling $m (may take a while on first install)"
  if ! ollama pull "$m"; then
    log "WARNING: pull of $m failed — re-run $0 (or: ollama pull $m) to resume"
  fi
done

log "ollama provisioning complete (model $MODEL + $EMBED_MODEL)"
