package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestNormalizePhiEvalPreset_DefaultsBlankToBalanced(t *testing.T) {
	if got := normalizePhiEvalPreset(""); got != "balanced" {
		t.Fatalf("preset=%q", got)
	}
	if got := normalizePhiEvalPreset("  "); got != "balanced" {
		t.Fatalf("preset=%q", got)
	}
}

func TestNormalizePhiEvalPreset_PreservesKnownPreset(t *testing.T) {
	if got := normalizePhiEvalPreset("quality"); got != "quality" {
		t.Fatalf("preset=%q", got)
	}
	if got := normalizePhiEvalPreset("speed"); got != "speed" {
		t.Fatalf("preset=%q", got)
	}
}

type scriptedPhiEvalProvider struct {
	responses []*providers.LLMResponse
	errs      []error
	callIndex int
}

func (p *scriptedPhiEvalProvider) Chat(_ context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	idx := p.callIndex
	p.callIndex++
	if idx < len(p.errs) && p.errs[idx] != nil {
		return nil, p.errs[idx]
	}
	if idx >= len(p.responses) {
		return &providers.LLMResponse{}, nil
	}
	return p.responses[idx], nil
}

func TestRunPhiTextEval_PassesOnExpectedToken(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		responses: []*providers.LLMResponse{{
			Content:      "READY_LOCAL_TEST",
			FinishReason: "stop",
			Diagnostics: &providers.ResponseDiagnostics{
				ContentSource: "thinking",
			},
		}},
	}

	result := runPhiTextEval(provider, "qwen3.5:9b", 2*time.Second)
	if !result.Passed {
		t.Fatalf("expected pass, got %+v", result)
	}
	if result.Diagnostics == nil || result.Diagnostics.ContentSource != "thinking" {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
}

func TestRunPhiJSONEval_FailsOnInvalidJSON(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		responses: []*providers.LLMResponse{{
			Content:      "```json\n{\"status\":\"ok\"}\n```",
			FinishReason: "stop",
		}},
	}

	result := runPhiJSONEval(provider, "qwen3.5:9b", 2*time.Second)
	if result.Passed {
		t.Fatalf("expected failure, got %+v", result)
	}
	if result.FailureCode != "invalid_json" {
		t.Fatalf("failureCode=%q want invalid_json", result.FailureCode)
	}
	if result.Note == "" {
		t.Fatalf("expected parse note, got %+v", result)
	}
}

func TestRunPhiExtractEval_PassesOnStructuredPayload(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		responses: []*providers.LLMResponse{{
			Content:      `{"patient":"Ada Lovelace","dob":"1815-12-10","priority":"urgent","task":"prior auth for MRI lumbar spine"}`,
			FinishReason: "stop",
			Diagnostics: &providers.ResponseDiagnostics{
				ContentSource: "reasoning",
			},
		}},
	}

	result := runPhiExtractEval(provider, "qwen3.5:9b", 2*time.Second)
	if !result.Passed {
		t.Fatalf("expected pass, got %+v", result)
	}
	if !result.FallbackUsed {
		t.Fatalf("expected fallbackUsed, got %+v", result)
	}
}

func TestRunPhiToolEval_PassesWithRecoveredToolCall(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		responses: []*providers.LLMResponse{
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{{
					ID:   "call_1",
					Name: "read_local_note",
					Arguments: map[string]interface{}{
						"path": "./patient-note.txt",
					},
				}},
				Diagnostics: &providers.ResponseDiagnostics{
					ToolCallSource: "thinking",
				},
			},
			{
				Content:      `{"patient":"Ada Lovelace","priority":"urgent","task":"prior auth for MRI lumbar spine"}`,
				FinishReason: "stop",
			},
		},
	}

	result := runPhiToolEval(provider, "qwen3.5:9b", 2*time.Second)
	if !result.Passed {
		t.Fatalf("expected pass, got %+v", result)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("toolCalls=%d want 1", result.ToolCalls)
	}
	if !result.FallbackUsed {
		t.Fatalf("expected fallbackUsed, got %+v", result)
	}
	if result.Diagnostics == nil || result.Diagnostics.ToolCallSource != "thinking" {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
}

func TestRunPhiToolEval_ReportsProviderError(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		errs: []error{errors.New("timeout")},
	}

	result := runPhiToolEval(provider, "qwen3.5:9b", 2*time.Second)
	if result.Passed {
		t.Fatalf("expected failure, got %+v", result)
	}
	if result.FailureCode != "provider_error" {
		t.Fatalf("failureCode=%q want provider_error", result.FailureCode)
	}
	if result.Note != "timeout" {
		t.Fatalf("note=%q want timeout", result.Note)
	}
}

type slowTwoTurnPhiEvalProvider struct {
	callIndex int
}

func (p *slowTwoTurnPhiEvalProvider) Chat(ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	p.callIndex++
	switch p.callIndex {
	case 1:
		time.Sleep(80 * time.Millisecond)
		return &providers.LLMResponse{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_1",
				Name: "read_local_note",
				Arguments: map[string]interface{}{
					"path": "patient-note.txt",
				},
			}},
		}, nil
	case 2:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &providers.LLMResponse{
			Content:      `{"patient":"Ada Lovelace","priority":"urgent","task":"prior auth for MRI lumbar spine"}`,
			FinishReason: "stop",
		}, nil
	default:
		return &providers.LLMResponse{}, nil
	}
}

func TestRunPhiToolEval_UsesFreshTimeoutForFollowupCall(t *testing.T) {
	provider := &slowTwoTurnPhiEvalProvider{}
	result := runPhiToolEval(provider, "qwen3.5:9b", 50*time.Millisecond)
	if !result.Passed {
		t.Fatalf("expected pass with fresh follow-up timeout, got %+v", result)
	}
	if provider.callIndex != 2 {
		t.Fatalf("callIndex=%d want 2", provider.callIndex)
	}
}

func TestRunPhiToolEval_FailsOnUnexpectedToolPath(t *testing.T) {
	provider := &scriptedPhiEvalProvider{
		responses: []*providers.LLMResponse{{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_1",
				Name: "read_local_note",
				Arguments: map[string]interface{}{
					"path": "wrong-note.txt",
				},
			}},
		}},
	}

	result := runPhiToolEval(provider, "qwen3.5:9b", 2*time.Second)
	if result.Passed {
		t.Fatalf("expected failure, got %+v", result)
	}
	if result.FailureCode != "tool_args_mismatch" {
		t.Fatalf("failureCode=%q want tool_args_mismatch", result.FailureCode)
	}
}
