package hardware

import "runtime"

// Profile describes the hardware capabilities of the local machine.
type Profile struct {
	OS        string // runtime.GOOS: "darwin", "linux", "windows"
	Arch      string // runtime.GOARCH: "arm64", "amd64"
	MemoryGB  int    // Total system RAM in GB
	GPUVendor string // "nvidia", "apple", "amd", "none"
	VRAMGB    int    // Discrete GPU VRAM in GB (0 if integrated/none)
}

// Detect returns a Profile describing the current machine.
func Detect() Profile {
	p := Profile{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	p.MemoryGB = detectMemoryGB()
	detectGPU(&p)
	return p
}
