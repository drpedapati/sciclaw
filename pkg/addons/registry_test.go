package addons

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func ptr(s string) *string { return &s }

func TestStore_LoadMissingFileReturnsZeroRegistry(t *testing.T) {
	s := NewStore(t.TempDir())
	r, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r == nil {
		t.Fatal("want non-nil registry")
	}
	if r.Version != 1 {
		t.Errorf("version = %d, want 1", r.Version)
	}
	if r.Addons == nil {
		t.Error("addons map should be initialized")
	}
	if len(r.Addons) != 0 {
		t.Errorf("addons should be empty, got %d", len(r.Addons))
	}
}

func TestStore_SaveLoadRoundTripAllFields(t *testing.T) {
	s := NewStore(t.TempDir())
	prev := "prev123"
	track := "main"
	tag := "v0.1.0"
	entry := &RegistryEntry{
		Version:           "0.1.0",
		InstalledAt:       "2026-04-13T14:22:00Z",
		InstalledCommit:   "abc123",
		ManifestSHA256:    "a1b2c3",
		BootstrapSHA256:   "d4e5f6",
		SidecarSHA256:     "9876ab",
		State:             StateEnabled,
		Source:            "https://github.com/sciclaw/sciclaw-addon-webtop",
		Track:             &track,
		SignedTag:         &tag,
		SignatureVerified: true,
		PreviousCommit:    &prev,
	}
	in := &Registry{
		Version: 1,
		Addons:  map[string]*RegistryEntry{"webtop": entry},
	}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := out.Addons["webtop"]
	if got == nil {
		t.Fatal("webtop entry missing after round trip")
	}
	if !reflect.DeepEqual(entry, got) {
		t.Errorf("round trip mismatch\n want: %+v\n  got: %+v", entry, got)
	}
}

func TestStore_SaveLeavesNoTempFile(t *testing.T) {
	home := t.TempDir()
	s := NewStore(home)
	if err := s.Save(&Registry{Version: 1, Addons: map[string]*RegistryEntry{}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(s.path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("lingering temp file: %s", e.Name())
		}
	}
}

func TestStore_SetPreservesOtherEntries(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Set("a", &RegistryEntry{Version: "0.1.0", State: StateInstalled}); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	if err := s.Set("b", &RegistryEntry{Version: "0.2.0", State: StateEnabled}); err != nil {
		t.Fatalf("Set b: %v", err)
	}

	a, err := s.Get("a")
	if err != nil || a == nil {
		t.Fatalf("Get a: err=%v entry=%v", err, a)
	}
	if a.Version != "0.1.0" {
		t.Errorf("a.version = %q", a.Version)
	}

	b, err := s.Get("b")
	if err != nil || b == nil {
		t.Fatalf("Get b: err=%v entry=%v", err, b)
	}
	if b.Version != "0.2.0" {
		t.Errorf("b.version = %q", b.Version)
	}
}

func TestStore_GetMissingReturnsNilNil(t *testing.T) {
	s := NewStore(t.TempDir())
	entry, err := s.Get("does-not-exist")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry, got %+v", entry)
	}
}

func TestStore_DeleteMissingIsNoOp(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Delete("nope"); err != nil {
		t.Errorf("Delete missing: %v", err)
	}
}

func TestStore_DeleteRemovesEntry(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Set("a", &RegistryEntry{Version: "0.1.0", State: StateInstalled}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected entry gone, got %+v", got)
	}
}

func TestStore_ListReturnsSortedNames(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if err := s.Set(name, &RegistryEntry{Version: "0.1.0", State: StateInstalled}); err != nil {
			t.Fatalf("Set %s: %v", name, err)
		}
	}
	names, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("List = %v, want %v", names, want)
	}
	if !sort.StringsAreSorted(names) {
		t.Error("List result is not sorted")
	}
}

func TestStore_ConcurrentSetIsRaceFree(t *testing.T) {
	s := NewStore(t.TempDir())
	const n = 20

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := string(rune('a' + i%26))
			entry := &RegistryEntry{
				Version:         "0.1.0",
				State:           StateInstalled,
				InstalledCommit: "commit",
			}
			if err := s.Set(name, entry); err != nil {
				t.Errorf("Set %s: %v", name, err)
			}
		}(i)
	}
	wg.Wait()

	r, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(r.Addons) == 0 {
		t.Error("expected at least one addon after concurrent writes")
	}
}

func TestStore_LoadMalformedJSON(t *testing.T) {
	home := t.TempDir()
	s := NewStore(home)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, []byte(`{bogus`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Error("expected error on malformed JSON")
	}
}
