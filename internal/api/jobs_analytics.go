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
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/jobqueue"
	"codedistill/internal/machine"
)

// The Jobs pane's analytics strip (docs/design/jobs-analytics.md): how busy the
// job slots have been, what the agents have used, and how busy the machine is.
// Installation-wide: the slots and the machine are shared by every project.

// analyticsWindow is a window the strip offers, and how finely it is drawn.
type analyticsWindow struct {
	span   time.Duration
	slices int
}

var analyticsWindows = map[string]analyticsWindow{
	"1h":  {time.Hour, 60},          // one-minute slices
	"24h": {24 * time.Hour, 96},     // fifteen-minute slices
	"7d":  {7 * 24 * time.Hour, 84}, // two-hour slices
}

// tokenCounts is tokens by kind. The total is their sum: input never includes
// the cache, on every provider.
type tokenCounts struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Total      int64 `json:"total"`
}

func (t *tokenCounts) add(r *domain.AgentRun) {
	v := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	t.In += v(r.TokensIn)
	t.Out += v(r.TokensOut)
	t.CacheRead += v(r.CacheRead)
	t.CacheWrite += v(r.CacheWrite)
	t.Total = t.In + t.Out + t.CacheRead + t.CacheWrite
}

type tokensByJob struct {
	JobID   string `json:"job_id"`
	Label   string `json:"label"`
	Project string `json:"project"`
	Type    string `json:"type"`
	tokenCounts
}

type tokensByRole struct {
	Role string `json:"role"`
	Lens string `json:"lens,omitempty"`
	tokenCounts
}

type tokensByModel struct {
	Model string `json:"model"`
	tokenCounts
}

// jobsAnalytics is GET /api/v1/jobs/analytics?window=1h|24h|7d.
func (s *Server) jobsAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.URL.Query().Get("window")
	win, ok := analyticsWindows[name]
	if !ok {
		name, win = "24h", analyticsWindows["24h"]
	}
	now := time.Now().UTC()
	from := now.Add(-win.span)

	jobs, err := s.store.ListJobsSince(ctx, from)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	running, queued := 0, 0
	byID := map[string]*domain.Job{}
	for _, j := range jobs {
		byID[j.ID] = j
		switch j.Status {
		case domain.JobRunning:
			running++
		case domain.JobQueued:
			queued++
		}
	}

	// Tokens: runs that ended inside the window (a run counts its tokens when
	// it ends), and the ones still going on, at what they have used so far.
	runs, err := s.store.ListAgentRunsSince(ctx, from.Add(-24*time.Hour))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var total tokenCounts
	jobTok := map[string]*tokensByJob{}
	roleTok := map[string]*tokensByRole{}
	modelTok := map[string]*tokensByModel{}
	liveN := 0
	count := func(run *domain.AgentRun) {
		total.add(run)
		jt := jobTok[run.JobID]
		if jt == nil {
			jt = &tokensByJob{JobID: run.JobID}
			jobTok[run.JobID] = jt
		}
		jt.add(run)
		rk := run.Role + "/" + run.Lens
		rt := roleTok[rk]
		if rt == nil {
			rt = &tokensByRole{Role: run.Role, Lens: run.Lens}
			roleTok[rk] = rt
		}
		rt.add(run)
		model := run.Model
		if model == "" {
			model = run.Agent
		}
		if model == "" {
			model = "unknown"
		}
		mt := modelTok[model]
		if mt == nil {
			mt = &tokensByModel{Model: model}
			modelTok[model] = mt
		}
		mt.add(run)
	}
	for _, run := range runs {
		if run.StartedAt.Add(time.Duration(run.DurationMS) * time.Millisecond).Before(from) {
			continue
		}
		count(run)
	}
	for _, run := range s.liveAgentRuns() {
		liveN++
		count(run)
	}

	projects := map[string]string{}
	if ps, err := s.store.ListProjects(ctx); err == nil {
		for _, p := range ps {
			projects[p.ID] = p.Name
		}
	}
	jobsOut := make([]tokensByJob, 0, len(jobTok))
	for id, jt := range jobTok {
		j := byID[id]
		if j == nil {
			j, _ = s.store.GetJob(ctx, id)
		}
		if j != nil {
			jt.Type, jt.Project = j.Type, projects[j.ProjectID]
			jt.Label = j.ScopeLabel
			if jt.Label == "" {
				jt.Label = j.Type
			}
		}
		jobsOut = append(jobsOut, *jt)
	}
	sort.Slice(jobsOut, func(a, b int) bool { return jobsOut[a].Total > jobsOut[b].Total })
	rolesOut := make([]tokensByRole, 0, len(roleTok))
	for _, rt := range roleTok {
		rolesOut = append(rolesOut, *rt)
	}
	sort.Slice(rolesOut, func(a, b int) bool { return rolesOut[a].Total > rolesOut[b].Total })
	modelsOut := make([]tokensByModel, 0, len(modelTok))
	for _, mt := range modelTok {
		modelsOut = append(modelsOut, *mt)
	}
	sort.Slice(modelsOut, func(a, b int) bool { return modelsOut[a].Total > modelsOut[b].Total })

	writeJSON(w, http.StatusOK, map[string]any{
		"window": name, "from": from, "to": now,
		"slots": s.jobSlots(ctx), "running": running, "queued": queued,
		"timeline": jobqueue.Timeline(jobs, from, now, win.slices),
		"tokens": map[string]any{
			"total": total, "live_runs": liveN,
			"by_job": jobsOut, "by_role": rolesOut, "by_model": modelsOut,
		},
	})
}

