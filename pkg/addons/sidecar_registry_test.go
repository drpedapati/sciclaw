package addons

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newHealthySidecar returns a *Sidecar backed by a unix-socket httptest
// server that answers /health with 200 and /shutdown with 204. Used to test
// Register/Lookup/StopAll against the real Sidecar type.
func newHealthySidecar(t *testing.T, name string) (*Sidecar, func()) {
	t.Helper()
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/shutdown":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	s := newSidecarAt(sock)
	s.Name = name
	return s, func() { ts.Close() }
}

func TestSidecarRegistry_RegisterAndLookup(t *testing.T) {
	r := NewSidecarRegistry()
	s, cleanup := newHealthySidecar(t, "webtop")
	defer cleanup()

	if got := r.Lookup("webtop"); got != nil {
		t.Errorf("Lookup before Register = %v, want nil", got)
	}

	r.Register("webtop", s)
	got := r.Lookup("webtop")
	if got != s {
		t.Errorf("Lookup after Register = %v, want %v", got, s)
	}
}

func TestSidecarRegistry_Unregister(t *testing.T) {
	r := NewSidecarRegistry()
	s, cleanup := newHealthySidecar(t, "jupyter")
	defer cleanup()

	r.Register("jupyter", s)
	r.Unregister("jupyter")

	if got := r.Lookup("jupyter"); got != nil {
		t.Errorf("Lookup after Unregister = %v, want nil", got)
	}

	// Unregister unknown is a no-op.
	r.Unregister("ghost")
}

func TestSidecarRegistry_LookupUnknownReturnsNil(t *testing.T) {
	r := NewSidecarRegistry()
	if got := r.Lookup("nope"); got != nil {
		t.Errorf("Lookup unknown = %v, want nil", got)
	}
}

func TestSidecarRegistry_ListSorted(t *testing.T) {
	r := NewSidecarRegistry()
	s1, c1 := newHealthySidecar(t, "zebra")
	defer c1()
	s2, c2 := newHealthySidecar(t, "alpha")
	defer c2()
	s3, c3 := newHealthySidecar(t, "mango")
	defer c3()

	r.Register("zebra", s1)
	r.Register("alpha", s2)
	r.Register("mango", s3)

	got := r.List()
	want := []string{"alpha", "mango", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestSidecarRegistry_ListEmpty(t *testing.T) {
	r := NewSidecarRegistry()
	got := r.List()
	if len(got) != 0 {
		t.Errorf("List on empty = %v, want empty", got)
	}
}

func TestSidecarRegistry_RegisterReplacesExisting(t *testing.T) {
	r := NewSidecarRegistry()
	s1, c1 := newHealthySidecar(t, "v1")
	defer c1()
	s2, c2 := newHealthySidecar(t, "v2")
	defer c2()

	r.Register("addon", s1)
	r.Register("addon", s2)

	if got := r.Lookup("addon"); got != s2 {
		t.Errorf("Lookup = %v, want %v (replacement)", got, s2)
	}
}

// TestSidecarRegistry_ConcurrentAccess exercises the Register/Unregister/
// Lookup paths under heavy concurrency so the race detector can flag any
// unlocked access. Run with -race.
func TestSidecarRegistry_ConcurrentAccess(t *testing.T) {
	r := NewSidecarRegistry()
	// Pre-populate with 16 sidecars whose sockets we never touch; Lookup
	// just returns pointers.
	sidecars := make(map[string]*Sidecar, 16)
	for i := 0; i < 16; i++ {
		s, cleanup := newHealthySidecar(t, fmt.Sprintf("preload-%d", i))
		defer cleanup()
		sidecars[fmt.Sprintf("name-%d", i)] = s
	}

	var wg sync.WaitGroup
	const goroutines = 32
	const iters = 200
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				key := fmt.Sprintf("name-%d", (id+i)%16)
				switch i % 4 {
				case 0:
					r.Register(key, sidecars[key])
				case 1:
					_ = r.Lookup(key)
				case 2:
					_ = r.List()
				case 3:
					r.Unregister(key)
				}
			}
		}(g)
	}
	wg.Wait()

	// Sanity: the map is in some consistent state — further Register/Lookup
	// must not panic.
	for k, s := range sidecars {
		r.Register(k, s)
	}
	if got := len(r.List()); got != len(sidecars) {
		t.Errorf("final List len = %d, want %d", got, len(sidecars))
	}
}

