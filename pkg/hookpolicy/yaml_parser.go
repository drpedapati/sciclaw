package hookpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/hooks"
	"gopkg.in/yaml.v3"
)

type rawYAMLPolicy struct {
	Enabled *bool `yaml:"enabled"`
	Events  map[string]struct {
		Enabled       *bool    `yaml:"enabled"`
		Verbosity     string   `yaml:"verbosity"`
		CaptureFields []string `yaml:"capture_fields"`
		Instructions  []string `yaml:"instructions"`
	} `yaml:"events"`
	Redaction struct {
		Keys []string `yaml:"keys"`
	} `yaml:"redaction"`
	Audit struct {
		Enabled *bool  `yaml:"enabled"`
		Path    string `yaml:"path"`
	} `yaml:"audit"`
}

func parseYAMLOverrides(workspace string, policy *Policy, diag *Diagnostics) error {
	path := filepath.Join(workspace, "hooks.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var raw rawYAMLPolicy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse hooks.yaml: %w", err)
	}

	if raw.Enabled != nil {
		policy.Enabled = *raw.Enabled
	}
	if len(raw.Redaction.Keys) > 0 {
		policy.RedactionKeys = append([]string(nil), raw.Redaction.Keys...)
	}
	if raw.Audit.Enabled != nil {
		policy.AuditEnabled = *raw.Audit.Enabled
	}
	if raw.Audit.Path != "" {
		policy.AuditPath = raw.Audit.Path
	}

	for key, cfg := range raw.Events {
		ev, ok := normalizeEvent(key)
		if !ok {
			diag.Warnings = append(diag.Warnings, "hooks.yaml unknown event: "+key)
			continue
		}
		ep := policy.Events[ev]
		if cfg.Enabled != nil {
			ep.Enabled = *cfg.Enabled
		}
		if cfg.Verbosity != "" {
			ep.Verbosity = cfg.Verbosity
		}
		if len(cfg.CaptureFields) > 0 {
			ep.CaptureFields = append([]string(nil), cfg.CaptureFields...)
		}
		if len(cfg.Instructions) > 0 {
			ep.Instructions = append([]string(nil), cfg.Instructions...)
		}
		policy.Events[ev] = ep
	}

	return nil
}

func normalizeEvent(input string) (hooks.Event, bool) {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == ' ':
			b.WriteByte('_')
		}
	}
	norm := b.String()
	for strings.Contains(norm, "__") {
		norm = strings.ReplaceAll(norm, "__", "_")
	}
	norm = strings.Trim(norm, "_")

	switch norm {
	case "before_turn":
		return hooks.EventBeforeTurn, true
	case "after_turn":
		return hooks.EventAfterTurn, true
	case "before_llm":
		return hooks.EventBeforeLLM, true
	case "after_llm":
		return hooks.EventAfterLLM, true
	case "before_tool":
		return hooks.EventBeforeTool, true
	case "after_tool":
		return hooks.EventAfterTool, true
	case "on_error", "error":
		return hooks.EventOnError, true
	default:
		return "", false
	}
}
