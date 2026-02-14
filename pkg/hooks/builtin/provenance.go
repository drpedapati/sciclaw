package builtin

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/hooks"
)

// ProvenanceHandler records normalized event metadata for reproducibility.
type ProvenanceHandler struct{}

func (h *ProvenanceHandler) Name() string {
	return "provenance"
}

func (h *ProvenanceHandler) Handle(_ context.Context, ev hooks.Event, data hooks.Context) hooks.Result {
	meta := map[string]any{
		"event":       string(ev),
		"turn_id":     data.TurnID,
		"session_key": data.SessionKey,
	}
	if data.ToolName != "" {
		meta["tool"] = data.ToolName
	}
	if data.Metadata != nil {
		meta["event_metadata"] = data.Metadata
	}
	return hooks.Result{
		Status:   hooks.StatusOK,
		Message:  "provenance captured",
		Metadata: meta,
	}
}
