package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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

	results := make([]phiEvalResult, 0, 3)
	results = append(results, runPhiTextEval(client, model, timeout))
	results = append(results, runPhiJSONEval(client, model, timeout))
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
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.Passed = strings.Contains(strings.TrimSpace(resp.Content), "READY_LOCAL_TEST")
	if !result.Passed {
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
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics

	var payload struct {
		Status string `json:"status"`
		N      int    `json:"n"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &payload); err != nil {
		result.Note = fmt.Sprintf("invalid JSON: %v", err)
		return result
	}
	result.Passed = payload.Status == "ok" && payload.N == 7
	if !result.Passed {
		result.Note = fmt.Sprintf("unexpected JSON payload: %q", shortenEvalPreview(resp.Content))
	}
	return result
}

func runPhiToolEval(client phiEvalChatClient, model string, timeout time.Duration) phiEvalResult {
	toolDef := providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "word_count",
			Description: "Count the number of words in a piece of text and return {\"count\": number}.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"text"},
			},
		},
	}
	initialMessages := []providers.Message{{
		Role:    "user",
		Content: "Use the word_count tool on the exact text 'alpha beta gamma delta'. After the tool result, answer with only the final count.",
	}}

	start := time.Now()
	firstCtx, firstCancel := context.WithTimeout(context.Background(), timeout)
	resp, err := client.Chat(firstCtx, initialMessages, []providers.ToolDefinition{toolDef}, model, map[string]interface{}{"max_tokens": 256, "temperature": 0.1})
	firstCancel()
	result := phiEvalResult{Name: "tool", DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		result.Note = err.Error()
		return result
	}
	result.FinishReason = resp.FinishReason
	result.ResponseChars = len(resp.Content)
	result.Diagnostics = resp.Diagnostics
	result.ToolCalls = len(resp.ToolCalls)
	if len(resp.ToolCalls) == 0 {
		result.Note = "model did not issue a tool call"
		return result
	}
	call := resp.ToolCalls[0]
	if call.Name != "word_count" {
		result.Note = fmt.Sprintf("unexpected tool: %s", call.Name)
		return result
	}

	textArg, _ := call.Arguments["text"].(string)
	count := len(strings.Fields(textArg))
	assistantMsg := providers.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	}
	toolPayload := fmt.Sprintf(`{"count":%d}`, count)
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
		result.Note = fmt.Sprintf("follow-up failed: %v", err)
		return result
	}
	result.Passed = strings.Contains(strings.TrimSpace(finalResp.Content), fmt.Sprintf("%d", count))
	if !result.Passed {
		result.Note = fmt.Sprintf("unexpected final answer: %q", shortenEvalPreview(finalResp.Content))
	}
	return result
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
