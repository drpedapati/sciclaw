package hookpolicy

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/hooks"
)

func parseNaturalLanguagePolicy(workspace string, policy *Policy, diag *Diagnostics) error {
	path := filepath.Join(workspace, "HOOKS.md")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	current := hooks.Event("")
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "##") {
			head := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			if ev, ok := normalizeEvent(head); ok {
				current = ev
			} else {
				current = ""
			}
			continue
		}

		if current == "" {
			if ev, ok := inferEventFromSentence(line); ok {
				current = ev
			}
		}

		if current == "" {
			continue
		}

		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			instruction := strings.TrimSpace(strings.TrimLeft(line, "-*"))
			ep := policy.Events[current]
			ep.Instructions = append(ep.Instructions, instruction)
			policy.Events[current] = ep
		}
	}
	if err := s.Err(); err != nil {
		return err
	}

	return nil
}

func inferEventFromSentence(line string) (hooks.Event, bool) {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "before turn"):
		return hooks.EventBeforeTurn, true
	case strings.Contains(lower, "after turn"):
		return hooks.EventAfterTurn, true
	case strings.Contains(lower, "before llm"):
		return hooks.EventBeforeLLM, true
	case strings.Contains(lower, "after llm"):
		return hooks.EventAfterLLM, true
	case strings.Contains(lower, "before tool"):
		return hooks.EventBeforeTool, true
	case strings.Contains(lower, "after tool"):
		return hooks.EventAfterTool, true
	case strings.Contains(lower, "on error") || strings.Contains(lower, "error"):
		return hooks.EventOnError, true
	default:
		return "", false
	}
}
