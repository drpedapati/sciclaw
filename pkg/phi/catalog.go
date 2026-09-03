package phi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/hardware"
)

//go:embed phi_model_catalog.json
var catalogData []byte

// Catalog represents the full model catalog.
type Catalog struct {
	SchemaVersion    int                        `json:"schema_version"`
	Name             string                     `json:"name"`
	SelectionPolicy  SelectionPolicy            `json:"selection_policy"`
	CommandTemplates map[string]CommandTemplate  `json:"command_templates"`
	Models           map[string]ModelSpec        `json:"models"`
	Profiles         []HardwareProfileSpec       `json:"profiles"`
}

type SelectionPolicy struct {
	DefaultPreset  string   `json:"default_preset"`
	PreferredOrder []string `json:"preferred_order"`
}

type CommandTemplate struct {
	InstallCheck string `json:"install_check"`
	HealthCheck  string `json:"health_check,omitempty"`
	Pull         string `json:"pull"`
	Warmup       string `json:"warmup"`
}

type ModelSpec struct {
	Family      string         `json:"family"`
	OllamaTag   string         `json:"ollama_tag"`
	HFModelID   string         `json:"hf_model_id"`
	MinMemoryGB map[string]int `json:"min_memory_gb"`
}

type ProfileMatch struct {
	OS          string   `json:"os,omitempty"`
	OsAnyOf     []string `json:"os_any_of,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	MinMemoryGB int      `json:"min_memory_gb,omitempty"`
	MaxMemoryGB int      `json:"max_memory_gb,omitempty"`
	GPUVendor   string   `json:"gpu_vendor,omitempty"`
	MinVRAMGB   int      `json:"min_vram_gb,omitempty"`
	MaxVRAMGB   int      `json:"max_vram_gb,omitempty"`
	CPUOnly     bool     `json:"cpu_only,omitempty"`
}

type HardwareProfileSpec struct {
	ProfileID    string            `json:"profile_id"`
	Match        ProfileMatch      `json:"match"`
	BackendOrder []string          `json:"backend_order"`
	Defaults     map[string]string `json:"defaults"`
	AllowModels  []string          `json:"allow_models"`
}

// LoadCatalog parses the embedded catalog JSON.
func LoadCatalog() (*Catalog, error) {
	var cat Catalog
	if err := json.Unmarshal(catalogData, &cat); err != nil {
		return nil, fmt.Errorf("parsing model catalog: %w", err)
	}
	return &cat, nil
}

// MatchProfile finds the first hardware profile that matches the given hardware.
// Returns nil if no profile matches.
func (c *Catalog) MatchProfile(hw hardware.Profile) *HardwareProfileSpec {
	for i := range c.Profiles {
		if profileMatches(&c.Profiles[i].Match, hw) {
			return &c.Profiles[i]
		}
	}
	return nil
}

func profileMatches(m *ProfileMatch, hw hardware.Profile) bool {
	// OS check
	if m.OS != "" && !strings.EqualFold(hw.OS, m.OS) {
		return false
	}
	if len(m.OsAnyOf) > 0 {
		found := false
		for _, os := range m.OsAnyOf {
			if strings.EqualFold(hw.OS, os) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Arch check
	if m.Arch != "" && !strings.EqualFold(hw.Arch, m.Arch) {
		return false
	}

	// CPU-only profiles reject any machine with a GPU (Apple Silicon
	// is handled by the apple_silicon profile, not cpu_only).
	if m.CPUOnly && hw.GPUVendor != "none" && hw.GPUVendor != "" {
		return false
	}

	// GPU vendor check
	if m.GPUVendor != "" && !strings.EqualFold(hw.GPUVendor, m.GPUVendor) {
		return false
	}

	// Memory checks (use system memory for general, VRAM for GPU-specific)
	mem := hw.MemoryGB
	if m.MinMemoryGB > 0 && mem < m.MinMemoryGB {
		return false
	}
	if m.MaxMemoryGB > 0 && mem > m.MaxMemoryGB {
		return false
	}

	// VRAM checks
	if m.MinVRAMGB > 0 && hw.VRAMGB < m.MinVRAMGB {
		return false
	}
	if m.MaxVRAMGB > 0 && hw.VRAMGB > m.MaxVRAMGB {
		return false
	}

	return true
}

// SelectModel picks a model for the given profile and preset.
func (c *Catalog) SelectModel(profile *HardwareProfileSpec, preset string) (*ModelSpec, error) {
	if preset == "" {
		preset = c.SelectionPolicy.DefaultPreset
	}
	if preset == "" {
		preset = "balanced"
	}

	modelID, ok := profile.Defaults[preset]
	if !ok {
		return nil, fmt.Errorf("preset %q not found in profile %s", preset, profile.ProfileID)
	}

	spec, ok := c.Models[modelID]
	if !ok {
		return nil, fmt.Errorf("model %q not found in catalog", modelID)
	}

	return &spec, nil
}

// SelectBackend returns the preferred backend for the given profile.
// The catalog's backend_order determines priority; ollama is listed first
// for all profiles until MLX support is implemented.
func (c *Catalog) SelectBackend(profile *HardwareProfileSpec) string {
	if len(profile.BackendOrder) > 0 {
		return profile.BackendOrder[0]
	}
	return config.BackendOllama
}
