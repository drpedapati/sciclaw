package phi

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/hardware"
)

func TestLoadCatalog(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", cat.SchemaVersion)
	}
	if len(cat.Models) == 0 {
		t.Error("expected at least one model")
	}
	if len(cat.Profiles) == 0 {
		t.Error("expected at least one profile")
	}
}

func TestMatchProfile_AppleSilicon16GB(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "darwin", Arch: "arm64", MemoryGB: 36, GPUVendor: "apple"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("expected a matching profile for Apple Silicon 36GB")
	}
	if profile.ProfileID != "macos_apple_silicon_16_plus" {
		t.Errorf("ProfileID = %q, want %q", profile.ProfileID, "macos_apple_silicon_16_plus")
	}
}

func TestMatchProfile_LinuxNvidia12GB(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "linux", Arch: "amd64", MemoryGB: 32, GPUVendor: "nvidia", VRAMGB: 16}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("expected a matching profile for Linux NVIDIA 16GB VRAM")
	}
	if profile.ProfileID != "linux_or_windows_nvidia_12_plus" {
		t.Errorf("ProfileID = %q, want %q", profile.ProfileID, "linux_or_windows_nvidia_12_plus")
	}
}

func TestMatchProfile_CPUOnly8GB(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "linux", Arch: "amd64", MemoryGB: 8, GPUVendor: "none"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("expected a matching profile for CPU-only 8GB")
	}
	if profile.ProfileID != "cpu_only_under_16" {
		t.Errorf("ProfileID = %q, want %q", profile.ProfileID, "cpu_only_under_16")
	}
}

func TestMatchProfile_CPUOnly16Plus(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "linux", Arch: "amd64", MemoryGB: 32, GPUVendor: "none"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("expected a matching profile for CPU-only 32GB")
	}
	if profile.ProfileID != "cpu_only_16_plus" {
		t.Errorf("ProfileID = %q, want %q", profile.ProfileID, "cpu_only_16_plus")
	}
}

func TestSelectModel_Balanced(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "darwin", Arch: "arm64", MemoryGB: 36, GPUVendor: "apple"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("no matching profile")
	}
	model, err := cat.SelectModel(profile, "balanced")
	if err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if model.OllamaTag != "qwen3.5:4b" {
		t.Errorf("OllamaTag = %q, want %q", model.OllamaTag, "qwen3.5:4b")
	}
}

func TestSelectModel_Speed(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "darwin", Arch: "arm64", MemoryGB: 36, GPUVendor: "apple"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("no matching profile")
	}
	model, err := cat.SelectModel(profile, "speed")
	if err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if model.OllamaTag != "qwen3.5:2b" {
		t.Errorf("OllamaTag = %q, want %q", model.OllamaTag, "qwen3.5:2b")
	}
}

func TestSelectBackend_AppleSilicon(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "darwin", Arch: "arm64", MemoryGB: 36, GPUVendor: "apple"}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("no matching profile")
	}
	backend := cat.SelectBackend(profile)
	if backend != "mlx" {
		t.Errorf("SelectBackend = %q, want %q", backend, "mlx")
	}
}

func TestSelectBackend_Linux(t *testing.T) {
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	hw := hardware.Profile{OS: "linux", Arch: "amd64", MemoryGB: 32, GPUVendor: "nvidia", VRAMGB: 16}
	profile := cat.MatchProfile(hw)
	if profile == nil {
		t.Fatal("no matching profile")
	}
	backend := cat.SelectBackend(profile)
	if backend != "ollama" {
		t.Errorf("SelectBackend = %q, want %q", backend, "ollama")
	}
}
