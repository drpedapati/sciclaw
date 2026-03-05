package hardware

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func detectGPU(p *Profile) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		p.GPUVendor = "apple"
		p.VRAMGB = 0 // unified memory — use MemoryGB for profile matching
		return
	}

	// Try nvidia-smi
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=memory.total", "--format=csv,noheader,nounits").Output()
	if err == nil {
		line := strings.TrimSpace(string(out))
		// Take first line if multiple GPUs
		if idx := strings.Index(line, "\n"); idx > 0 {
			line = line[:idx]
		}
		if mb, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			p.GPUVendor = "nvidia"
			p.VRAMGB = mb / 1024
			return
		}
	}

	p.GPUVendor = "none"
	p.VRAMGB = 0
}
