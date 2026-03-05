package hardware

import (
	"runtime"
	"testing"
)

func TestDetect_BasicFields(t *testing.T) {
	p := Detect()
	if p.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", p.OS, runtime.GOOS)
	}
	if p.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", p.Arch, runtime.GOARCH)
	}
	if p.MemoryGB <= 0 {
		t.Errorf("MemoryGB = %d, want > 0", p.MemoryGB)
	}
	if p.GPUVendor == "" {
		t.Error("GPUVendor should not be empty")
	}
}

func TestDetect_AppleSilicon(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Apple Silicon test only runs on darwin/arm64")
	}
	p := Detect()
	if p.GPUVendor != "apple" {
		t.Errorf("GPUVendor = %q, want %q", p.GPUVendor, "apple")
	}
	if p.VRAMGB != 0 {
		t.Errorf("VRAMGB = %d, want 0 (unified memory)", p.VRAMGB)
	}
}
