package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/phi"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type phiEvalChatClient interface {
	Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error)
}

type phiEvalResult struct {
	Name          string                         `json:"name"`
	Passed        bool                           `json:"passed"`
	DurationMS    int64                          `json:"duration_ms"`
	FinishReason  string                         `json:"finish_reason,omitempty"`
	ResponseChars int                            `json:"response_chars,omitempty"`
	ToolCalls     int                            `json:"tool_calls,omitempty"`
	FallbackUsed  bool                           `json:"fallback_used,omitempty"`
	FailureCode   string                         `json:"failure_code,omitempty"`
	Note          string                         `json:"note,omitempty"`
	Diagnostics   *providers.ResponseDiagnostics `json:"diagnostics,omitempty"`
}

func modesPhiEvalCmd(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("modes phi-eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	timeoutSec := fs.Int("timeout", 120, "per model-call timeout in seconds")
	if err := fs.Parse(args); err != nil {
		fmt.Printf("Usage: %s modes phi-eval [--json] [--timeout <seconds>]\n", invokedCLIName())
		os.Exit(1)
	}

	results, backend, model, err := runPhiEval(cfg, time.Duration(*timeoutSec)*time.Second)
	if err != nil {
		fmt.Printf("PHI eval failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		payload := map[string]any{
			"backend": backend,
			"model":   model,
			"results": results,
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Println("PHI local eval")
	fmt.Printf("Backend: %s\n", backend)
	fmt.Printf("Model:   %s\n", model)
	fmt.Println()

	allPassed := true
	for _, result := range results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
			allPassed = false
		}
		fmt.Printf("[%s] %-10s %4dms  finish=%s", status, result.Name, result.DurationMS, blankIfEmpty(result.FinishReason, "unknown"))
		if result.Diagnostics != nil {
			if src := strings.TrimSpace(result.Diagnostics.ContentSource); src != "" {
				fmt.Printf("  content=%s", src)
			}
			if src := strings.TrimSpace(result.Diagnostics.ToolCallSource); src != "" {
				fmt.Printf("  tools=%s", src)
			}
		}
		if result.ToolCalls > 0 {
			fmt.Printf("  tool_calls=%d", result.ToolCalls)
		}
		if result.FallbackUsed {
			fmt.Printf("  fallback=yes")
		}
		if code := strings.TrimSpace(result.FailureCode); code != "" {
			fmt.Printf("  code=%s", code)
		}
		if strings.TrimSpace(result.Note) != "" {
			fmt.Printf("  %s", result.Note)
		}
		fmt.Println()
	}

	if !allPassed {
		os.Exit(1)
	}
}

func runPhiEval(cfg *config.Config, timeout time.Duration) ([]phiEvalResult, string, string, error) {
	backend := strings.TrimSpace(cfg.Agents.Defaults.LocalBackend)
	model := strings.TrimSpace(cfg.Agents.Defaults.LocalModel)
	if backend == "" || model == "" {
		return nil, "", "", fmt.Errorf("local PHI runtime is not configured; run: %s modes phi-setup", invokedCLIName())
	}
	if backend == config.BackendMLX {
		return nil, backend, model, fmt.Errorf("MLX local backend is not supported in this build yet; use Ollama")
	}

	status := phi.CheckBackend(backend)
	if !status.Installed {
		return nil, backend, model, fmt.Errorf("%s is not installed", backend)
	}
	if !status.Running {
		return nil, backend, model, fmt.Errorf("%s is not running", backend)
	}
	if backend == config.BackendOllama && !phi.CheckModelReady(model) {
		return nil, backend, model, fmt.Errorf("local model %q is not ready", model)
	}

	cfgCopy := *cfg
	cfgCopy.Agents = cfg.Agents
	cfgCopy.Agents.Defaults.Mode = config.ModePhi
	client, err := providers.CreateProvider(&cfgCopy)
	if err != nil {
		return nil, backend, model, err
	}

	results := make([]phiEvalResult, 0, 4)
	results = append(results, runPhiTextEval(client, model, timeout))
	results = append(results, runPhiJSONEval(client, model, timeout))
	results = append(results, runPhiExtractEval(client, model, timeout))
	results = append(results, runPhiToolEval(client, model, timeout))
	return results, backend, model, nil
}

func runPhiTextEval(client phiEvalChatClient, model string, timeout time.Duration) phiEvalResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.Chat(ctx, []providers.Message{{
		Role:    "user",
		Content: "Reply with exactly READY_LOCAL_TEST and nothing else.",
	}}, nil, model, map[string]interface{}{"max_tokens": 128, "temperature": 0.1})

	result := phiEvalResult{Name: "text", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		result.FailureCode = "provider_error"
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.FallbackUsed = evalUsedFallback(resp.Diagnostics)
	result.Passed = strings.Contains(strings.TrimSpace(resp.Content), "READY_LOCAL_TEST")
	if !result.Passed {
		result.FailureCode = "unexpected_response"
		result.Note = fmt.Sprintf("unexpected response: %q", shortenEvalPreview(resp.Content))
	}
	return result
}

func runPhiJSONEval(client phiEvalChatClient, model string, timeout time.Duration) phiEvalResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.Chat(ctx, []providers.Message{{
		Role:    "user",
		Content: `Return only compact JSON with exactly this shape: {"status":"ok","n":7}`,
	}}, nil, model, map[string]interface{}{"max_tokens": 192, "temperature": 0.1})

	result := phiEvalResult{Name: "json", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		result.FailureCode = "provider_error"
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.FallbackUsed = evalUsedFallback(resp.Diagnostics)

	var payload struct {
		Status string `json:"status"`
		N      int    `json:"n"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &payload); err != nil {
		result.FailureCode = "invalid_json"
		result.Note = fmt.Sprintf("invalid JSON: %v", err)
		return result
	}
	result.Passed = payload.Status == "ok" && payload.N == 7
	if !result.Passed {
		result.FailureCode = "unexpected_payload"
		result.Note = fmt.Sprintf("unexpected JSON payload: %q", shortenEvalPreview(resp.Content))
	}
	return result
}

func runPhiExtractEval(client phiEvalChatClient, model string, timeout time.Duration) phiEvalResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.Chat(ctx, []providers.Message{{
		Role: "user",
		Content: `Extract the key fields from this short clinical note and return only compact JSON with exactly this shape: {"patient":"...","dob":"...","priority":"...","task":"..."}.

Clinical note:
Patient: Ada Lovelace
DOB: 1815-12-10
Priority: urgent
Task: prior auth for MRI lumbar spine`,
	}}, nil, model, map[string]interface{}{"max_tokens": 256, "temperature": 0.1})

	result := phiEvalResult{Name: "extract", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		result.FailureCode = "provider_error"
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.FallbackUsed = evalUsedFallback(resp.Diagnostics)

	var payload struct {
		Patient  string `json:"patient"`
		DOB      string `json:"dob"`
		Priority string `json:"priority"`
		Task     string `json:"task"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &payload); err != nil {
		result.FailureCode = "invalid_json"
		result.Note = fmt.Sprintf("invalid JSON: %v", err)
		return result
	}

	result.Passed = strings.TrimSpace(payload.Patient) == "Ada Lovelace" &&
		strings.TrimSpace(payload.DOB) == "1815-12-10" &&
		strings.EqualFold(strings.TrimSpace(payload.Priority), "urgent") &&
		strings.TrimSpace(payload.Task) == "prior auth for MRI lumbar spine"
	if !result.Passed {
		result.FailureCode = "unexpected_payload"
		result.Note = fmt.Sprintf("unexpected extraction payload: %q", shortenEvalPreview(resp.Content))
	}
	return result
}

func runPhiToolEval(client phiEvalChatClient, model string, timeout time.Duration) phiEvalResult {
	toolDef := providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "read_local_note",
			Description: "Read a short local note by path and return the plain text contents.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"path"},
			},
		},
	}
	initialMessages := []providers.Message{{
		Role: "user",
		Content: `Use the read_local_note tool on the exact path patient-note.txt. After the tool result, return only compact JSON with exactly this shape: {"patient":"...","priority":"...","task":"..."}.
Do not include any explanation.`,
	}}

	start := time.Now()
	firstCtx, firstCancel := context.WithTimeout(context.Background(), timeout)
	resp, err := client.Chat(firstCtx, initialMessages, []providers.ToolDefinition{toolDef}, model, map[string]interface{}{"max_tokens": 256, "temperature": 0.1})
	firstCancel()
	result := phiEvalResult{Name: "tool", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		result.FailureCode = "provider_error"
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.FallbackUsed = evalUsedFallback(resp.Diagnostics)
	result.ToolCalls = len(resp.ToolCalls)
	if len(resp.ToolCalls) == 0 {
		result.FailureCode = "tool_call_missing"
		result.Note = "model did not issue a tool call"
		return result
	}
	call := resp.ToolCalls[0]
	if call.Name != "read_local_note" {
		result.FailureCode = "tool_name_mismatch"
		result.Note = fmt.Sprintf("unexpected tool: %s", call.Name)
		return result
	}

	pathArg, _ := call.Arguments["path"].(string)
	if !matchesEvalFixturePath(pathArg, "patient-note.txt") {
		result.FailureCode = "tool_args_mismatch"
		result.Note = fmt.Sprintf("unexpected tool path: %q", pathArg)
		return result
	}
	assistantMsg := providers.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	}
	toolPayload := "Patient: Ada Lovelace\nDOB: 1815-12-10\nPriority: urgent\nTask: prior auth for MRI lumbar spine\n"
	toolMsg := providers.Message{
		Role:       "tool",
		Content:    toolPayload,
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}

	followStart := time.Now()
	secondCtx, secondCancel := context.WithTimeout(context.Background(), timeout)
	finalResp, err := client.Chat(secondCtx, append(initialMessages, assistantMsg, toolMsg), []providers.ToolDefinition{toolDef}, model, map[string]interface{}{"max_tokens": 128, "temperature": 0.1})
	secondCancel()
	result.DurationMS += time.Since(followStart).Milliseconds()
	if err != nil {
		result.FailureCode = "followup_failed"
		result.Note = fmt.Sprintf("follow-up failed: %v", err)
		return result
	}
	result.FallbackUsed = result.FallbackUsed || evalUsedFallback(finalResp.Diagnostics)

	var payload struct {
		Patient  string `json:"patient"`
		Priority string `json:"priority"`
		Task     string `json:"task"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(finalResp.Content)), &payload); err != nil {
		result.FailureCode = "invalid_json"
		result.Note = fmt.Sprintf("invalid final JSON: %v", err)
		return result
	}

	result.Passed = strings.TrimSpace(payload.Patient) == "Ada Lovelace" &&
		strings.EqualFold(strings.TrimSpace(payload.Priority), "urgent") &&
		strings.TrimSpace(payload.Task) == "prior auth for MRI lumbar spine"
	if !result.Passed {
		result.FailureCode = "unexpected_payload"
		result.Note = fmt.Sprintf("unexpected final answer: %q", shortenEvalPreview(finalResp.Content))
	}
	return result
}

func evalUsedFallback(diag *providers.ResponseDiagnostics) bool {
	if diag == nil {
		return false
	}
	contentSource := strings.TrimSpace(strings.ToLower(diag.ContentSource))
	toolSource := strings.TrimSpace(strings.ToLower(diag.ToolCallSource))
	if contentSource != "" && contentSource != "content" {
		return true
	}
	if toolSource != "" && toolSource != "native" && toolSource != "content" {
		return true
	}
	return false
}

func matchesEvalFixturePath(raw, expected string) bool {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return false
	}
	return filepath.Base(raw) == expected
}

func shortenEvalPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) <= 80 {
		return trimmed
	}
	return trimmed[:77] + "..."
}

func blankIfEmpty(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}
