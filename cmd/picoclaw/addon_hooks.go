package main

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/addons"
	"github.com/sipeed/picoclaw/pkg/config"
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

// addonHookParentCtx is the long-lived gateway context fireAddonHook derives
// its per-call timeouts from. When the gateway shutdown cancels this ctx,
// every in-flight hook delivery short-circuits immediately instead of
// running out its full 10s timeout — which matters because the shutdown
// path calls fireAddonHook several times (channels.StopAll, profile
// flushes, cron teardown) and serial 10s timeouts would stack into
// minutes of shutdown lag.
//
// Set alongside addonDispatcher via SetAddonDispatcher. Falls back to
// context.Background() when nil so unit tests and CLI one-shots still work.
var addonHookParentCtx context.Context

// addonSidecarRegistry is the process-wide live sidecar registry. Wave 4a
// added this so the Lifecycle knows where to register newly spawned
// sidecars and so Dispatcher.Lookup can resolve a name to a live process.
//
// Like addonDispatcher it is nil until main() initializes it. All CLI
// subcommands that construct a Lifecycle thread it in so enable/disable/
// upgrade/uninstall actually manage processes.
//
// The registry is kept as a package-level variable (rather than a
// function injected into addon_cmd.go) because the web backend in Wave 4b
// will need to reach the same instance from HTTP handlers, and a package
// var is the smallest seam that both CLI and web share.
var addonSidecarRegistry *addons.SidecarRegistry

// SetAddonDispatcher installs the process-wide dispatcher. Called once from
// main() after the addon store has been constructed. Passing nil is a valid
// way to tear the dispatcher down in tests.
//
// parentCtx should be the gateway's shutdown ctx so in-flight hook
// deliveries abort immediately when shutdown is requested, rather than
// holding the shutdown sequence hostage to their 10s per-call timeout.
// nil parentCtx falls back to context.Background().
func SetAddonDispatcher(d *addons.Dispatcher, parentCtx context.Context) {
	addonDispatcher = d
	addonHookParentCtx = parentCtx
}

// fireAddonHook is the safe entrypoint for emitting an addon hook event from
// core code. It is a no-op if the dispatcher has not been initialized.
//
// Uses a bounded 10s context derived from the gateway shutdown ctx so that
// a misbehaving addon sidecar cannot block the caller's hot path AND
// cannot keep the gateway from shutting down by stalling hook deliveries
// through the entire timeout window. The dispatcher itself also applies
// its own per-addon timeout (5s default) on top of this.
//
// Hook emission is fire-and-forget: errors are logged by the dispatcher and
// never propagated back to the caller.
func fireAddonHook(event string, payload any) {
	if addonDispatcher == nil {
		return
	}
	parent := addonHookParentCtx
	if parent == nil {
		parent = context.Background()
	}
	// If the parent is already cancelled, don't even bother dispatching —
	// every addon handler would immediately return ctx.Err() anyway, and
	// the dispatcher's WaitGroup still holds us for one scheduling round.
	if err := parent.Err(); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	addonDispatcher.Fire(ctx, event, payload)
}

// RoutingChangedPayload is the payload for the "routing_changed" event.
//
// Rules is a projection of config.RoutingMapping that excludes fields
// addons don't need: allowed_senders (per-chat ACL), local_backend,
// local_model, local_preset, and any other runtime knobs. Leaking those
// to every enabled addon would expose the guest list of every routed
// chat and reveal which local model each channel prefers.
type RoutingChangedPayload struct {
	Rules []RoutingChangedRule `json:"rules"`
}

// RoutingChangedRule is the addon-visible projection of a routing mapping.
// Only channel, chat_id, workspace, label, and mode are surfaced. The
// allowed_senders list and local_* runtime knobs are deliberately omitted
// so addons cannot exfiltrate per-chat ACLs or model preferences.
type RoutingChangedRule struct {
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	Workspace string `json:"workspace"`
	Label     string `json:"label,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// projectRoutingMappings converts the internal config.RoutingMapping slice
// into the addon-safe projection that Dispatcher.Fire will serialise as
// JSON. Any field not listed in RoutingChangedRule is dropped.
func projectRoutingMappings(in []config.RoutingMapping) []RoutingChangedRule {
	out := make([]RoutingChangedRule, 0, len(in))
	for _, m := range in {
		out = append(out, RoutingChangedRule{
			Channel:   m.Channel,
			ChatID:    m.ChatID,
			Workspace: m.Workspace,
			Label:     m.Label,
			Mode:      m.Mode,
		})
	}
	return out
}