// ── The machine ─────────────────────────────────────────────────────────────

// sampler is the machine sampler, started on first use.
func (s *Server) sampler() *machine.Sampler {
	s.machineOnce.Do(func() {
		s.machine = machine.New()
		s.machine.Start(context.Background())
	})
	return s.machine
}

// loadedModel is a model a local model server has in memory, and how much of
// it sits in GPU memory: the reason a local model is slow is usually that it
// runs on the CPU, which this says plainly.
type loadedModel struct {
	Endpoint string `json:"endpoint"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SizeVRAM int64  `json:"size_vram"`
	// On is "gpu", "cpu" or "split".
	On string `json:"on"`
}

// machineView is GET /api/v1/machine: the last hour of samples (taken only
// while someone looks), the GPU, and the models loaded.
func (s *Server) machineView(w http.ResponseWriter, r *http.Request) {
	sm := s.sampler()
	sm.Watch()
	samples, gpu := sm.Recent()
	if samples == nil {
		samples = []machine.Sample{}
	}
	models, modelsErr := s.loadedModels(r.Context())
	out := map[string]any{
		"samples": samples, "gpu": gpu, "every_seconds": int(machine.Every / time.Second),
		"models": models,
	}
	if modelsErr != "" {
		out["models_error"] = modelsErr
	}
	writeJSON(w, http.StatusOK, out)
}

// loadedModels asks each local model server (Ollama: the app's own, and any
// added under Settings › Models) what it has loaded. Bounded: a server that is
// off must not hold up the strip.
func (s *Server) loadedModels(ctx context.Context) ([]loadedModel, string) {
	endpoints := map[string]bool{}
	if s.setup != nil && s.setup.Endpoint != "" {
		endpoints[strings.TrimRight(s.setup.Endpoint, "/")] = true
	}
	if ps, err := s.store.ListModelProviders(ctx); err == nil {
		for _, p := range ps {
			if p.Protocol == "ollama" && p.Enabled && p.Endpoint != "" {
				endpoints[strings.TrimRight(p.Endpoint, "/")] = true
			}
		}
	}
	out := []loadedModel{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failed []string
	client := &http.Client{Timeout: 2 * time.Second}
	for ep := range endpoints {
		wg.Add(1)
		go func(ep string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep+"/api/ps", nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				failed = append(failed, ep)
				mu.Unlock()
				return
			}
			defer resp.Body.Close()
			var ps struct {
				Models []struct {
					Name     string `json:"name"`
					Size     int64  `json:"size"`
					SizeVRAM int64  `json:"size_vram"`
				} `json:"models"`
			}
			if json.NewDecoder(resp.Body).Decode(&ps) != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range ps.Models {
				on := "split"
				switch {
				case m.SizeVRAM <= 0:
					on = "cpu"
				case m.SizeVRAM >= m.Size:
					on = "gpu"
				}
				out = append(out, loadedModel{Endpoint: ep, Name: m.Name, Size: m.Size, SizeVRAM: m.SizeVRAM, On: on})
			}
		}(ep)
	}
	wg.Wait()
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	if len(failed) > 0 && len(out) == 0 {
		sort.Strings(failed)
		return out, "the model server at " + strings.Join(failed, ", ") + " did not answer"
	}
	return out, ""
}
