package hookpolicy

import "github.com/sipeed/picoclaw/pkg/hooks"

type Diagnostics struct {
	Warnings []string
}

type EventPolicy struct {
	Enabled       bool
	Verbosity     string
	CaptureFields []string
	Instructions  []string
}

type Policy struct {
	Enabled       bool
	Events        map[hooks.Event]EventPolicy
	RedactionKeys []string
	AuditEnabled  bool
	AuditPath     string
}

func defaultPolicy() Policy {
	events := map[hooks.Event]EventPolicy{}
	for _, ev := range hooks.KnownEvents() {
		events[ev] = EventPolicy{Enabled: true, Verbosity: "medium"}
	}
	return Policy{
		Enabled:       true,
		Events:        events,
		RedactionKeys: []string{"api_key", "token", "secret", "authorization", "password"},
		AuditEnabled:  true,
		AuditPath:     "hooks/hook-events.jsonl",
	}
}
