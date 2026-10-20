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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"codedistill/internal/domain"
)

// First-run setup (the desktop app's replacement for scripts/install.sh).
//
// On a fresh machine the server used to exit when Ollama was missing, so no
// screen could ever explain what to install. serve now keeps running without
// Ollama, and the UI asks here what is missing: the daemon, the binary, the
// classifier model, the embedding model. The screen pulls models through
// /setup/pull (a streaming proxy to Ollama's /api/pull) and stores the chosen
// classifier model as the user setting `model.classifier`, which the server
// reads as its global default (see cmd/codedistill: globalModel).

// SetupDeps is what the setup endpoints need from serve.
type SetupDeps struct {
	// Endpoint is the Ollama base URL, e.g. http://localhost:11434.
	Endpoint string
	// DefaultModel is the classifier model from the -model flag.
	DefaultModel string
	// EmbedModel is the (fixed) embedding model.
	EmbedModel string
	// DataDir is shown to the user as where their data lives.
	DataDir string
	// Binary locates the ollama executable (nil = unknown).
	Binary func() (string, error)
	// Start tries to start Ollama (the supervisor); nil = autostart disabled.
	Start func(ctx context.Context) error
	// StartupError is what the supervisor said when serve began, if anything.
	StartupError string
}

// setupKeyCompleted and setupKeyModel are the user settings the screen writes.
const (
	setupKeyCompleted = "setup.completed"
	SetupKeyModel     = "model.classifier"
	// Recommendation thresholds, as scripts/install.sh had them.
	modelLarge     = "qwen2.5:14b"
	modelLargeMinB = 15 << 30
)

// WithSetup wires the first-run endpoints. Returns the receiver.
func (s *Server) WithSetup(d SetupDeps) *Server {
	s.setup = &d
	return s
}

// SetupStatus is GET /api/v1/setup/status.
type SetupStatus struct {
	Completed bool `json:"completed"`
	Ollama    struct {
		Endpoint  string `json:"endpoint"`
		Reachable bool   `json:"reachable"`
		Binary    string `json:"binary"`
		Error     string `json:"error,omitempty"`
		CanStart  bool   `json:"can_start"`
	} `json:"ollama"`
	Models struct {
		Classifier  string   `json:"classifier"`  // effective choice
		Default     string   `json:"default"`     // the -model flag
		Recommended string   `json:"recommended"` // by installed memory
		Embed       string   `json:"embed"`
		Installed   []string `json:"installed"`
	} `json:"models"`
	MemoryGB float64 `json:"memory_gb"`
	DataDir  string  `json:"data_dir"`
	Platform string  `json:"platform"`
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	st := s.collectSetupStatus(r.Context())
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) collectSetupStatus(ctx context.Context) SetupStatus {
	d := s.setup
	var st SetupStatus
	st.Platform = runtime.GOOS
	st.DataDir = d.DataDir
	st.Ollama.Endpoint = d.Endpoint
	st.Ollama.CanStart = d.Start != nil
	st.Models.Default = d.DefaultModel
	st.Models.Embed = d.EmbedModel
	st.Models.Installed = []string{}
	if d.Binary != nil {
		if bin, err := d.Binary(); err == nil {
			st.Ollama.Binary = bin
		}
	}
	if s.models != nil {
		if got, err := s.models(ctx); err == nil {
			st.Ollama.Reachable = true
			st.Models.Installed = got
		} else {
			st.Ollama.Error = err.Error()
		}
	}
	if !st.Ollama.Reachable && st.Ollama.Error == "" {
		st.Ollama.Error = d.StartupError
	}
	st.Models.Classifier = d.DefaultModel
	if v := s.userString(ctx, SetupKeyModel); v != "" {
		st.Models.Classifier = v
	}
	st.Completed = s.userBool(ctx, setupKeyCompleted)
	mem := totalMemory()
	st.MemoryGB = float64(mem) / (1 << 30)
	st.Models.Recommended = d.DefaultModel
	if mem >= modelLargeMinB {
		st.Models.Recommended = modelLarge
	}
	return st
}

// setupStartOllama is POST /api/v1/setup/ollama/start: try the supervisor
// again (the user has just installed Ollama) and report the new status.
func (s *Server) setupStartOllama(w http.ResponseWriter, r *http.Request) {
	if s.setup.Start == nil {
		writeMsg(w, http.StatusConflict, "ollama autostart is disabled (-ollama-autostart=false)")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	err := s.setup.Start(ctx)
	st := s.collectSetupStatus(r.Context())
	if err != nil && !st.Ollama.Reachable {
		st.Ollama.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, st)
}

// setupPull is POST /api/v1/setup/pull {"model": "..."}: proxies Ollama's
// streaming pull so the screen can show progress. Each line is Ollama's own
// NDJSON ({status, total, completed}); the last line is {"status":"success"}
// or {"error": "..."}.
func (s *Server) setupPull(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Model) == "" {
		writeMsg(w, http.StatusBadRequest, "model is required")
		return
	}
	body, _ := json.Marshal(map[string]any{"model": in.Model})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.setup.Endpoint+"/api/pull", bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req) // a pull can run for many minutes; no client timeout
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		writeMsg(w, http.StatusBadGateway, "ollama pull: "+strings.TrimSpace(string(b)))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// setupComplete is POST /api/v1/setup/complete {"model": "..."}: records the
// classifier choice and marks setup done.
func (s *Server) setupComplete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	uid := userFromContext(r.Context())
	if m := strings.TrimSpace(in.Model); m != "" {
		if err := s.saveUserSetting(r.Context(), uid, SetupKeyModel, m); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
	}
	if err := s.saveUserSetting(r.Context(), uid, setupKeyCompleted, true); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, s.collectSetupStatus(r.Context()))
}

func (s *Server) saveUserSetting(ctx context.Context, uid, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.SetUserSetting(ctx, &domain.UserSetting{UserID: uid, Key: key, Value: raw})
}

func (s *Server) userString(ctx context.Context, key string) string {
	st, err := s.store.GetUserSetting(ctx, localUserID, key)
	if err != nil || st == nil {
		return ""
	}
	var v string
	_ = json.Unmarshal(st.Value, &v)
	return strings.TrimSpace(v)
}

func (s *Server) userBool(ctx context.Context, key string) bool {
	st, err := s.store.GetUserSetting(ctx, localUserID, key)
	if err != nil || st == nil {
		return false
	}
	var v bool
	_ = json.Unmarshal(st.Value, &v)
	return v
}

// totalMemory returns installed RAM in bytes, 0 when unknown.
func totalMemory() int64 {
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					kb, _ := strconv.ParseInt(f[1], 10, 64)
					return kb * 1024
				}
			}
		}
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		return n
	}
	return 0
}
