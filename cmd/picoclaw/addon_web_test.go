package main

// addon_web_test.go covers the Wave 4b web endpoints:
//
//   - GET /api/addons/enabled
//   - /addons/<name>/ui/*
//
// Registry and manifest fixtures are written to t.TempDir() and wired into
// the handler via the addonWebHome test seam. The sidecar proxy tests mirror
// pkg/addons/sidecar_test.go: an httptest.Server listens on a Unix socket
// because Sidecar.Proxy dials only over unix.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/addons"
)

// setupAddonHome creates a temp sciclaw home and points the handler at it for
// the life of the test. Returns the home path so callers can drop fixtures.
func setupAddonHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	prev := addonWebHome
	addonWebHome = func() string { return home }
	t.Cleanup(func() { addonWebHome = prev })
	return home
}

// writeAddonManifest drops a minimal but valid addon.json at <home>/addons/<name>/.
// ui_tab is populated so the enabled endpoint has something to surface.
func writeAddonManifest(t *testing.T, home, name, tabName string) {
	t.Helper()
	dir := filepath.Join(home, "addons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir addon dir: %v", err)
	}
	manifest := map[string]any{
		"name":    name,
		"version": "0.1.0",
		"requires": map[string]any{
			"sciclaw": ">=0.0.0",
		},
		"sidecar": map[string]any{
			"binary": "sc",
		},
		"provides": map[string]any{
			"ui_tab": map[string]any{
				"name": tabName,
				"icon": "puzzle",
				"path": "/ui",
			},
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

// writeBrokenManifest drops an unparseable addon.json to exercise the
// skip-on-error branch of handleAddonsEnabled.
func writeBrokenManifest(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, "addons", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir broken addon dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "addon.json"), []byte("{ not valid json"), 0o644); err != nil {
		t.Fatalf("write broken manifest: %v", err)
	}
}

// registerEntry persists a registry entry in the store under <home>.
func registerEntry(t *testing.T, home, name string, state addons.State) {
	t.Helper()
	store := addons.NewStore(home)
	if err := store.Set(name, &addons.RegistryEntry{
		Version:         "0.1.0",
		InstalledAt:     "2026-04-13T00:00:00Z",
		InstalledCommit: "deadbeef",
		State:           state,
		Source:          "test",
	}); err != nil {
		t.Fatalf("registry set %s: %v", name, err)
	}
}

// TestHandleAddonsEnabledEmpty verifies the smoke-test acceptance criterion:
// an empty registry yields 200 + "[]", not 500 or null.
func TestHandleAddonsEnabledEmpty(t *testing.T) {
	setupAddonHome(t)

	srv := newWebServer(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/addons/enabled", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty registry, got %d: %s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("expected empty JSON array, got %q", body)
	}
}

// TestHandleAddonsEnabledFiltersByState verifies that only addons with
// State == enabled appear in the response.
func TestHandleAddonsEnabledFiltersByState(t *testing.T) {
	home := setupAddonHome(t)
	writeAddonManifest(t, home, "webtop", "Desktops")
	writeAddonManifest(t, home, "jupyter", "Notebooks")
	registerEntry(t, home, "webtop", addons.StateEnabled)
	registerEntry(t, home, "jupyter", addons.StateInstalled)

	srv := newWebServer(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/addons/enabled", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []addonEnabledEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enabled entry, got %d: %+v", len(got), got)
	}
	if got[0].Name != "webtop" {
		t.Errorf("expected webtop, got %q", got[0].Name)
	}
	if got[0].State != "enabled" {
		t.Errorf("expected state=enabled, got %q", got[0].State)
	}
	if got[0].UITab == nil || got[0].UITab.Name != "Desktops" {
		t.Errorf("expected ui_tab.name=Desktops, got %+v", got[0].UITab)
	}
}

