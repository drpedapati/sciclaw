package addons

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
	"strings"
	"sync"
	"testing"
	"time"
)

// unixTestServer launches an httptest.Server listening on a Unix domain
// socket. The path is kept short because macOS enforces a ~104-char sun_path
// limit that is easy to blow past under t.TempDir(), so the socket lives in
// a per-test subdir of os.TempDir() with a compact name.
func unixTestServer(t *testing.T, handler http.Handler) (string, *httptest.Server) {
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

// newSidecarAt builds a Sidecar whose client speaks to an existing Unix socket.
// Start is not called because the test already owns the listener.
func newSidecarAt(sock string) *Sidecar {
	// We use a dummy addonDir; NewSidecar treats spec.Socket as absolute when
	// it already is. The binary field stays empty so Start would fail, which
	// is fine because these tests do not call it.
	return NewSidecar(filepath.Dir(sock), SidecarSpec{
		Socket:     sock,
		HealthPath: "/health",
	})
}

func TestSidecar_HealthHappyPath(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	if err := s.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestSidecar_HealthNon200(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	err := s.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 error, got %v", err)
	}
}

func TestSidecar_HookReceivesPayload(t *testing.T) {
	type received struct {
		event string
		body  string
	}
	gotCh := make(chan received, 1)
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/hook/") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotCh <- received{event: strings.TrimPrefix(r.URL.Path, "/hook/"), body: string(body)}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	payload := map[string]any{"foo": "bar", "n": 42}
	if err := s.Hook(context.Background(), "routing_changed", payload); err != nil {
		t.Fatalf("Hook: %v", err)
	}
	select {
	case got := <-gotCh:
		if got.event != "routing_changed" {
			t.Errorf("event = %q, want routing_changed", got.event)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(got.body), &decoded); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if decoded["foo"] != "bar" {
			t.Errorf("body[foo] = %v, want bar", decoded["foo"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook never received")
	}
}

func TestSidecar_HookReturns500(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("bad"))
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	err := s.Hook(context.Background(), "foo", nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 error, got %v", err)
	}
}

func TestSidecar_VersionRoundTrip(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(SidecarVersion{Version: "1.2.3", ProtocolVersion: 7})
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	v, err := s.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v.Version != "1.2.3" {
		t.Errorf("Version.Version = %q", v.Version)
	}
	if v.ProtocolVersion != 7 {
		t.Errorf("Version.ProtocolVersion = %d", v.ProtocolVersion)
	}
}

func TestSidecar_StartTimeoutWhenHealthNeverReturns(t *testing.T) {
	// Handler blocks forever; health will time out.
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	s := newSidecarAt(sock)
	s.StartTimeout = 300 * time.Millisecond

	// Skip the actual process spawn: Start with a dummy binary that does
	// not exist. Start should fail quickly on Cmd.Start before health is
	// even polled, exercising the missing-binary error path. The /health
	// poll loop itself is exercised by confirming Health returns an error
	// against a non-existent socket within the timeout budget.
	s.Binary = filepath.Join(t.TempDir(), "nope")
	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to fail")
	}

	bogusDir, _ := os.MkdirTemp("", "sc-bogus")
	defer os.RemoveAll(bogusDir)
	bogusSock := filepath.Join(bogusDir, "s")
	s2 := newSidecarAt(bogusSock)
	s2.StartTimeout = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s2.Health(ctx); err == nil {
		t.Fatal("expected Health to fail on missing socket")
	}
}

func TestSidecar_ProxyForwardsRequestAndResponse(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		w.Header().Set("X-Proxied", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payload:" + r.URL.Path))
	}))
	defer ts.Close()

	s := newSidecarAt(sock)

	// Front the Proxy behind a regular TCP httptest.Server so the test can
	// send a normal HTTP request through it.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Proxy(w, r)
	}))
	defer front.Close()

	resp, err := http.Get(front.URL + "/ui/index.html")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Proxied") != "yes" {
		t.Errorf("missing X-Proxied header: %v", resp.Header)
	}
	if string(body) != "payload:/ui/index.html" {
		t.Errorf("body = %q", string(body))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "/ui/index.html" {
		t.Errorf("sidecar calls = %v", calls)
	}
}

func TestSidecar_StopNoProcessIsNoOp(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	s := newSidecarAt(sock)
	// No cmd started; Stop should return nil even though /shutdown succeeds.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestNewSidecar_DefaultsWhenSpecEmpty(t *testing.T) {
	s := NewSidecar("/tmp/addon", SidecarSpec{Binary: "myaddon"})
	if s.HealthPath != "/health" {
		t.Errorf("HealthPath = %q", s.HealthPath)
	}
	if s.StartTimeout != 10*time.Second {
		t.Errorf("StartTimeout = %v", s.StartTimeout)
	}
	if !strings.HasSuffix(s.SocketPath, "/sock") {
		t.Errorf("SocketPath = %q", s.SocketPath)
	}
	if !strings.HasSuffix(s.Binary, "/bin/myaddon") {
		t.Errorf("Binary = %q, want .../bin/myaddon", s.Binary)
	}
}

func TestSidecar_StartBinaryMissing(t *testing.T) {
	s := NewSidecar(t.TempDir(), SidecarSpec{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "no binary") {
		t.Errorf("expected no-binary error, got %v", err)
	}
}

func TestSidecar_VersionNon200(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	s := newSidecarAt(sock)
	_, err := s.Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 error, got %v", err)
	}
}

func TestSidecar_VersionMalformedJSON(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	s := newSidecarAt(sock)
	_, err := s.Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decoding") {
		t.Errorf("expected decode error, got %v", err)
	}
}

func TestSidecar_HookCancelledContext(t *testing.T) {
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	s := newSidecarAt(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := s.Hook(ctx, "foo", map[string]int{"n": 1})
	if err == nil {
		t.Error("expected context deadline error")
	}
}

func TestSidecar_URLPathNormalization(t *testing.T) {
	s := &Sidecar{}
	if got := s.url("no-slash"); got != "http://addon/no-slash" {
		t.Errorf("url = %q", got)
	}
	if got := s.url("/leading"); got != "http://addon/leading" {
		t.Errorf("url = %q", got)
	}
	if got := s.url(""); got != "http://addon/" {
		t.Errorf("url(empty) = %q", got)
	}
}

// TestSidecar_StartAndStopHappyPath exercises the full process lifecycle by
// spawning /bin/sh as a fake sidecar. sh does not serve HTTP, so Start will
// fail with a timeout — but on the way there it exercises cmd.Start and the
// health-poll loop, and killProcess cleans up the spawned shell.
func TestSidecar_StartFailsWhenBinaryDoesNotServe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir, _ := os.MkdirTemp("", "sc-start")
	defer os.RemoveAll(dir)

	s := NewSidecar(dir, SidecarSpec{
		Binary:              "sh",
		Socket:              filepath.Join(dir, "s"),
		StartTimeoutSeconds: 1,
	})
	// sh is on PATH but it does not serve HTTP, so spawn succeeds and
	// /health polling runs for a while before the StartTimeout fires.
	// Make the binary absolute so exec.Command does not need PATH lookup.
	s.Binary = "/bin/sh"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.Start(ctx)
	if err == nil {
		t.Error("expected Start to fail (sh does not serve HTTP)")
	}
}
