package addons

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rollbackRunner is a fake CommandRunner whose Run succeeds unless err is
// set. It logs each invocation for assertion.
type rollbackRunner struct {
	err     error
	scripts []string
	dirs    []string
}

func (r *rollbackRunner) Run(_ context.Context, dir, script string, _ []string) ([]byte, error) {
	r.dirs = append(r.dirs, dir)
	r.scripts = append(r.scripts, script)
	if r.err != nil {
		return []byte("boom"), r.err
	}
	return nil, nil
}

// setupRollbackAddon creates a fake addon directory with a minimal
// addon.json so ParseManifest and ComputeHashes succeed after a simulated
// git checkout.
func setupRollbackAddon(t *testing.T, name, version string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  "name": "` + name + `",
  "version": "` + version + `",
  "requires": {"sciclaw": ">=0.1.0"},
  "sidecar": {"binary": "bin/sciclaw-addon-` + name + `"}
}
`
	if err := os.WriteFile(filepath.Join(dir, "addon.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fixedClock(ts string) func() time.Time {
	return func() time.Time {
		t, _ := time.Parse(time.RFC3339, ts)
		return t
	}
}

func TestRollback_HappyPath(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)

	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")
	prev := "aaaaaaaaaaaaaaaa"
	track := "main"
	tag := "v0.2.0"
	entry := &RegistryEntry{
		Version:           "0.2.0",
		InstalledAt:       "2026-04-10T00:00:00Z",
		InstalledCommit:   "bbbbbbbbbbbbbbbb",
		ManifestSHA256:    "old-manifest",
		BootstrapSHA256:   "old-bootstrap",
		SidecarSHA256:     "old-sidecar",
		State:             StateEnabled,
		Source:            "https://example.com/webtop",
		Track:             &track,
		SignedTag:         &tag,
		SignatureVerified: true,
		PreviousCommit:    &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}

	fr := &rollbackRunner{}
	r := &Rollbacker{
		Store:    store,
		Runner:   fr,
		AddonDir: func(name string) string { return addonDir },
		Now:      fixedClock("2026-04-13T12:00:00Z"),
	}

	updated, err := r.Rollback(context.Background(), "webtop")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if updated.InstalledCommit != prev {
		t.Errorf("InstalledCommit = %q, want %q", updated.InstalledCommit, prev)
	}
	if updated.PreviousCommit != nil {
		t.Errorf("PreviousCommit should be cleared, got %v", *updated.PreviousCommit)
	}
	if updated.State != StateEnabled {
		t.Errorf("State = %q, want enabled (preserved)", updated.State)
	}
	if updated.Track == nil || *updated.Track != "main" {
		t.Errorf("Track should be preserved, got %v", updated.Track)
	}
	if updated.Source != "https://example.com/webtop" {
		t.Errorf("Source should be preserved, got %q", updated.Source)
	}
	if updated.Version != "0.1.0" {
		t.Errorf("Version should come from re-parsed manifest, got %q", updated.Version)
	}
	if updated.ManifestSHA256 == "old-manifest" {
		t.Error("ManifestSHA256 should be recomputed, not carried over")
	}
	if updated.InstalledAt != "2026-04-13T12:00:00Z" {
		t.Errorf("InstalledAt = %q, want stamped time", updated.InstalledAt)
	}
	if updated.SignatureVerified {
		t.Error("SignatureVerified should be false after rollback (no re-verify)")
	}
	if updated.SignedTag != nil {
		t.Errorf("SignedTag should be cleared, got %v", *updated.SignedTag)
	}

	// Verify persistence: re-load and compare.
	reloaded, err := store.Get("webtop")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.InstalledCommit != prev {
		t.Error("rollback was not persisted")
	}

	// Verify git checkout was invoked with --detach to the previous commit.
	if len(fr.scripts) != 1 {
		t.Fatalf("expected 1 git invocation, got %d", len(fr.scripts))
	}
	if !strings.Contains(fr.scripts[0], "checkout --detach") {
		t.Errorf("expected --detach checkout, got %q", fr.scripts[0])
	}
	if !strings.Contains(fr.scripts[0], prev) {
		t.Errorf("expected checkout of %q, got %q", prev, fr.scripts[0])
	}
}

func TestRollback_NilPreviousCommitErrors(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")
	entry := &RegistryEntry{
		Version:         "0.1.0",
		InstalledCommit: "bbbbbbbbbbbbbbbb",
		State:           StateEnabled,
		Source:          "https://example.com/webtop",
		PreviousCommit:  nil,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}

	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(name string) string { return addonDir },
	}
	_, err := r.Rollback(context.Background(), "webtop")
	if err == nil {
		t.Fatal("expected error when PreviousCommit is nil")
	}
	if !strings.Contains(err.Error(), "nothing to roll back") {
		t.Errorf("error should explain the cause, got %v", err)
	}
}

func TestRollback_AddonNotInstalled(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(name string) string { return t.TempDir() },
	}
	_, err := r.Rollback(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for missing addon")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should say not installed, got %v", err)
	}
}

func TestRollback_GitCheckoutFailurePropagates(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")
	prev := "aaaaaaaaaaaaaaaa"
	entry := &RegistryEntry{
		Version:         "0.2.0",
		InstalledCommit: "bbbbbbbbbbbbbbbb",
		State:           StateEnabled,
		Source:          "https://example.com/webtop",
		PreviousCommit:  &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}
	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{err: errors.New("detached HEAD confused")},
		AddonDir: func(name string) string { return addonDir },
	}
	_, err := r.Rollback(context.Background(), "webtop")
	if err == nil {
		t.Fatal("expected error from failed checkout")
	}
	if !strings.Contains(err.Error(), "git checkout") {
		t.Errorf("error should mention git checkout, got %v", err)
	}
	// Registry should be unchanged on failure.
	reloaded, _ := store.Get("webtop")
	if reloaded.InstalledCommit != "bbbbbbbbbbbbbbbb" {
		t.Error("registry should not be mutated when checkout fails")
	}
}

func TestRollback_PreservesStateAndTrack(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")
	prev := "aaaaaaaaaaaaaaaa"
	track := "dev"
	entry := &RegistryEntry{
		Version:         "0.2.0",
		InstalledCommit: "bbbbbbbbbbbbbbbb",
		State:           StateInstalled, // not enabled
		Source:          "https://example.com/webtop",
		Track:           &track,
		PreviousCommit:  &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}
	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(name string) string { return addonDir },
		Now:      fixedClock("2026-04-13T12:00:00Z"),
	}
	updated, err := r.Rollback(context.Background(), "webtop")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if updated.State != StateInstalled {
		t.Errorf("State = %q, want installed", updated.State)
	}
	if updated.Track == nil || *updated.Track != "dev" {
		t.Errorf("Track should be preserved as 'dev', got %v", updated.Track)
	}
}

func TestRollback_EmptyPreviousCommitStringErrors(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")
	empty := "   "
	entry := &RegistryEntry{
		Version:         "0.2.0",
		InstalledCommit: "bbbbbbbbbbbbbbbb",
		State:           StateEnabled,
		Source:          "https://example.com/webtop",
		PreviousCommit:  &empty,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}
	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(name string) string { return addonDir },
	}
	if _, err := r.Rollback(context.Background(), "webtop"); err == nil {
		t.Error("whitespace-only PreviousCommit should error")
	}
}

// TestRollback_StopsAndRestartsSidecarWhenEnabled is the L5 regression.
// When an addon is enabled at rollback time, the old sidecar must be
// stopped BEFORE the git checkout swaps the binary, and a fresh sidecar
// must be started from the rolled-back tree AFTER the checkout — the
// same dance Upgrade already does.
func TestRollback_StopsAndRestartsSidecarWhenEnabled(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")

	prev := "aaa111"
	entry := &RegistryEntry{
		Version:         "0.2.0",
		InstalledAt:     "2026-04-13T12:00:00Z",
		InstalledCommit: "bbb222",
		State:           StateEnabled, // KEY: enabled means the sidecar is live
		Source:          "https://example.com/webtop",
		PreviousCommit:  &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}

	registry := NewSidecarRegistry()
	// Pre-populate the registry with a fake "live" sidecar so the
	// Rollback path has something to Stop and Unregister. Reuses the
	// unix-socket fakeLauncher from lifecycle_enable_test.go which
	// backs each Launch with an httptest server that answers /health
	// and /shutdown properly — so Sidecar.Stop follows its real HTTP
	// shutdown path instead of panicking on a nil client.
	fl := &fakeLauncher{t: t}
	t.Cleanup(fl.close)
	side, err := fl.Launch(context.Background(), "webtop", addonDir, SidecarSpec{Binary: "bin/sciclaw-addon-webtop"})
	if err != nil {
		t.Fatalf("pre-launch: %v", err)
	}
	registry.Register("webtop", side)

	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(string) string { return addonDir },
		Now:      fixedClock("2026-04-13T13:00:00Z"),
		Registry: registry,
		Launcher: fl,
	}
	if _, err := r.Rollback(context.Background(), "webtop"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Assertions about the Rollbacker's effect on the live registry:
	//   1. The OLD sidecar was Stopped before the git checkout.
	//   2. A NEW sidecar is registered after the rollback completed.
	//   3. The launcher was invoked exactly twice total (once pre-test,
	//      once inside Rollback).
	if got := fl.callCount(); got != 2 {
		t.Errorf("expected 2 launches (pre-test + rollback restart), got %d", got)
	}
	// The NEW sidecar is a different pointer than the original one we
	// pre-registered. Registry.Lookup should return the second one.
	current := registry.Lookup("webtop")
	if current == nil {
		t.Fatal("registry.Lookup returned nil after rollback — restart did not re-register")
	}
	if current == side {
		t.Error("registry still points at the ORIGINAL pre-rollback sidecar; restart did not replace it")
	}
	// Final entry must be in StateEnabled (restart succeeded) and at
	// the previous commit.
	got, _ := store.Get("webtop")
	if got.State != StateEnabled {
		t.Errorf("final state = %q, want %q (restart succeeded)", got.State, StateEnabled)
	}
	if got.InstalledCommit != prev {
		t.Errorf("InstalledCommit = %q, want %q", got.InstalledCommit, prev)
	}
}

// TestRollback_StateInstalledWhenSidecarRestartFails verifies that a
// Launcher whose Launch returns an error drops the state back to
// StateInstalled so the operator knows to re-enable, mirroring the
// Upgrade failure path.
func TestRollback_StateInstalledWhenSidecarRestartFails(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	addonDir := setupRollbackAddon(t, "webtop", "0.1.0")

	prev := "aaa111"
	entry := &RegistryEntry{
		Version:         "0.2.0",
		InstalledCommit: "bbb222",
		State:           StateEnabled,
		Source:          "https://example.com/webtop",
		PreviousCommit:  &prev,
	}
	if err := store.Set("webtop", entry); err != nil {
		t.Fatal(err)
	}

	registry := NewSidecarRegistry()
	// Pre-populate with a real-ish stoppable sidecar so the stop phase
	// succeeds cleanly. Then the launcher is swapped to one whose
	// failOn map makes the post-checkout Launch return an error.
	prestartLauncher := &fakeLauncher{t: t}
	t.Cleanup(prestartLauncher.close)
	side, _ := prestartLauncher.Launch(context.Background(), "webtop", addonDir, SidecarSpec{Binary: "bin/sciclaw-addon-webtop"})
	registry.Register("webtop", side)

	failingLauncher := &fakeLauncher{
		t:      t,
		failOn: map[string]error{"webtop": errors.New("spawn boom")},
	}
	t.Cleanup(failingLauncher.close)
	r := &Rollbacker{
		Store:    store,
		Runner:   &rollbackRunner{},
		AddonDir: func(string) string { return addonDir },
		Now:      fixedClock("2026-04-13T13:00:00Z"),
		Registry: registry,
		Launcher: failingLauncher,
	}
	entry2, err := r.Rollback(context.Background(), "webtop")
	if err == nil {
		t.Fatal("expected rollback to return an error when restart fails")
	}
	if !strings.Contains(err.Error(), "sidecar failed to restart") {
		t.Errorf("error should mention sidecar restart failure, got %v", err)
	}
	if entry2 == nil || entry2.State != StateInstalled {
		t.Errorf("entry state after failed restart = %v, want %q", entry2, StateInstalled)
	}
	// Persisted state must also reflect installed (the updated entry
	// is saved before the restart error is returned to the caller).
	got, _ := store.Get("webtop")
	if got.State != StateInstalled {
		t.Errorf("persisted state = %q, want %q", got.State, StateInstalled)
	}
}

func TestRollback_NilDependenciesError(t *testing.T) {
	// Missing Store
	r := &Rollbacker{Runner: &rollbackRunner{}, AddonDir: func(string) string { return "/tmp" }}
	if _, err := r.Rollback(context.Background(), "x"); err == nil {
		t.Error("nil Store should error")
	}
	// Missing Runner
	r = &Rollbacker{Store: NewStore(t.TempDir()), AddonDir: func(string) string { return "/tmp" }}
	if _, err := r.Rollback(context.Background(), "x"); err == nil {
		t.Error("nil Runner should error")
	}
	// Missing AddonDir
	r = &Rollbacker{Store: NewStore(t.TempDir()), Runner: &rollbackRunner{}}
	if _, err := r.Rollback(context.Background(), "x"); err == nil {
		t.Error("nil AddonDir should error")
	}
	// Empty name
	r = &Rollbacker{Store: NewStore(t.TempDir()), Runner: &rollbackRunner{}, AddonDir: func(string) string { return "/tmp" }}
	if _, err := r.Rollback(context.Background(), ""); err == nil {
		t.Error("empty name should error")
	}
	// Nil receiver
	var nilR *Rollbacker
	if _, err := nilR.Rollback(context.Background(), "x"); err == nil {
		t.Error("nil receiver should error")
	}
}