// TestHandleAddonsEnabledSkipsBrokenManifest verifies that one corrupt
// addon.json does not poison the whole response.
func TestHandleAddonsEnabledSkipsBrokenManifest(t *testing.T) {
	home := setupAddonHome(t)
	writeAddonManifest(t, home, "webtop", "Desktops")
	writeBrokenManifest(t, home, "broken")
	registerEntry(t, home, "webtop", addons.StateEnabled)
	registerEntry(t, home, "broken", addons.StateEnabled)

	srv := newWebServer(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/addons/enabled", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite one broken manifest, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []addonEnabledEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].Name != "webtop" {
		t.Fatalf("expected only webtop in response, got %+v", got)
	}
}

// TestHandleAddonsEnabledWrongMethod verifies non-GET methods are refused.
func TestHandleAddonsEnabledWrongMethod(t *testing.T) {
	setupAddonHome(t)
	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodPost, "/api/addons/enabled", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestHandleAddonProxyNotRunning verifies 503 when the registry has no live
// sidecar for the requested addon.
func TestHandleAddonProxyNotRunning(t *testing.T) {
	prev := addonSidecarRegistry
	addonSidecarRegistry = addons.NewSidecarRegistry()
	t.Cleanup(func() { addonSidecarRegistry = prev })

	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodGet, "/addons/webtop/ui/index.html", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not running") {
		t.Errorf("expected 'not running' in body, got %q", rec.Body.String())
	}
}

