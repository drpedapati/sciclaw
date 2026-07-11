package agent

import (
	"fmt"
	"strings"
)

// TurnDirectives are optional leading model/effort hints stripped from the
// user message before the LLM sees it.
type TurnDirectives struct {
	Model         string
	Effort        string
	Body          string
	HadDirectives bool
}

var modelAliases = map[string]string{
	"luna":   "gpt-5.6-luna",
	"sol":    "gpt-5.6-sol",
	"terra":  "gpt-5.6-terra",
	"sonnet": "claude-sonnet-4.6",
	"opus":   "claude-opus-4-6",
	"haiku":  "claude-haiku-4-5-20251001",
}

var allowedEfforts = map[string]bool{
	"low":     true,
	"medium":  true,
	"high":    true,
	"xhigh":   true,
	"max":     true,
	"ultra":   true,
	"minimal": true,
	"none":    true,
}

// ParseTurnDirectives extracts leading model:/effort: lines (or a combined
// first line) from content. The remainder is returned as Body.
func ParseTurnDirectives(content string) TurnDirectives {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return TurnDirectives{Body: content}
	}

	lines := strings.Split(content, "\n")
	var model, effort string
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if model != "" || effort != "" {
				i++
				continue
			}
			break
		}

		lower := strings.ToLower(line)
		parsedAny := false

		// Combined first-line form: "model: luna effort: high"
		if m, rest, ok := cutDirective(lower, line, "model:"); ok {
			model = m
			parsedAny = true
			if e, _, ok := cutDirective(strings.ToLower(rest), rest, "effort:"); ok {
				effort = e
			} else if e, _, ok := cutDirective(strings.ToLower(rest), rest, "reasoning:"); ok {
				effort = e
			} else if e, _, ok := cutDirective(strings.ToLower(rest), rest, "reasoning_effort:"); ok {
				effort = e
			}
			i++
			continue
		}
		if e, _, ok := cutDirective(lower, line, "effort:"); ok {
			effort = e
			parsedAny = true
			i++
			continue
		}
		if e, _, ok := cutDirective(lower, line, "reasoning:"); ok {
			effort = e
			parsedAny = true
			i++
			continue
		}
		if e, _, ok := cutDirective(lower, line, "reasoning_effort:"); ok {
			effort = e
			parsedAny = true
			i++
			continue
		}
		if !parsedAny {
			break
		}
	}

	if model == "" && effort == "" {
		return TurnDirectives{Body: content}
	}

	body := strings.TrimSpace(strings.Join(lines[i:], "\n"))
	return TurnDirectives{
		Model:         strings.TrimSpace(model),
		Effort:        strings.TrimSpace(effort),
		Body:          body,
		HadDirectives: true,
	}
}

func cutDirective(lowerLine, originalLine, prefix string) (value, rest string, ok bool) {
	if !strings.HasPrefix(lowerLine, prefix) {
		return "", "", false
	}
	// Preserve original casing for the value by slicing originalLine.
	raw := strings.TrimSpace(originalLine[len(prefix):])
	if raw == "" {
		return "", "", true
	}
	// Split off another directive on the same line.
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", "", true
	}
	valParts := []string{fields[0]}
	restStart := 1
	for restStart < len(fields) {
		f := strings.ToLower(fields[restStart])
		if strings.HasPrefix(f, "model:") || strings.HasPrefix(f, "effort:") ||
			strings.HasPrefix(f, "reasoning:") || strings.HasPrefix(f, "reasoning_effort:") {
			break
		}
		valParts = append(valParts, fields[restStart])
		restStart++
	}
	value = strings.Join(valParts, " ")
	if restStart < len(fields) {
		rest = strings.Join(fields[restStart:], " ")
	}
	return value, rest, true
}

// ResolveTurnOverrides maps aliases and enforces same-provider-family scope
// relative to the workspace default model. effort-only overrides are always OK.
func ResolveTurnOverrides(defaultModel, requestedModel, requestedEffort string) (model, effort string, err error) {
	model = strings.TrimSpace(defaultModel)
	if requestedModel != "" {
		resolved := expandModelAlias(requestedModel)
		defFam := modelProviderFamily(defaultModel)
		reqFam := modelProviderFamily(resolved)
		if defFam == "" || reqFam == "" {
			return "", "", fmt.Errorf("could not resolve provider family for model override %q", requestedModel)
		}
		if defFam != reqFam {
			return "", "", fmt.Errorf(
				"model %q is a %s model, but this workspace is running %s (%s). Same-provider overrides only — try a %s model",
				requestedModel, reqFam, defaultModel, defFam, defFam,
			)
		}
		model = resolved
	}

	if requestedEffort != "" {
		e := strings.ToLower(strings.TrimSpace(requestedEffort))
		if !allowedEfforts[e] {
			return "", "", fmt.Errorf("unknown effort %q (want low|medium|high|xhigh|max|ultra|minimal|none)", requestedEffort)
		}
		effort = e
	}
	return model, effort, nil
}

func expandModelAlias(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "openai/")
	s = strings.TrimPrefix(s, "anthropic/")
	if alias, ok := modelAliases[strings.ToLower(s)]; ok {
		return alias
	}
	return s
}

// modelProviderFamily is a cfg-free heuristic aligned with models.ResolveProvider.
func modelProviderFamily(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	if lower == "" {
		return ""
	}
	if strings.Contains(lower, "claude") || strings.HasPrefix(lower, "anthropic/") {
		return "anthropic"
	}
	if strings.Contains(lower, "gpt") || strings.Contains(lower, "o1") || strings.Contains(lower, "o3") ||
		strings.Contains(lower, "o4") || strings.Contains(lower, "codex") || strings.HasPrefix(lower, "openai/") {
		return "openai"
	}
	if strings.Contains(lower, "gemini") || strings.HasPrefix(lower, "google/") {
		return "gemini"
	}
	if strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu") {
		return "zhipu"
	}
	if strings.Contains(lower, "groq") || strings.HasPrefix(lower, "groq/") {
		return "groq"
	}
	if strings.Contains(lower, "deepseek") {
		return "deepseek"
	}
	if strings.HasPrefix(lower, "openrouter/") || strings.HasPrefix(lower, "meta-llama/") {
		return "openrouter"
	}
	return ""
}
