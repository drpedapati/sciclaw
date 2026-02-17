package providers

// Smoke tests to validate SDK reasoning effort types before production integration.
// These tests verify that the SDK types compile, serialize correctly, and produce
// the expected JSON payloads for both OpenAI and Anthropic reasoning effort parameters.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// --- OpenAI / Codex reasoning effort ---

func TestCodexReasoningEffortTypes(t *testing.T) {
	// Verify the SDK constants exist and have the expected string values
	tests := []struct {
		name   string
		effort shared.ReasoningEffort
		want   string
	}{
		{"none", shared.ReasoningEffortNone, "none"},
		{"minimal", shared.ReasoningEffortMinimal, "minimal"},
		{"low", shared.ReasoningEffortLow, "low"},
		{"medium", shared.ReasoningEffortMedium, "medium"},
		{"high", shared.ReasoningEffortHigh, "high"},
		{"xhigh", shared.ReasoningEffortXhigh, "xhigh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.effort) != tt.want {
				t.Errorf("ReasoningEffort %s = %q, want %q", tt.name, tt.effort, tt.want)
			}
		})
	}
}

func TestCodexReasoningParamSerialization(t *testing.T) {
	// Build a minimal ResponseNewParams with reasoning effort and verify JSON output
	params := responses.ResponseNewParams{
		Model: "o3",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.Opt("Hello"),
		},
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffortHigh,
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal ResponseNewParams with reasoning: %v", err)
	}

	jsonStr := string(data)
	t.Logf("Serialized params: %s", jsonStr)

	// Verify reasoning.effort appears in the JSON
	if !strings.Contains(jsonStr, `"reasoning"`) {
		t.Error("JSON missing 'reasoning' key")
	}
	if !strings.Contains(jsonStr, `"effort"`) {
		t.Error("JSON missing 'effort' key inside reasoning")
	}
	if !strings.Contains(jsonStr, `"high"`) {
		t.Error("JSON missing effort value 'high'")
	}

	// Parse back and verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	reasoning, ok := parsed["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatal("'reasoning' is not an object in JSON output")
	}
	if effort, ok := reasoning["effort"].(string); !ok || effort != "high" {
		t.Errorf("reasoning.effort = %v, want 'high'", reasoning["effort"])
	}
}

func TestCodexReasoningEffortXhigh(t *testing.T) {
	// Specifically test xhigh — the user's primary concern for codex models
	params := responses.ResponseNewParams{
		Model: "codex-mini-latest",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.Opt("Explain ALS"),
		},
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffortXhigh,
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal with xhigh: %v", err)
	}

	jsonStr := string(data)
	t.Logf("xhigh params: %s", jsonStr)

	if !strings.Contains(jsonStr, `"xhigh"`) {
		t.Error("JSON missing effort value 'xhigh'")
	}
}

func TestCodexReasoningFromStringCast(t *testing.T) {
	// Test that casting a raw string to ReasoningEffort works
	// (this is how we'll set it from the options map / CLI flag)
	effortStr := "high"
	params := responses.ResponseNewParams{
		Model: "o3",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.Opt("test"),
		},
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffort(effortStr),
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal with string cast: %v", err)
	}

	if !strings.Contains(string(data), `"high"`) {
		t.Error("String cast to ReasoningEffort failed to serialize")
	}
}

func TestCodexNoReasoningWhenEmpty(t *testing.T) {
	// Verify that when reasoning is zero-value, it's omitted from JSON
	params := responses.ResponseNewParams{
		Model: "gpt-4o",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.Opt("Hello"),
		},
		// No Reasoning set — should be omitted
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal without reasoning: %v", err)
	}

	jsonStr := string(data)
	t.Logf("No-reasoning params: %s", jsonStr)

	if strings.Contains(jsonStr, `"reasoning"`) {
		t.Error("JSON should NOT contain 'reasoning' when not set (omitzero)")
	}
}

// --- Anthropic / Claude reasoning effort ---

