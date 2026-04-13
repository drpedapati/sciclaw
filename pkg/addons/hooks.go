package addons

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// Dispatcher fans out events to every enabled addon subscribed to them.
// Errors do not propagate: core operations must not be blocked by addon
// failures, so Fire logs outcomes and returns once every goroutine has
// either responded or timed out.
type Dispatcher struct {
	Store       *Store
	SciclawHome string
	Lookup      func(name string) *Sidecar
	Timeout     time.Duration
	Log         func(name, event string, err error)
}

// Fire dispatches event to every enabled addon whose manifest lists it in
// provides.hooks. Subscribers run in parallel with a per-addon timeout; a
// panicking handler is recovered so one addon cannot take down the caller.
func (d *Dispatcher) Fire(ctx context.Context, event string, payload any) {
	if d.Store == nil || d.Lookup == nil {
		return
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	registry, err := d.Store.Load()
	if err != nil {
		d.log("", event, fmt.Errorf("loading registry: %w", err))
		return
	}
	if len(registry.Addons) == 0 {
		return
	}

	var wg sync.WaitGroup
	for name, entry := range registry.Addons {
		if entry == nil || entry.State != StateEnabled {
			continue
		}
		manifest, mErr := d.loadManifest(name)
		if mErr != nil {
			d.log(name, event, fmt.Errorf("loading manifest: %w", mErr))
			continue
		}
		if !subscribes(manifest, event) {
			continue
		}
		side := d.Lookup(name)
		if side == nil {
			// Addon is enabled in the registry but no running sidecar — log
			// and move on; core may be in the middle of a restart.
			d.log(name, event, fmt.Errorf("no running sidecar"))
			continue
		}

		wg.Add(1)
		go func(name string, side *Sidecar) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					d.log(name, event, fmt.Errorf("panic in hook handler: %v", r))
				}
			}()
			hookCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if err := side.Hook(hookCtx, event, payload); err != nil {
				d.log(name, event, err)
				return
			}
			d.log(name, event, nil)
		}(name, side)
	}
	wg.Wait()
}

func (d *Dispatcher) log(name, event string, err error) {
	if d.Log == nil {
		return
	}
	d.Log(name, event, err)
}

func (d *Dispatcher) loadManifest(name string) (*Manifest, error) {
	path := filepath.Join(d.SciclawHome, "addons", name, "addon.json")
	return ParseManifest(path)
}

func subscribes(m *Manifest, event string) bool {
	if m == nil {
		return false
	}
	for _, h := range m.Provides.Hooks {
		if h == event {
			return true
		}
	}
	return false
}
