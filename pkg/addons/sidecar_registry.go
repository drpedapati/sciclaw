package addons

import (
	"context"
	"sort"
	"sync"
)

// SidecarRegistry tracks live sidecar processes across the lifetime of a
// sciclaw process. It is the single source of truth for "is addon X running
// right now?" — separate from the Store, which only tracks persistent state.
//
// The registry is goroutine-safe: concurrent Register/Unregister/Lookup from
// any number of callers is allowed. It does NOT own the Sidecar values —
// callers are responsible for calling Sidecar.Stop before Unregister (or use
// StopAll on shutdown). The registry just maps names to pointers.
type SidecarRegistry struct {
	mu       sync.RWMutex
	sidecars map[string]*Sidecar
}

// NewSidecarRegistry returns an empty registry ready for concurrent use.
func NewSidecarRegistry() *SidecarRegistry {
	return &SidecarRegistry{
		sidecars: make(map[string]*Sidecar),
	}
}

// Register stores a running sidecar under its name. Replaces any previous
// entry (caller is responsible for stopping the previous one first).
func (r *SidecarRegistry) Register(name string, s *Sidecar) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sidecars == nil {
		r.sidecars = make(map[string]*Sidecar)
	}
	r.sidecars[name] = s
}

// Unregister removes a name. Does NOT stop the sidecar — caller stops it.
// No-op when the name is not present.
func (r *SidecarRegistry) Unregister(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sidecars, name)
}

// Lookup returns the live sidecar for a name, or nil if not running.
// Intended as the Dispatcher.Lookup callback.
func (r *SidecarRegistry) Lookup(name string) *Sidecar {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sidecars[name]
}

// List returns a snapshot of currently running addon names (sorted).
func (r *SidecarRegistry) List() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sidecars))
	for n := range r.sidecars {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// StopAll stops every registered sidecar in parallel with a deadline.
// Used on sciclaw shutdown. Errors are logged via the provided callback but
// not returned. Every registered sidecar is cleared from the map regardless
// of whether its Stop call succeeded — a half-stopped process is not safer
// than a stopped one, and retaining the entry would just leak.
//
// The ctx deadline is respected as a hard upper bound; individual Sidecar.Stop
// calls receive the same context. If ctx is already cancelled, StopAll still
// attempts each Stop (they will fast-fail, which is fine — process teardown
// is the OS's problem at that point).
func (r *SidecarRegistry) StopAll(ctx context.Context, log func(name string, err error)) {
	if r == nil {
		return
	}
	// Snapshot under lock so concurrent Register/Unregister calls during
	// shutdown don't deadlock or race the iteration.
	r.mu.Lock()
	snapshot := make(map[string]*Sidecar, len(r.sidecars))
	for n, s := range r.sidecars {
		snapshot[n] = s
	}
	r.sidecars = make(map[string]*Sidecar)
	r.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}

	var wg sync.WaitGroup
	for name, side := range snapshot {
		wg.Add(1)
		go func(name string, side *Sidecar) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil && log != nil {
					// Convert panic to an error so callers see something
					// instead of silently losing the failure.
					log(name, &panicError{v: rec})
				}
			}()
			if side == nil {
				return
			}
			if err := side.Stop(ctx); err != nil && log != nil {
				log(name, err)
			}
		}(name, side)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// Deadline hit. Goroutines will eventually exit when their own Stop
		// timeouts fire; we do not block on them because the caller wanted a
		// bounded shutdown. Not a bug — each Sidecar.Stop has its own
		// SIGTERM/SIGKILL ladder that cleans up independently.
	}
}

// panicError wraps a recovered panic value as an error so StopAll's log
// callback receives a consistent error type.
type panicError struct {
	v any
}

func (p *panicError) Error() string {
	return "panic in Sidecar.Stop: " + sprintAny(p.v)
}

func sprintAny(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "non-string panic value"
}