func TestClaudeEffortTypes(t *testing.T) {
	// Verify the SDK constants exist
	tests := []struct {
		name   string
		effort anthropic.OutputConfigEffort
		want   string
	}{
		{"low", anthropic.OutputConfigEffortLow, "low"},
		{"medium", anthropic.OutputConfigEffortMedium, "medium"},
		{"high", anthropic.OutputConfigEffortHigh, "high"},
		{"max", anthropic.OutputConfigEffortMax, "max"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.effort) != tt.want {
				t.Errorf("OutputConfigEffort %s = %q, want %q", tt.name, tt.effort, tt.want)
			}
		})
	}
}

func TestClaudeAdaptiveThinkingSerialization(t *testing.T) {
	// Build MessageNewParams with adaptive thinking + effort
	params := anthropic.MessageNewParams{
		Model:     "claude-opus-4-6",
		MaxTokens: 16384,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Hello")),
		},
		Thinking: anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortHigh,
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal Claude params with thinking: %v", err)
	}

	jsonStr := string(data)
	t.Logf("Claude thinking params: %s", jsonStr)

	// Verify thinking block appears
	if !strings.Contains(jsonStr, `"thinking"`) {
		t.Error("JSON missing 'thinking' key")
	}
	if !strings.Contains(jsonStr, `"adaptive"`) {
		t.Error("JSON missing thinking type 'adaptive'")
	}

	// Verify output_config.effort appears
	if !strings.Contains(jsonStr, `"output_config"`) {
		t.Error("JSON missing 'output_config' key")
	}
	if !strings.Contains(jsonStr, `"high"`) {
		t.Error("JSON missing effort value 'high'")
	}
}

func TestClaudeEffortFromStringCast(t *testing.T) {
	// Test that casting a raw string works for Anthropic too
	effortStr := "max"
	effort := anthropic.OutputConfigEffort(effortStr)

	params := anthropic.MessageNewParams{
		Model:     "claude-opus-4-6",
		MaxTokens: 16384,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("test")),
		},
		Thinking: anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: effort,
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal with string cast: %v", err)
	}

	if !strings.Contains(string(data), `"max"`) {
		t.Error("String cast to OutputConfigEffort failed to serialize")
	}
}

func TestClaudeNoThinkingWhenEmpty(t *testing.T) {
	// Verify zero-value thinking is omitted (current behavior, backward compat)
	params := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Hello")),
		},
		// No Thinking, no OutputConfig
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal without thinking: %v", err)
	}

	jsonStr := string(data)
	t.Logf("No-thinking params: %s", jsonStr)

	if strings.Contains(jsonStr, `"thinking"`) {
		t.Error("JSON should NOT contain 'thinking' when not set")
	}
	if strings.Contains(jsonStr, `"output_config"`) {
		t.Error("JSON should NOT contain 'output_config' when not set")
	}
}

// --- Integration pattern: options map extraction ---

func TestOptionsMapReasoningExtraction(t *testing.T) {
	// Test the exact pattern we'll use in buildCodexParams / buildClaudeParams
	// to extract reasoning_effort from the options map
	options := map[string]interface{}{
		"max_tokens":       8192,
		"temperature":      0.7,
		"reasoning_effort": "high",
	}

	// Extract like we would in the provider
	if effort, ok := options["reasoning_effort"].(string); ok && effort != "" {
		if effort != "high" {
			t.Errorf("extracted effort = %q, want 'high'", effort)
		}
	} else {
		t.Error("failed to extract reasoning_effort from options map")
	}

	// Test empty/missing case
	emptyOptions := map[string]interface{}{
		"max_tokens":  8192,
		"temperature": 0.7,
	}
	if effort, ok := emptyOptions["reasoning_effort"].(string); ok && effort != "" {
		t.Error("should not extract effort from empty options")
	}

	// Test explicit empty string
	emptyEffort := map[string]interface{}{
		"reasoning_effort": "",
	}
	if effort, ok := emptyEffort["reasoning_effort"].(string); ok && effort != "" {
		t.Error("should not treat empty string as valid effort")
	}
}
