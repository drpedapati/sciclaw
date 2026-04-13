package main

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/addons"
)

// addonDispatcher is the process-wide addon hook dispatcher.
//
// It is nil until SetAddonDispatcher is called from main() at startup, and may
// remain nil in contexts where the addon system has not been initialized
// (e.g. unit tests, one-shot CLI subcommands). All Fire* helpers below are
// safe to call with a nil dispatcher — they no-op silently so emission sites
// in hot paths do not need to guard the call themselves.
//
// The dispatcher is set once during startup and read from multiple goroutines
// without a mutex. This is safe in practice because SetAddonDispatcher is
// called before any goroutine that could observe it, and the pointer itself
// is never mutated after startup. If future code needs to replace the
// dispatcher at runtime, add a sync.RWMutex around this variable.
var addonDispatcher *addons.Dispatcher

// SetAddonDispatcher installs the process-wide dispatcher. Called once from
// main() after the addon store has been constructed. Passing nil is a valid
// way to tear the dispatcher down in tests.
func SetAddonDispatcher(d *addons.Dispatcher) {
	addonDispatcher = d
}

// fireAddonHook is the safe entrypoint for emitting an addon hook event from
// core code. It is a no-op if the dispatcher has not been initialized.
//
// Uses a bounded 10s context so that a misbehaving addon sidecar cannot block
// the caller's hot path indefinitely. The dispatcher itself also applies its
// own per-addon timeout (5s default) on top of this.
//
// Hook emission is fire-and-forget: errors are logged by the dispatcher and
// never propagated back to the caller.
func fireAddonHook(event string, payload any) {
	if addonDispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addonDispatcher.Fire(ctx, event, payload)
}

// RoutingChangedPayload is the payload for the "routing_changed" event.
//
// Rules is typed as `any` rather than []config.RoutingMapping so callers at
// emission sites do not need to import pkg/config; the payload is serialized
// to JSON by the sidecar transport and addons receive it as a generic object.
type RoutingChangedPayload struct {
	Rules any `json:"rules"`
}
