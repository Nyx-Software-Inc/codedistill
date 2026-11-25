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

// Package machine samples how busy the machine CodeDistill runs on is: CPU,
// memory, and the GPU where it can be read (docs/design/jobs-analytics.md).
//
// It samples only while someone is looking. The Jobs pane's analytics strip
// asks for figures every few seconds; each ask keeps the sampler going for two
// minutes more. Nobody looking, nothing runs. An hour of samples is kept, in
// memory: these are glances at the machine, not records.
package machine

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// Every is how often a sample is taken while someone is looking.
const Every = 5 * time.Second

// keep is how many samples are kept: an hour's worth.
const keep = int(time.Hour / Every)

// watchedFor is how long one look keeps the sampler going.
const watchedFor = 2 * time.Minute

// Sample is the machine at one moment. A figure that could not be read is
// negative, which is not zero.
type Sample struct {
	At         time.Time `json:"at"`
	CPUPercent float64   `json:"cpu_percent"`
	MemUsed    uint64    `json:"mem_used"`
	MemTotal   uint64    `json:"mem_total"`
	GPUPercent float64   `json:"gpu_percent"`
	GPUMemUsed uint64    `json:"gpu_mem_used,omitempty"`
}

// GPU is what can be said about the machine's graphics processors.
type GPU struct {
	// Readable: usage figures are available.
	Readable bool `json:"readable"`
	// Name of the GPU read, when there is one.
	Name string `json:"name,omitempty"`
	// MemTotal is its own memory, when known.
	MemTotal uint64 `json:"mem_total,omitempty"`
	// Why, when not readable: no GPU, or which GPU and why it publishes no
	// figures (the open-source NVIDIA driver, Intel's needing root).
	Why string `json:"why,omitempty"`
}

// Sampler keeps the recent samples.
type Sampler struct {
	mu      sync.Mutex
	samples []Sample
	watched time.Time
	gpu     GPU
	readGPU func(ctx context.Context) (gpuReading, GPU)
	wake    chan struct{}
	now     func() time.Time
	started bool
}

// gpuReading is one GPU sample: utilisation percent and memory used; percent
// negative when there is nothing to read.
type gpuReading struct {
	percent float64
	memUsed uint64
}

// New returns a sampler; Watch starts it on first use.
func New() *Sampler {
	return &Sampler{readGPU: readGPU, wake: make(chan struct{}, 1), now: time.Now}
}

// Start runs the sampler until ctx ends. It samples only while watched.
func (s *Sampler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.run(ctx)
}

// Watch says someone is looking: sample for the next two minutes. The first
// look takes a sample at once, so the strip has figures to show.
func (s *Sampler) Watch() {
	s.mu.Lock()
	first := s.now().Sub(s.watched) > watchedFor
	s.watched = s.now()
	s.mu.Unlock()
	if first {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// Recent returns the samples kept, oldest first, and what is known of the
// GPU.
func (s *Sampler) Recent() ([]Sample, GPU) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...), s.gpu
}

func (s *Sampler) run(ctx context.Context) {
	// CPU percent is measured between two reads; prime the first.
	_, _ = cpu.PercentWithContext(ctx, 0, false)
	t := time.NewTicker(Every)
	defer t.Stop()
	for {
		s.mu.Lock()
		watched := s.now().Sub(s.watched) <= watchedFor
		s.mu.Unlock()
		if watched {
			s.sample(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

func (s *Sampler) sample(ctx context.Context) {
	smp := Sample{At: s.now().UTC(), CPUPercent: -1, GPUPercent: -1}
	if p, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(p) == 1 {
		smp.CPUPercent = p[0]
	}
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		smp.MemUsed, smp.MemTotal = v.Used, v.Total
	}
	gctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	g, info := s.readGPU(gctx)
	cancel()
	smp.GPUPercent, smp.GPUMemUsed = g.percent, g.memUsed
	s.add(smp, info)
}

func (s *Sampler) add(smp Sample, info GPU) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, smp)
	if len(s.samples) > keep {
		s.samples = append([]Sample(nil), s.samples[len(s.samples)-keep:]...)
	}
	s.gpu = info
}