// stopCountingSidecar wraps a regular sidecar but records how many times
// Stop gets called. We use this to verify StopAll.
type stopTrackingServer struct {
	calls *int32
}

func (s *stopTrackingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		w.WriteHeader(http.StatusOK)
	case "/shutdown":
		atomic.AddInt32(s.calls, 1)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func TestSidecarRegistry_StopAllCallsEverySidecar(t *testing.T) {
	r := NewSidecarRegistry()

	// Spawn three sidecars, each with its own shutdown counter. Because
	// StopAll calls Sidecar.Stop which issues POST /shutdown, we can
	// observe the call count via the handler.
	type held struct {
		name    string
		calls   *int32
		cleanup func()
	}
	var helds []held
	for _, name := range []string{"a", "b", "c"} {
		var count int32
		sock, ts := unixTestServer(t, &stopTrackingServer{calls: &count})
		s := newSidecarAt(sock)
		s.Name = name
		r.Register(name, s)
		helds = append(helds, held{name: name, calls: &count, cleanup: ts.Close})
	}
	defer func() {
		for _, h := range helds {
			h.cleanup()
		}
	}()

	var logged []string
	logFn := func(name string, err error) {
		logged = append(logged, name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.StopAll(ctx, logFn)

	for _, h := range helds {
		if got := atomic.LoadInt32(h.calls); got != 1 {
			t.Errorf("sidecar %q: /shutdown called %d times, want 1", h.name, got)
		}
	}

	// After StopAll the registry is empty.
	if remaining := r.List(); len(remaining) != 0 {
		t.Errorf("registry not empty after StopAll: %v", remaining)
	}
}

func TestSidecarRegistry_StopAllRespectsContextDeadline(t *testing.T) {
	r := NewSidecarRegistry()

	// release gates the /shutdown handler so it stays open until the
	// test tears down the server (which unblocks it). Using a channel
	// instead of select{} lets httptest.Server.Close return cleanly once
	// StopAll has given up waiting.
	release := make(chan struct{})
	sock, ts := unixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			<-release
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		close(release)
		ts.Close()
	})

	s := newSidecarAt(sock)
	s.Name = "hanger"
	r.Register("hanger", s)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	r.StopAll(ctx, nil)
	elapsed := time.Since(start)
	// StopAll must return close to the ctx deadline — not wait on the
	// individual Sidecar.Stop ladder. Allow generous slack for macOS CI.
	if elapsed > 3*time.Second {
		t.Errorf("StopAll took %v, expected to return near ctx deadline (~200ms)", elapsed)
	}
}

func TestSidecarRegistry_StopAllEmptyRegistryIsNoOp(t *testing.T) {
	r := NewSidecarRegistry()
	r.StopAll(context.Background(), func(name string, err error) {
		t.Errorf("log called with (%q, %v) on empty registry", name, err)
	})
}

func TestPanicError_FormatsValue(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{"boom", "panic in Sidecar.Stop: boom"},
		{errors.New("kaboom"), "panic in Sidecar.Stop: kaboom"},
		{42, "panic in Sidecar.Stop: non-string panic value"},
	}
	for _, c := range cases {
		pe := &panicError{v: c.v}
		if got := pe.Error(); got != c.want {
			t.Errorf("Error() for %v = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestDefaultLauncher_LaunchFailsOnMissingBinary(t *testing.T) {
	// Exercise defaultLauncher.Launch without spawning a real process by
	// pointing it at a non-existent binary; NewSidecar resolves the path
	// under addonDir/bin so cmd.Start fails with "no such file".
	dl := defaultLauncher{}
	dir := t.TempDir()
	_, err := dl.Launch(context.Background(), "x", dir, SidecarSpec{Binary: "nope-does-not-exist"})
	if err == nil {
		t.Fatal("expected Launch to fail on missing binary")
	}
}

func TestSidecarRegistry_NilReceiverSafe(t *testing.T) {
	var r *SidecarRegistry
	// None of these should panic on nil receivers.
	r.Register("x", nil)
	r.Unregister("x")
	if got := r.Lookup("x"); got != nil {
		t.Errorf("nil Lookup = %v", got)
	}
	if got := r.List(); got != nil {
		t.Errorf("nil List = %v", got)
	}
	r.StopAll(context.Background(), nil)
}
