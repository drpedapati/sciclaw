package addons

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const validManifest = `{
  "name": "webtop",
  "version": "0.1.0",
  "description": "Per-user browser desktops",
  "author": "sciclaw",
  "homepage": "https://example.com/webtop",
  "requires": {
    "sciclaw": ">=0.3.0",
    "runtime": ["docker"],
    "platform": ["linux", "darwin"]
  },
  "sidecar": {
    "binary": "sciclaw-addon-webtop",
    "socket": "sock",
    "start_timeout_seconds": 10,
    "health_path": "/health"
  },
  "provides": {
    "ui_tab": {"name": "Desktops", "icon": "desktop", "path": "/ui"},
    "cli_group": "webtop",
    "hooks": ["routing_changed"],
    "config_schema": "schema.json"
  },
  "bootstrap": {
    "install": "./bin/install.sh",
    "uninstall": "./bin/uninstall.sh"
  },
  "compose": "compose.yaml"
}`

func TestParseManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "addon.json")
	writeFile(t, path, validManifest)

	m, err := ParseManifest(path)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Name != "webtop" {
		t.Errorf("name = %q, want webtop", m.Name)
	}
	if m.Version != "0.1.0" {
		t.Errorf("version = %q, want 0.1.0", m.Version)
	}
	if m.Requires.Sciclaw != ">=0.3.0" {
		t.Errorf("requires.sciclaw = %q", m.Requires.Sciclaw)
	}
	if m.Sidecar.Binary != "sciclaw-addon-webtop" {
		t.Errorf("sidecar.binary = %q", m.Sidecar.Binary)
	}
	if m.Sidecar.StartTimeoutSeconds != 10 {
		t.Errorf("sidecar.start_timeout_seconds = %d, want 10", m.Sidecar.StartTimeoutSeconds)
	}
	if m.Provides.UITab == nil || m.Provides.UITab.Name != "Desktops" {
		t.Errorf("ui_tab = %+v", m.Provides.UITab)
	}
	if m.Provides.CLIGroup != "webtop" {
		t.Errorf("cli_group = %q", m.Provides.CLIGroup)
	}
	if len(m.Provides.Hooks) != 1 || m.Provides.Hooks[0] != "routing_changed" {
		t.Errorf("hooks = %v", m.Provides.Hooks)
	}
	if m.Bootstrap.Install != "./bin/install.sh" {
		t.Errorf("bootstrap.install = %q", m.Bootstrap.Install)
	}
	if m.Compose != "compose.yaml" {
		t.Errorf("compose = %q", m.Compose)
	}
}

func TestParseManifest_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "addon.json")
	writeFile(t, path, `{"name": "x", "version": }`)

	if _, err := ParseManifest(path); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseManifest_FileMissing(t *testing.T) {
	_, err := ParseManifest(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "nope.json") {
		t.Errorf("error should mention the path, got: %v", err)
	}
}

func TestManifest_ValidateMissingFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"name", func(m *Manifest) { m.Name = "" }, "name"},
		{"version", func(m *Manifest) { m.Version = "" }, "version"},
		{"requires.sciclaw", func(m *Manifest) { m.Requires.Sciclaw = "" }, "requires.sciclaw"},
		{"sidecar.binary", func(m *Manifest) { m.Sidecar.Binary = "" }, "sidecar.binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &Manifest{
				Name:     "x",
				Version:  "0.1.0",
				Requires: Requirements{Sciclaw: ">=0.1.0"},
				Sidecar:  SidecarSpec{Binary: "x-sidecar"},
			}
			c.mutate(m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error for missing %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestValidateRequirements_VersionTooOld(t *testing.T) {
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{Sciclaw: ">=0.3.0"},
		Sidecar:  SidecarSpec{Binary: "x"},
	}
	err := ValidateRequirements(m, "0.2.0", "linux", okLookPath)
	if err == nil || !strings.Contains(err.Error(), "requires sciclaw") {
		t.Errorf("expected version error, got: %v", err)
	}
}

func TestValidateRequirements_VersionExactMismatch(t *testing.T) {
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{Sciclaw: "0.3.0"},
		Sidecar:  SidecarSpec{Binary: "x"},
	}
	if err := ValidateRequirements(m, "0.2.0", "linux", okLookPath); err == nil {
		t.Error("expected error for exact version mismatch")
	}
	if err := ValidateRequirements(m, "0.3.0", "linux", okLookPath); err != nil {
		t.Errorf("exact match should succeed: %v", err)
	}
}

func TestValidateRequirements_PlatformMismatch(t *testing.T) {
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{
			Sciclaw:  ">=0.1.0",
			Platform: []string{"linux", "darwin"},
		},
		Sidecar: SidecarSpec{Binary: "x"},
	}
	err := ValidateRequirements(m, "0.2.0", "windows", okLookPath)
	if err == nil || !strings.Contains(err.Error(), "platform") {
		t.Errorf("expected platform error, got: %v", err)
	}
}

func TestValidateRequirements_MissingBinary(t *testing.T) {
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{
			Sciclaw: ">=0.1.0",
			Runtime: []string{"docker"},
		},
		Sidecar: SidecarSpec{Binary: "x"},
	}
	missing := func(bin string) (string, error) {
		return "", errors.New("not found")
	}
	err := ValidateRequirements(m, "0.2.0", "linux", missing)
	if err == nil || !strings.Contains(err.Error(), "docker") {
		t.Errorf("expected missing-binary error, got: %v", err)
	}
}

func TestValidateRequirements_HappyPath(t *testing.T) {
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{
			Sciclaw:  ">=0.1.0",
			Runtime:  []string{"docker"},
			Platform: []string{"linux"},
		},
		Sidecar: SidecarSpec{Binary: "x"},
	}
	if err := ValidateRequirements(m, "0.2.5", "linux", okLookPath); err != nil {
		t.Errorf("happy path failed: %v", err)
	}
}

func okLookPath(bin string) (string, error) { return "/usr/bin/" + bin, nil }
