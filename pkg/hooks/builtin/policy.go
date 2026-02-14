package builtin

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/hookpolicy"
	"github.com/sipeed/picoclaw/pkg/hooks"
)

// PolicyHandler applies workspace hook policy (HOOKS.md + hooks.yaml).
type PolicyHandler struct {
	workspace string
}

func NewPolicyHandler(workspace string) *PolicyHandler {
	return &PolicyHandler{workspace: workspace}
}

func (h *PolicyHandler) Name() string {
	return "policy"
}

func (h *PolicyHandler) Handle(_ context.Context, ev hooks.Event, data hooks.Context) hooks.Result {
	policy, diag, err := hookpolicy.LoadPolicy(h.workspace)
	if err != nil {
		return hooks.Result{
			Status:  hooks.StatusError,
			Message: "failed to load hook policy",
			Err:     err,
		}
	}

	meta := map[string]any{
		"policy_enabled": policy.Enabled,
		"turn_id":        data.TurnID,
	}
	if len(diag.Warnings) > 0 {
		meta["warnings"] = diag.Warnings
	}

	if !policy.Enabled {
		return hooks.Result{Status: hooks.StatusOK, Message: "hooks disabled by policy", Metadata: meta}
	}

	eventPolicy, ok := policy.Events[ev]
	if !ok {
		return hooks.Result{Status: hooks.StatusOK, Message: "event not configured", Metadata: meta}
	}
	meta["event_enabled"] = eventPolicy.Enabled
	meta["verbosity"] = eventPolicy.Verbosity
	if len(eventPolicy.CaptureFields) > 0 {
		meta["capture_fields"] = eventPolicy.CaptureFields
	}
	if len(eventPolicy.Instructions) > 0 {
		meta["instructions"] = eventPolicy.Instructions
	}

	if !eventPolicy.Enabled {
		return hooks.Result{Status: hooks.StatusOK, Message: "event disabled by policy", Metadata: meta}
	}

	message := "policy evaluated"
	if len(eventPolicy.Instructions) > 0 {
		message = eventPolicy.Instructions[0]
	}

	return hooks.Result{Status: hooks.StatusOK, Message: message, Metadata: meta}
}