// TestHandleAddonProxyNilRegistry verifies that a nil registry (test/CLI
// one-shot context) does not panic and returns 503.
func TestHandleAddonProxyNilRegistry(t *testing.T) {
	prev := addonSidecarRegistry
	addonSidecarRegistry = nil
	t.Cleanup(func() { addonSidecarRegistry = prev })

	srv := newWebServer(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/addons/webtop/ui/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// TestHandleAddonProxyInvalidName is a table test asserting that unsafe
// addon names never reach the sidecar proxy. Some rows are rejected by the
// handler's validator (400); others are canonicalised by net/http's default
// mux before the handler sees them (307 redirect to a cleaned path) — both
// outcomes are acceptable because neither leaks traversal.
func TestHandleAddonProxyInvalidName(t *testing.T) {
	// Install a non-nil registry so the handler reaches the validation
	// branch rather than the 503 nil-registry guard.
	prev := addonSidecarRegistry
	addonSidecarRegistry = addons.NewSidecarRegistry()
	t.Cleanup(func() { addonSidecarRegistry = prev })

	cases := []struct {
		name      string
		path      string
		wantCodes []int // any one of these is acceptable
	}{
		// Literal dotdot encoded in a path segment: the handler rejects
		// this with 400 because it contains a forward slash after decoding.
		{"dotdot in name", "/addons/..%2Fetc%2Fpasswd/ui/", []int{http.StatusBadRequest}},
		// net/http cleans empty, "." and ".." path segments before dispatch
		// and returns 307 to the canonical path. The handler never sees
		// the unsafe form. That is a valid defence-in-depth outcome.
		{"empty name", "/addons//ui/", []int{http.StatusTemporaryRedirect, http.StatusBadRequest}},
		{"hidden prefix", "/addons/.hidden/ui/", []int{http.StatusBadRequest}},
		{"single dot", "/addons/./ui/", []int{http.StatusTemporaryRedirect, http.StatusBadRequest}},
		{"double dot", "/addons/../ui/", []int{http.StatusTemporaryRedirect, http.StatusBadRequest}},
	}

	srv := newWebServer(nil, "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			ok := false
			for _, c := range tc.wantCodes {
				if rec.Code == c {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("expected one of %v for %s, got %d: %s",
					tc.wantCodes, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestValidateAddonName is a direct unit test for the name validator so the
// HTTP-level test can stay focused on wiring.
func TestValidateAddonName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"hidden", ".hidden", true},
		{"slash", "a/b", true},
		{"backslash", "a\\b", true},
		{"nul", "a\x00b", true},
		{"simple", "webtop", false},
		{"hyphen", "sciclaw-webtop", false},
		{"alphanum", "addon1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAddonName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

// TestSplitAddonPath exercises the URL splitter used by the proxy handler.
// Each row maps a post-/addons/ remainder to (name, subpath).
func TestSplitAddonPath(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantPath string
	}{
		{"webtop/ui/", "webtop", "/ui/"},
		{"webtop/ui/index.html", "webtop", "/ui/index.html"},
		{"webtop", "webtop", "/"},
		{"webtop/", "webtop", "/"},
		{"/webtop/ui/", "webtop", "/ui/"},
		{"", "", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotName, gotPath := splitAddonPath(tc.in)
			if gotName != tc.wantName || gotPath != tc.wantPath {
				t.Errorf("splitAddonPath(%q) = (%q, %q); want (%q, %q)",
					tc.in, gotName, gotPath, tc.wantName, tc.wantPath)
			}
		})
	}
}

// TestHandleAddonProxyForwardsToSidecar stands up a fake sidecar on a Unix
// socket and verifies that a request to /addons/<name>/ui/index.html arrives
// at the sidecar as /ui/index.html (prefix stripped) and that the response
// flows back to the caller unchanged.
func TestHandleAddonProxyForwardsToSidecar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets unavailable on windows")
	}

	// Record incoming paths so the assertion can inspect what the sidecar
	// actually saw — the test is primarily about the prefix strip.
	var seenPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("X-From-Sidecar", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sidecar payload:" + r.URL.Path))
	})

	// Short socket path for macOS sun_path limit compatibility.
	sockDir, err := os.MkdirTemp("", "sc")
	if err != nil {
		t.Fatalf("mktemp sock dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ts := httptest.NewUnstartedServer(handler)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)

	// Build a Sidecar bound to the fake socket and register it under the
	// addon name "webtop".
	side := addons.NewSidecar(filepath.Dir(sock), addons.SidecarSpec{
		Socket:     sock,
		HealthPath: "/health",
	})
	prev := addonSidecarRegistry
	addonSidecarRegistry = addons.NewSidecarRegistry()
	addonSidecarRegistry.Register("webtop", side)
	t.Cleanup(func() { addonSidecarRegistry = prev })

	// Put the webServer behind a real TCP listener so http.Get (which the
	// reverse proxy needs for its transport) can talk to it through the
	// standard socket lifecycle.
	srv := newWebServer(nil, "")
	front := httptest.NewServer(srv)
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/addons/webtop/ui/index.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-From-Sidecar") != "yes" {
		t.Errorf("expected X-From-Sidecar header, got %v", resp.Header)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "/ui/index.html") {
		t.Errorf("body = %q; expected sidecar to see /ui/index.html", string(body))
	}
	if seenPath != "/ui/index.html" {
		t.Errorf("sidecar saw path %q; expected /ui/index.html (prefix should be stripped)", seenPath)
	}
}

// TestHandleAddonProxyForwardsRoot verifies that /addons/<name>/ui/ (trailing
// slash) arrives at the sidecar as /ui/ — the common iframe entrypoint.
func TestHandleAddonProxyForwardsRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets unavailable on windows")
	}
	var seenPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	sockDir, err := os.MkdirTemp("", "sc")
	if err != nil {
		t.Fatalf("mktemp sock dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ts := httptest.NewUnstartedServer(handler)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)

	side := addons.NewSidecar(filepath.Dir(sock), addons.SidecarSpec{
		Socket:     sock,
		HealthPath: "/health",
	})
	prev := addonSidecarRegistry
	addonSidecarRegistry = addons.NewSidecarRegistry()
	addonSidecarRegistry.Register("webtop", side)
	t.Cleanup(func() { addonSidecarRegistry = prev })

	srv := newWebServer(nil, "")
	front := httptest.NewServer(srv)
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/addons/webtop/ui/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if seenPath != "/ui/" {
		t.Errorf("sidecar saw path %q; expected /ui/", seenPath)
	}
}
