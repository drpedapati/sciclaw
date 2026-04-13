package addons

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sbomFixedClock() func() time.Time {
	return func() time.Time {
		t, _ := time.Parse(time.RFC3339, "2026-04-13T12:00:00Z")
		return t
	}
}

func populatedStore(t *testing.T) (*Store, *Manifest) {
	t.Helper()
	store := NewStore(t.TempDir())
	track := "main"
	tag := "v0.1.0"
	prev := "aaaaaaaa"
	entry := &RegistryEntry{
		Version:           "0.1.0",
		InstalledAt:       "2026-04-13T14:22:00Z",
		InstalledCommit:   "abc123def456",
		ManifestSHA256:    "manifest-hash",
		BootstrapSHA256:   "bootstrap-hash",
		SidecarSHA256:     "sidecar-hash",
		State:             StateEnabled,
		Source:            "https://github.com/sciclaw/sciclaw-addon-webtop",
		Track:             &track,
		SignedTag:         &tag,
		SignatureVerified: true,
		PreviousCommit:    &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Name:    "webtop",
		Version: "0.1.0",
		Requires: Requirements{
			Sciclaw:  ">=0.3.0",
			Runtime:  []string{"docker", "git"},
			Platform: []string{"linux/amd64", "darwin/arm64"},
		},
		Sidecar: SidecarSpec{Binary: "bin/sciclaw-addon-webtop"},
	}
	return store, m
}

func TestExport_HappyPath(t *testing.T) {
	store, m := populatedStore(t)
	sbom, err := Export(store, m, "webtop", "linux/amd64", sbomFixedClock())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if sbom.Name != "webtop" {
		t.Errorf("Name = %q", sbom.Name)
	}
	if sbom.InstalledCommit != "abc123def456" {
		t.Errorf("InstalledCommit = %q", sbom.InstalledCommit)
	}
	if sbom.SignedTag != "v0.1.0" {
		t.Errorf("SignedTag = %q", sbom.SignedTag)
	}
	if !sbom.SignatureVerified {
		t.Error("SignatureVerified should be true")
	}
	if sbom.Track != "main" {
		t.Errorf("Track = %q", sbom.Track)
	}
	if sbom.Platform != "linux/amd64" {
		t.Errorf("Platform = %q", sbom.Platform)
	}
	if sbom.ExportedAt != "2026-04-13T12:00:00Z" {
		t.Errorf("ExportedAt = %q", sbom.ExportedAt)
	}
	// Dependencies: 1 sciclaw + 2 runtime + 2 platform = 5
	if got := len(sbom.Dependencies); got != 5 {
		t.Fatalf("want 5 deps, got %d: %+v", got, sbom.Dependencies)
	}
	if sbom.Dependencies[0].Kind != "sciclaw" {
		t.Errorf("first dep should be sciclaw, got %+v", sbom.Dependencies[0])
	}
}

func TestExport_AddonNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	m := &Manifest{Name: "ghost", Version: "0.1.0", Requires: Requirements{Sciclaw: ">=0.1.0"}, Sidecar: SidecarSpec{Binary: "x"}}
	_, err := Export(store, m, "ghost", "linux/amd64", sbomFixedClock())
	if err == nil {
		t.Fatal("expected error for missing addon")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say not found, got %v", err)
	}
}

func TestExport_NilArgsError(t *testing.T) {
	store := NewStore(t.TempDir())
	m := &Manifest{Name: "x", Version: "0.1.0", Requires: Requirements{Sciclaw: ">=0.1.0"}, Sidecar: SidecarSpec{Binary: "x"}}
	if _, err := Export(nil, m, "x", "p", sbomFixedClock()); err == nil {
		t.Error("nil store should error")
	}
	if _, err := Export(store, nil, "x", "p", sbomFixedClock()); err == nil {
		t.Error("nil manifest should error")
	}
	if _, err := Export(store, m, "", "p", sbomFixedClock()); err == nil {
		t.Error("empty name should error")
	}
}

func TestExport_DependenciesIncludeAllRuntimes(t *testing.T) {
	store, m := populatedStore(t)
	sbom, err := Export(store, m, "webtop", "linux/amd64", sbomFixedClock())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"docker": false, "git": false}
	for _, d := range sbom.Dependencies {
		if d.Kind == "runtime" {
			want[d.Value] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("runtime dependency %q missing from SBOM", k)
		}
	}
}

