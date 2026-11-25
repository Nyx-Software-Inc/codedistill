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
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// GPUs publish their usage in different ways, and some not at all to an
// ordinary user. Each way is tried in turn; when none applies, the reason is
// said rather than a 0% shown.
//
//   - NVIDIA, with its own driver: nvidia-smi (Linux and Windows).
//   - AMD on Linux: the amdgpu driver's files under /sys/class/drm.
//   - Apple Silicon: ioreg's "Device Utilization %".
//   - NVIDIA on the open-source nouveau driver, and Intel: no figures for an
//     ordinary user (Intel's need root, through intel_gpu_top).

func readGPU(ctx context.Context) (gpuReading, GPU) {
	if r, info, ok := nvidiaSMI(ctx); ok {
		return r, info
	}
	switch runtime.GOOS {
	case "linux":
		return linuxGPU("/sys/class/drm")
	case "darwin":
		if r, info, ok := appleGPU(ctx); ok {
			return r, info
		}
		return gpuReading{percent: -1}, GPU{Why: "this Mac does not publish GPU usage"}
	case "windows":
		return gpuReading{percent: -1}, GPU{Why: "GPU usage is read through nvidia-smi, which is not installed"}
	}
	return gpuReading{percent: -1}, GPU{Why: "GPU usage is not read on " + runtime.GOOS}
}

func nvidiaSMI(ctx context.Context) (gpuReading, GPU, bool) {
	bin, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return gpuReading{}, GPU{}, false
	}
	out, err := exec.CommandContext(ctx, bin, "--query-gpu=name,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return gpuReading{}, GPU{}, false
	}
	return parseNvidiaSMI(string(out))
}

// parseNvidiaSMI reads the first GPU of nvidia-smi's CSV: name, percent,
// MiB used, MiB total.
func parseNvidiaSMI(out string) (gpuReading, GPU, bool) {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	f := strings.Split(line, ",")
	if len(f) != 4 {
		return gpuReading{}, GPU{}, false
	}
	pct, err1 := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
	used, err2 := strconv.ParseUint(strings.TrimSpace(f[2]), 10, 64)
	total, err3 := strconv.ParseUint(strings.TrimSpace(f[3]), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return gpuReading{}, GPU{}, false
	}
	return gpuReading{percent: pct, memUsed: used << 20},
		GPU{Readable: true, Name: strings.TrimSpace(f[0]), MemTotal: total << 20}, true
}

// linuxGPU reads an AMD GPU's figures, or says why none can be read.
func linuxGPU(root string) (gpuReading, GPU) {
	cards, _ := filepath.Glob(filepath.Join(root, "card[0-9]*"))
	var why []string
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") {
			continue // a connector (card1-HDMI-A-1), not a card
		}
		dev := filepath.Join(c, "device")
		vendor := readTrim(filepath.Join(dev, "vendor"))
		driver := ""
		if l, err := os.Readlink(filepath.Join(dev, "driver")); err == nil {
			driver = filepath.Base(l)
		}
		switch vendor {
		case "0x1002": // AMD
			if pct, ok := readUint(filepath.Join(dev, "gpu_busy_percent")); ok {
				used, _ := readUint(filepath.Join(dev, "mem_info_vram_used"))
				total, _ := readUint(filepath.Join(dev, "mem_info_vram_total"))
				return gpuReading{percent: float64(pct), memUsed: used}, GPU{Readable: true, Name: "AMD GPU", MemTotal: total}
			}
			why = append(why, "AMD, whose "+orUnknown(driver)+" driver publishes no usage")
		case "0x10de": // NVIDIA
			if driver == "nouveau" {
				why = append(why, "NVIDIA on the nouveau driver, which publishes no usage (NVIDIA's own driver does)")
			} else {
				why = append(why, "NVIDIA, but nvidia-smi is not installed")
			}
		case "0x8086": // Intel
			why = append(why, "Intel, which only root can read")
		}
	}
	if len(why) == 0 {
		return gpuReading{percent: -1}, GPU{Why: "no GPU found"}
	}
	return gpuReading{percent: -1}, GPU{Why: strings.Join(why, "; ")}
}

var ioregUtil = regexp.MustCompile(`"Device Utilization %"\s*=\s*(\d+)`)
var ioregModel = regexp.MustCompile(`"model"\s*=\s*"([^"]+)"`)

func appleGPU(ctx context.Context) (gpuReading, GPU, bool) {
	out, err := exec.CommandContext(ctx, "ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator").Output()
	if err != nil {
		return gpuReading{}, GPU{}, false
	}
	return parseIoreg(string(out))
}

func parseIoreg(out string) (gpuReading, GPU, bool) {
	m := ioregUtil.FindStringSubmatch(out)
	if m == nil {
		return gpuReading{}, GPU{}, false
	}
	pct, _ := strconv.ParseFloat(m[1], 64)
	name := "Apple GPU"
	if n := ioregModel.FindStringSubmatch(out); n != nil {
		name = n[1]
	}
	return gpuReading{percent: pct}, GPU{Readable: true, Name: name}, true
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readUint(p string) (uint64, bool) {
	v, err := strconv.ParseUint(readTrim(p), 10, 64)
	return v, err == nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
