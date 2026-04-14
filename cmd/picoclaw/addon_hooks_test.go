package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/addons"
)

// TestFireAddonHookNilDispatcher verifies that emission sites can call
// fireAddonHook without panicking when the process has not installed a
// dispatcher (tests, one-shot commands, etc.).
func TestFireAddonHookNilDispatcher(t *testing.T) {
	prev := addonDispatcher
	addonDispatcher = nil
	t.Cleanup(func() { addonDispatcher = prev })

	// Must not panic. The payload is arbitrary; no subscriber will see it.
	fireAddonHook("routing_changed", RoutingChangedPayload{Rules: []RoutingChangedRule{{Channel: "a"}, {Channel: "b"}}})
	fireAddonHook("profile_updated", map[string]any{"sender_id": "u1"})
}

// TestFireAddonHookWithDispatcher wires a real dispatcher against an in-memory
// registry and a local httptest.Server on a unix socket masquerading as a
// sidecar, then verifies that fireAddonHook delivers the payload to the
// subscribed addon.
func TestFireAddonHookWithDispatcher(t *testing.T) {
	home := t.TempDir()
	writeHookManifest(t, home, "test-addon", []string{"routing_changed"})

	store := addons.NewStore(home)
	if err := store.Set("test-addon", &addons.RegistryEntry{
		Version:         "0.0.1",
		InstalledAt:     time.Now().UTC().Format(time.RFC3339),
		InstalledCommit: "deadbeef",
		State:           addons.StateEnabled,
		Source:          "test",
	}); err != nil {
		t.Fatalf("registry set: %v", err)
	}

	var delivered atomic.Int32
	var gotEvent atomic.Value
	sock, ts := unixHookTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hook/routing_changed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delivered.Add(1)
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		gotEvent.Store(parsed)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	side := addons.NewSidecar(filepath.Dir(sock), addons.SidecarSpec{
		Socket:     sock,
		HealthPath: "/health",
	})

	prev := addonDispatcher
	prevCtx := addonHookParentCtx
	t.Cleanup(func() { addonDispatcher = prev; addonHookParentCtx = prevCtx })
	SetAddonDispatcher(&addons.Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup: func(name string) *addons.Sidecar {
			if name == "test-addon" {
				return side
			}
			return nil
		},
		Timeout: 2 * time.Second,
	}, context.Background())

	fireAddonHook("routing_changed", RoutingChangedPayload{Rules: []RoutingChangedRule{{Channel: "a"}, {Channel: "b"}}})

	if got := delivered.Load(); got != 1 {
		t.Fatalf("expected 1 delivery to sidecar, got %d", got)
	}
	body, _ := gotEvent.Load().(map[string]any)
	if body == nil {
		t.Fatal("expected payload to be delivered, got nil")
	}
	if _, ok := body["rules"]; !ok {
		t.Errorf("expected payload to contain `rules` key, got %v", body)
	}
}

// TestFireAddonHookBoundedContext verifies that a slow subscriber does not
// hang the emission site for the full 10s helper timeout. The dispatcher's
// per-addon timeout (set short here) is respected, so fireAddonHook returns
// well under the helper's outer 10s bound.
func TestFireAddonHookBoundedContext(t *testing.T) {
	home := t.TempDir()
	writeHookManifest(t, home, "slow-addon", []string{"routing_changed"})

	store := addons.NewStore(home)
	if err := store.Set("slow-addon", &addons.RegistryEntry{
		Version:         "0.0.1",
		InstalledAt:     time.Now().UTC().Format(time.RFC3339),
		InstalledCommit: "deadbeef",
		State:           addons.StateEnabled,
		Source:          "test",
	}); err != nil {
		t.Fatalf("registry set: %v", err)
	}

	blocked := make(chan struct{})
	sock, ts := unixHookTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		close(blocked)
		ts.Close()
	})

	side := addons.NewSidecar(filepath.Dir(sock), addons.SidecarSpec{
		Socket:     sock,
		HealthPath: "/health",
	})

	prev := addonDispatcher
	prevCtx := addonHookParentCtx
	t.Cleanup(func() { addonDispatcher = prev; addonHookParentCtx = prevCtx })
	SetAddonDispatcher(&addons.Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup:      func(name string) *addons.Sidecar { return side },
		// Short per-addon timeout so the test doesn't need to wait 10s.
		Timeout: 150 * time.Millisecond,
	}, context.Background())

	start := time.Now()
	fireAddonHook("routing_changed", RoutingChangedPayload{Rules: []RoutingChangedRule{{Channel: "x"}}})
	elapsed := time.Since(start)
	// Generous upper bound: the per-addon timeout is 150ms, so the emission
	// site should return well before 2s. If this fails the bounded context
	// wiring regressed and core code would block on slow addons.
	if elapsed > 2*time.Second {
		t.Fatalf("fireAddonHook blocked for %v (expected well under 2s)", elapsed)
	}
}

// writeHookManifest drops a minimal addon.json under <home>/addons/<name>/.
// Only the fields the Dispatcher reads are populated.
func writeHookManifest(t *testing.T, home, name string, hooks []string) {
	t.Helper()
	dir := filepath.Join(home, "addons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := map[string]any{
		"name":    name,
		"version": "0.0.1",
		"requires": map[string]any{
			"sciclaw": ">=0.0.0",
		},
		"sidecar": map[string]any{
			"binary": "sc",
		},
		"provides": map[string]any{
			"hooks": hooks,
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "addon.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// unixHookTestServer mirrors pkg/addons/sidecar_test.go's unixTestServer: it
// launches an httptest.Server backed by a short-path unix socket so the
// Sidecar built by NewSidecar can dial it. The short path matters on macOS,
// which enforces a ~104-char sun_path limit that t.TempDir often exceeds.
func unixHookTestServer(t *testing.T, handler http.Handler) (string, *httptest.Server) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets unavailable on windows")
	}
	dir, err := os.MkdirTemp("", "sc")
	if err != nil {
		t.Fatalf("mkdir sock: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sock, err)
	}
	ts := httptest.NewUnstartedServer(handler)
	ts.Listener = ln
	ts.Start()
	return sock, ts
}