func TestExport_DependenciesSkipEmptyEntries(t *testing.T) {
	store := NewStore(t.TempDir())
	entry := &RegistryEntry{
		Version:         "0.1.0",
		InstalledAt:     "2026-04-13T14:22:00Z",
		InstalledCommit: "abc",
		ManifestSHA256:  "h",
		State:           StateEnabled,
		Source:          "https://example.com",
	}
	if err := store.Set("x", entry); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Name:    "x",
		Version: "0.1.0",
		Requires: Requirements{
			Sciclaw:  ">=0.1.0",
			Runtime:  []string{"", "docker", " "},
			Platform: []string{"linux/amd64", ""},
		},
		Sidecar: SidecarSpec{Binary: "bin/x"},
	}
	sbom, err := Export(store, m, "x", "linux/amd64", sbomFixedClock())
	if err != nil {
		t.Fatal(err)
	}
	// 1 sciclaw + 1 docker + 1 platform = 3
	if len(sbom.Dependencies) != 3 {
		t.Errorf("want 3 deps (empty entries skipped), got %d: %+v", len(sbom.Dependencies), sbom.Dependencies)
	}
}

func TestWriteSBOM_RoundTrip(t *testing.T) {
	store, m := populatedStore(t)
	sbom, err := Export(store, m, "webtop", "linux/amd64", sbomFixedClock())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteSBOM(&buf, sbom); err != nil {
		t.Fatalf("WriteSBOM: %v", err)
	}
	// JSON must be parseable.
	var decoded SBOM
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("round-trip unmarshal: %v\njson: %s", err, buf.String())
	}
	if !reflect.DeepEqual(sbom, &decoded) {
		t.Errorf("round trip mismatch\n want: %+v\n  got: %+v", sbom, &decoded)
	}
	// Must be indented.
	if !strings.Contains(buf.String(), "\n  \"") {
		t.Error("output should be indented JSON")
	}
	// Must end in a trailing newline.
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("output should end with newline")
	}
}

func TestWriteSBOM_NullableFieldsOmitted(t *testing.T) {
	// Registry entry with no Track, no PreviousCommit, no SignedTag.
	store := NewStore(t.TempDir())
	entry := &RegistryEntry{
		Version:         "0.1.0",
		InstalledAt:     "2026-04-13T14:22:00Z",
		InstalledCommit: "abc",
		ManifestSHA256:  "h",
		State:           StateEnabled,
		Source:          "https://example.com/x",
	}
	if err := store.Set("x", entry); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Name:     "x",
		Version:  "0.1.0",
		Requires: Requirements{Sciclaw: ">=0.1.0"},
		Sidecar:  SidecarSpec{Binary: "bin/x"},
	}
	sbom, err := Export(store, m, "x", "linux/amd64", sbomFixedClock())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteSBOM(&buf, sbom); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	// Omitempty fields should not appear in the output.
	for _, field := range []string{"signed_tag", "track", "bootstrap_sha256", "sidecar_sha256"} {
		if strings.Contains(s, field) {
			t.Errorf("expected %q to be omitted from SBOM with nullable fields, got:\n%s", field, s)
		}
	}
}

func TestWriteSBOM_NilInputsError(t *testing.T) {
	if err := WriteSBOM(nil, &SBOM{}); err == nil {
		t.Error("nil writer should error")
	}
	var buf bytes.Buffer
	if err := WriteSBOM(&buf, nil); err == nil {
		t.Error("nil sbom should error")
	}
}

func TestExport_DefaultsNowFunc(t *testing.T) {
	store, m := populatedStore(t)
	// Pass nil for now — Export should default to time.Now without crashing.
	sbom, err := Export(store, m, "webtop", "linux/amd64", nil)
	if err != nil {
		t.Fatalf("Export with nil now: %v", err)
	}
	if sbom.ExportedAt == "" {
		t.Error("ExportedAt should be stamped")
	}
	if _, err := time.Parse(time.RFC3339, sbom.ExportedAt); err != nil {
		t.Errorf("ExportedAt not RFC3339: %v", err)
	}
}
