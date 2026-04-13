package addons

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hookEnv seeds a fake ~/sciclaw with two addons (alpha, beta) installed and
// enabled, each with an addon.json declaring a single hook subscription. It
// returns the home directory plus the two manifest hooks names the caller
// asked for.
func hookEnv(t *testing.T, alphaHooks, betaHooks []string) (home string, store *Store) {
	t.Helper()
	home = t.TempDir()
	store = NewStore(home)

	write := func(name string, hooks []string) {
		dir := filepath.Join(home, "addons", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		hooksJSON := "[]"
		if len(hooks) > 0 {
			quoted := make([]string, len(hooks))
			for i, h := range hooks {
				quoted[i] = `"` + h + `"`
			}
			hooksJSON = "[" + strings.Join(quoted, ",") + "]"
		}
		body := fmt.Sprintf(`{
  "name": %q,
  "version": "0.1.0",
  "requires": {"sciclaw": ">=0.1.0"},
  "sidecar": {"binary": "sc"},
  "provides": {"hooks": %s}
}`, name, hooksJSON)
		if err := os.WriteFile(filepath.Join(dir, "addon.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := store.Set(name, &RegistryEntry{
			Version:         "0.1.0",
			InstalledAt:     "2026-04-13T14:22:00Z",
			InstalledCommit: "deadbeef",
			State:           StateEnabled,
			Source:          "local",
		}); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha", alphaHooks)
	write("beta", betaHooks)
	return home, store
}

// fakeSidecarOn creates a Sidecar wired to a local unix-socket httptest.Server
// and returns both for test assertions.
func fakeSidecarOn(t *testing.T, handler http.Handler) (*Sidecar, *httptest.Server) {
	sock, ts := unixTestServer(t, handler)
	s := newSidecarAt(sock)
	return s, ts
}

func TestDispatcher_FireFansOutToSubscribers(t *testing.T) {
	home, store := hookEnv(t, []string{"routing_changed"}, []string{"routing_changed"})

	var alphaHit, betaHit int32
	alpha, alphaTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&alphaHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer alphaTS.Close()
	beta, betaTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&betaHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer betaTS.Close()

	var logMu sync.Mutex
	var logged []string
	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup: func(name string) *Sidecar {
			switch name {
			case "alpha":
				return alpha
			case "beta":
				return beta
			}
			return nil
		},
		Timeout: 2 * time.Second,
		Log: func(name, event string, err error) {
			logMu.Lock()
			defer logMu.Unlock()
			if err != nil {
				logged = append(logged, fmt.Sprintf("%s %s err=%v", name, event, err))
			} else {
				logged = append(logged, fmt.Sprintf("%s %s ok", name, event))
			}
		},
	}

	d.Fire(context.Background(), "routing_changed", map[string]any{"rules": []any{}})

	if atomic.LoadInt32(&alphaHit) != 1 {
		t.Errorf("alpha hit = %d, want 1", alphaHit)
	}
	if atomic.LoadInt32(&betaHit) != 1 {
		t.Errorf("beta hit = %d, want 1", betaHit)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if len(logged) != 2 {
		t.Errorf("logged = %v", logged)
	}
}

func TestDispatcher_FireSkipsUnsubscribed(t *testing.T) {
	home, store := hookEnv(t, []string{"routing_changed"}, []string{"user_added"})

	var alphaHit, betaHit int32
	alpha, alphaTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&alphaHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer alphaTS.Close()
	beta, betaTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&betaHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer betaTS.Close()

	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup: func(name string) *Sidecar {
			if name == "alpha" {
				return alpha
			}
			return beta
		},
		Timeout: 2 * time.Second,
	}

	d.Fire(context.Background(), "routing_changed", nil)

	if atomic.LoadInt32(&alphaHit) != 1 {
		t.Errorf("alpha hit = %d, want 1", alphaHit)
	}
	if atomic.LoadInt32(&betaHit) != 0 {
		t.Errorf("beta hit = %d, want 0", betaHit)
	}
}

func TestDispatcher_FireTimeoutDoesNotBlockOthers(t *testing.T) {
	home, store := hookEnv(t, []string{"routing_changed"}, []string{"routing_changed"})

	slow, slowTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowTS.Close()
	var fastHit int32
	fast, fastTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fastHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fastTS.Close()

	var logMu sync.Mutex
	var slowErr error
	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup: func(name string) *Sidecar {
			if name == "alpha" {
				return slow
			}
			return fast
		},
		Timeout: 200 * time.Millisecond,
		Log: func(name, event string, err error) {
			if name == "alpha" && err != nil {
				logMu.Lock()
				slowErr = err
				logMu.Unlock()
			}
		},
	}

	done := make(chan struct{})
	go func() {
		d.Fire(context.Background(), "routing_changed", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Fire blocked past slow addon timeout")
	}

	if atomic.LoadInt32(&fastHit) != 1 {
		t.Errorf("fast hit = %d, want 1", fastHit)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if slowErr == nil {
		t.Error("expected slow addon to have logged an error")
	}
}

func TestDispatcher_PanicInSidecarIsRecovered(t *testing.T) {
	home, store := hookEnv(t, []string{"routing_changed"}, []string{"routing_changed"})

	// beta's sidecar lookup returns a Sidecar whose Hook call is routed to a
	// handler that panics. Use a custom http.Handler that panics; the Go
	// net/http server recovers per-connection panics, so the client will see
	// a broken pipe error. Instead we make Lookup return nil for beta so we
	// can directly exercise the "nil sidecar" skip path, and trigger the
	// panic path with a synthetic Sidecar whose transport fails hard.
	alphaPanicSidecar := &Sidecar{Name: "alpha"}
	alphaPanicSidecar.client = &http.Client{
		Transport: panicTransport{},
	}

	var betaHit int32
	beta, betaTS := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&betaHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer betaTS.Close()

	var logMu sync.Mutex
	var logs []string
	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup: func(name string) *Sidecar {
			if name == "alpha" {
				return alphaPanicSidecar
			}
			return beta
		},
		Timeout: 2 * time.Second,
		Log: func(name, event string, err error) {
			logMu.Lock()
			defer logMu.Unlock()
			logs = append(logs, fmt.Sprintf("%s %v", name, err))
		},
	}
	d.Fire(context.Background(), "routing_changed", nil)

	if atomic.LoadInt32(&betaHit) != 1 {
		t.Errorf("beta hit = %d, want 1", betaHit)
	}
	logMu.Lock()
	defer logMu.Unlock()
	var sawAlphaPanic bool
	for _, l := range logs {
		if strings.HasPrefix(l, "alpha ") && (strings.Contains(l, "panic") || strings.Contains(l, "boom")) {
			sawAlphaPanic = true
		}
	}
	if !sawAlphaPanic {
		t.Errorf("expected alpha panic log, got %v", logs)
	}
}

func TestDispatcher_FireOnEmptyRegistry(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup:      func(string) *Sidecar { return nil },
	}
	// Should not panic or hang.
	d.Fire(context.Background(), "routing_changed", nil)
}

func TestDispatcher_FireSkipsDisabledAddons(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)

	dir := filepath.Join(home, "addons", "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "name": "alpha",
  "version": "0.1.0",
  "requires": {"sciclaw": ">=0.1.0"},
  "sidecar": {"binary": "sc"},
  "provides": {"hooks": ["routing_changed"]}
}`
	if err := os.WriteFile(filepath.Join(dir, "addon.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("alpha", &RegistryEntry{State: StateInstalled}); err != nil {
		t.Fatal(err)
	}

	var hit int32
	side, ts := fakeSidecarOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := &Dispatcher{
		Store:       store,
		SciclawHome: home,
		Lookup:      func(string) *Sidecar { return side },
	}
	d.Fire(context.Background(), "routing_changed", nil)
	if atomic.LoadInt32(&hit) != 0 {
		t.Errorf("disabled addon was called, hit = %d", hit)
	}
}

// panicTransport is an http.RoundTripper that panics, exercising the panic
// recovery path inside Dispatcher.Fire without relying on a real process.
type panicTransport struct{}

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("boom")
}
