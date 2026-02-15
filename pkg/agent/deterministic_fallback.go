package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (al *AgentLoop) tryDeterministicFallback(ctx context.Context, opts processOptions) (string, bool) {
	if !isIRLProjectListIntent(opts.UserMessage) {
		return "", false
	}

	result := al.tools.ExecuteWithContext(
		ctx,
		"irl_project",
		map[string]interface{}{"operation": "discover_projects"},
		opts.Channel,
		opts.ChatID,
		nil,
	)
	if result == nil {
		return "I tried to list IRL projects but the tool returned no result.", true
	}
	if result.IsError {
		return fmt.Sprintf("I couldn't list IRL projects: %s", compactLine(result.ForLLM)), true
	}

	reply, err := formatIRLDiscoverProjects(result.ForLLM)
	if err != nil {
		return fmt.Sprintf("I listed IRL projects, but couldn't parse the result: %v", err), true
	}
	return reply, true
}

func isIRLProjectListIntent(message string) bool {
	normalized := compactLine(strings.ToLower(message))
	switch normalized {
	case "list irl",
		"list projects",
		"list project",
		"show projects",
		"show project",
		"projects",
		"irl projects",
		"what projects do i have",
		"what irl projects do i have":
		return true
	default:
		return false
	}
}

func compactLine(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

type irlDiscoverPayload struct {
	Status string `json:"status"`
	Data   struct {
		Projects []map[string]interface{} `json:"projects"`
	} `json:"data"`
}

func formatIRLDiscoverProjects(raw string) (string, error) {
	var payload irlDiscoverPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}

	projects := payload.Data.Projects
	if len(projects) == 0 {
		return "No IRL-managed projects found.", nil
	}

	lines := make([]string, 0, len(projects)+1)
	lines = append(lines, fmt.Sprintf("Found %d IRL-managed project(s):", len(projects)))
	for _, p := range projects {
		name := stringifyMapField(p, "name", "project", "slug", "id")
		path := stringifyMapField(p, "path", "project_path", "dir", "directory")
		template := stringifyMapField(p, "template", "template_name")

		var line string
		if name != "" && path != "" {
			line = fmt.Sprintf("- %s: %s", name, path)
		} else if name != "" {
			line = fmt.Sprintf("- %s", name)
		} else if path != "" {
			line = fmt.Sprintf("- %s", path)
		} else {
			line = "- (unlabeled project)"
		}
		if template != "" {
			line += fmt.Sprintf(" (template: %s)", template)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), nil
}

func stringifyMapField(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				return trimmed
			}
		case fmt.Stringer:
			if trimmed := strings.TrimSpace(t.String()); trimmed != "" {
				return trimmed
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", t))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
