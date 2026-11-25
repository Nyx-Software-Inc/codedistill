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

package machine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseNvidiaSMI(t *testing.T) {
	r, g, ok := parseNvidiaSMI("NVIDIA RTX 1000 Ada Generation Laptop GPU, 37, 2048, 6144\n")
	if !ok || r.percent != 37 || r.memUsed != 2048<<20 || g.MemTotal != 6144<<20 || !g.Readable || !strings.HasPrefix(g.Name, "NVIDIA RTX") {
		t.Fatalf("%+v %+v %v", r, g, ok)
	}
	if _, _, ok := parseNvidiaSMI("No devices were found"); ok {
		t.Error("garbage parsed")
	}
}

func TestParseIoreg(t *testing.T) {
	out := `+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X>
    {
      "model" = "Apple M2 Pro"
      "PerformanceStatistics" = {"In use system memory"=812654592,"Device Utilization %"=23,"Renderer Utilization %"=21}
    }`
	r, g, ok := parseIoreg(out)
	if !ok || r.percent != 23 || g.Name != "Apple M2 Pro" {
		t.Fatalf("%+v %+v %v", r, g, ok)
	}
}

// card writes a fake /sys/class/drm card.
func card(t *testing.T, root, name, vendor, driver string, files map[string]string) {
	t.Helper()
	dev := filepath.Join(root, name, "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dev, "vendor"), []byte(vendor+"\n"), 0o644)
	drv := filepath.Join(root, "drivers", driver)
	os.MkdirAll(drv, 0o755)
	os.Symlink(drv, filepath.Join(dev, "driver"))
	for f, v := range files {
		os.WriteFile(filepath.Join(dev, f), []byte(v+"\n"), 0o644)
	}
}

func TestLinuxGPU(t *testing.T) {
	// Rich's laptop: NVIDIA on nouveau, and Intel. Nothing readable; say why.
	root := t.TempDir()
	card(t, root, "card1", "0x8086", "i915", nil)
	card(t, root, "card2", "0x10de", "nouveau", nil)
	os.MkdirAll(filepath.Join(root, "card2-eDP-1"), 0o755)
	r, g := linuxGPU(root)
	if g.Readable || r.percent >= 0 || !strings.Contains(g.Why, "nouveau") || !strings.Contains(g.Why, "Intel") {
		t.Errorf("nouveau + intel: %+v %+v", r, g)
	}
	// An AMD card publishes its figures.
	root = t.TempDir()
	card(t, root, "card0", "0x1002", "amdgpu", map[string]string{"gpu_busy_percent": "64", "mem_info_vram_used": "1073741824", "mem_info_vram_total": "8589934592"})
	r, g = linuxGPU(root)
	if !g.Readable || r.percent != 64 || r.memUsed != 1<<30 || g.MemTotal != 8<<30 {
		t.Errorf("amd: %+v %+v", r, g)
	}
	if _, g := linuxGPU(t.TempDir()); g.Why != "no GPU found" {
		t.Errorf("none: %+v", g)
	}
}

// The sampler samples only while watched, and keeps an hour.
func TestSamplerSamplesOnlyWhileWatched(t *testing.T) {
	s := New()
	reads := 0
	s.readGPU = func(context.Context) (gpuReading, GPU) {
		reads++
		return gpuReading{percent: 10}, GPU{Readable: true, Name: "fake"}
	}
	clock := time.Now()
	s.now = func() time.Time { return clock }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	if got, _ := s.Recent(); len(got) != 0 {
		t.Fatalf("sampled with nobody looking: %d", len(got))
	}
	s.Watch()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, g := s.Recent(); len(got) == 1 && g.Name == "fake" && got[0].GPUPercent == 10 && got[0].MemTotal > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no sample after a look")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < keep+5; i++ {
		s.add(Sample{}, GPU{})
	}
	if got, _ := s.Recent(); len(got) != keep {
		t.Errorf("kept %d, want %d", len(got), keep)
	}
}

// On this machine, whatever it is: a reading or a reason, never neither.
func TestReadGPUHere(t *testing.T) {
	r, g := readGPU(context.Background())
	if !g.Readable && g.Why == "" {
		t.Errorf("no reading and no reason on %s: %+v %+v", runtime.GOOS, r, g)
	}
	t.Logf("%s: readable=%v %q %s", runtime.GOOS, g.Readable, g.Name, g.Why)
}
