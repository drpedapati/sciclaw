package builtin

import (
	"context"
	"errors"

	"github.com/sipeed/picoclaw/pkg/hookpolicy"
	"github.com/sipeed/picoclaw/pkg/hooks"
)

// PolicyHandler applies workspace hook policy (HOOKS.md + hooks.yaml).
type PolicyHandler struct {
	policy   hookpolicy.Policy
	warnings []string
	loadErr  error
}

func NewPolicyHandler(policy hookpolicy.Policy, diag hookpolicy.Diagnostics, loadErr error) *PolicyHandler {
	warnings := make([]string, 0, len(diag.Warnings))
	warnings = append(warnings, diag.Warnings...)
	return &PolicyHandler{
		policy:   policy,
		warnings: warnings,
		loadErr:  loadErr,
	}
}

func (h *PolicyHandler) Name() string {
	return "policy"
}

func (h *PolicyHandler) Handle(_ context.Context, ev hooks.Event, data hooks.Context) hooks.Result {
	if h.loadErr != nil {
		return hooks.Result{
			Status:  hooks.StatusError,
			Message: "failed to load hook policy",
			Err:     h.loadErr,
		}
	}
	if h.policy.Events == nil {
		return hooks.Result{
			Status:  hooks.StatusError,
			Message: "hook policy missing event configuration",
			Err:     errors.New("hook policy events are not initialized"),
		}
	}

	meta := map[string]any{
		"policy_enabled": h.policy.Enabled,
		"turn_id":        data.TurnID,
	}
	if len(h.warnings) > 0 {
		meta["warnings"] = h.warnings
	}

	if !h.policy.Enabled {
		return hooks.Result{Status: hooks.StatusOK, Message: "hooks disabled by policy", Metadata: meta}
	}

	eventPolicy, ok := h.policy.Events[ev]
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
