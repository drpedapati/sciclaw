package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/transport"
)

type CodexProvider struct {
	client      *openai.Client
	accountID   string
	tokenSource func() (string, string, error)
}

func NewCodexProvider(token, accountID string) *CodexProvider {
	opts := []option.RequestOption{
		option.WithBaseURL("https://chatgpt.com/backend-api/codex"),
		option.WithAPIKey(token),
		option.WithHTTPClient(transport.NewCloudflareClient()),
	}
	if accountID != "" {
		opts = append(opts, option.WithHeader("Chatgpt-Account-Id", accountID))
	}
	client := openai.NewClient(opts...)
	return &CodexProvider{
		client:    &client,
		accountID: accountID,
	}
}

func NewCodexProviderWithTokenSource(token, accountID string, tokenSource func() (string, string, error)) *CodexProvider {
	p := NewCodexProvider(token, accountID)
	p.tokenSource = tokenSource
	return p
}

func (p *CodexProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	var opts []option.RequestOption
	if p.tokenSource != nil {
		tok, accID, err := p.tokenSource()
		if err != nil {
			return nil, fmt.Errorf("refreshing token: %w", err)
		}
		opts = append(opts, option.WithAPIKey(tok))
		if accID != "" {
			opts = append(opts, option.WithHeader("Chatgpt-Account-Id", accID))
		}
	}

	params := buildCodexParams(messages, tools, model, options)

	// Set reasoning effort if provided (critical for o-series and codex models).
	if effort, ok := options["reasoning_effort"].(string); ok && effort != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort: shared.ReasoningEffort(effort),
		}
	}

	stream := p.client.Responses.NewStreaming(ctx, params, opts...)
	if stream == nil {
		return nil, fmt.Errorf("codex API call: empty stream")
	}
	defer stream.Close()

	var finalResp *responses.Response
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseCompletedEvent:
			resp := event.Response
			finalResp = &resp
		case responses.ResponseIncompleteEvent:
			// Keep the incomplete response payload so callers can map finish reason.
			resp := event.Response
			finalResp = &resp
		case responses.ResponseErrorEvent:
			if event.Param != "" {
				return nil, fmt.Errorf("response error (%s): %s (param=%s)", event.Code, event.Message, event.Param)
			}
			return nil, fmt.Errorf("response error (%s): %s", event.Code, event.Message)
		case responses.ResponseFailedEvent:
			return nil, fmt.Errorf("response failed with status %q", event.Response.Status)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("codex API call: %w", err)
	}
	if finalResp == nil {
		return nil, fmt.Errorf("codex API call: stream ended without a response payload")
	}

	return parseCodexResponse(finalResp), nil
}

func (p *CodexProvider) GetDefaultModel() string {
	return "gpt-5.2"
}

func buildCodexParams(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) responses.ResponseNewParams {
	var inputItems responses.ResponseInputParam
	var instructions string

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			instructions = msg.Content
		case "user":
			if msg.ToolCallID != "" {
				inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
					OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
						CallID: msg.ToolCallID,
						Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: openai.Opt(msg.Content)},
					},
				})
			} else {
				inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
					OfMessage: &responses.EasyInputMessageParam{
						Role:    responses.EasyInputMessageRoleUser,
						Content: responses.EasyInputMessageContentUnionParam{OfString: openai.Opt(msg.Content)},
					},
				})
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				if msg.Content != "" {
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
						OfMessage: &responses.EasyInputMessageParam{
							Role:    responses.EasyInputMessageRoleAssistant,
							Content: responses.EasyInputMessageContentUnionParam{OfString: openai.Opt(msg.Content)},
						},
					})
				}
				for _, tc := range msg.ToolCalls {
					name := resolveToolCallName(tc)
					if name == "" {
						continue
					}
					argsJSON := resolveToolCallArguments(tc)
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
						OfFunctionCall: &responses.ResponseFunctionToolCallParam{
							CallID:    tc.ID,
							Name:      name,
							Arguments: argsJSON,
						},
					})
				}
			} else {
				inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
					OfMessage: &responses.EasyInputMessageParam{
						Role:    responses.EasyInputMessageRoleAssistant,
						Content: responses.EasyInputMessageContentUnionParam{OfString: openai.Opt(msg.Content)},
					},
				})
			}
		case "tool":
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: msg.ToolCallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: openai.Opt(msg.Content)},
				},
			})
		}
	}

	params := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
		Store: openai.Opt(false),
	}

	if instructions != "" {
		params.Instructions = openai.Opt(instructions)
	}
	// Codex backend currently rejects temperature/max_output_tokens for chatgpt.com OAuth calls.
	// Keep these unset to avoid 400 "Unsupported parameter" errors.

	if len(tools) > 0 {
		params.Tools = translateToolsForCodex(tools)
	}

	return params
}

func resolveToolCallName(tc ToolCall) string {
	if tc.Name != "" {
		return tc.Name
	}
	if tc.Function != nil {
		return tc.Function.Name
	}
	return ""
}

func resolveToolCallArguments(tc ToolCall) string {
	if len(tc.Arguments) > 0 {
		argsJSON, _ := json.Marshal(tc.Arguments)
		return string(argsJSON)
	}
	if tc.Function != nil && tc.Function.Arguments != "" {
		return tc.Function.Arguments
	}
	return "{}"
}

func translateToolsForCodex(tools []ToolDefinition) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		ft := responses.FunctionToolParam{
			Name:       t.Function.Name,
			Parameters: t.Function.Parameters,
			Strict:     openai.Opt(false),
		}
		if t.Function.Description != "" {
			ft.Description = openai.Opt(t.Function.Description)
		}
		result = append(result, responses.ToolUnionParam{OfFunction: &ft})
	}
	return result
}

func parseCodexResponse(resp *responses.Response) *LLMResponse {
	var content strings.Builder
	var toolCalls []ToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" {
					content.WriteString(c.Text)
				}
			}
		case "function_call":
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": item.Arguments}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: args,
			})
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if resp.Status == "incomplete" {
		finishReason = "length"
	}

	var usage *UsageInfo
	if resp.Usage.TotalTokens > 0 {
		usage = &UsageInfo{
			PromptTokens:     int(resp.Usage.InputTokens),
			CompletionTokens: int(resp.Usage.OutputTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		}
	}

	return &LLMResponse{
		Content:      content.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}
}

func createCodexTokenSource() func() (string, string, error) {
	return func() (string, string, error) {
		cred, err := auth.GetCredential("openai")
		if err != nil {
			return "", "", fmt.Errorf("loading auth credentials: %w", err)
		}
		if cred == nil {
			return "", "", fmt.Errorf("no credentials for openai. Run: picoclaw auth login --provider openai")
		}

		if cred.AuthMethod == "oauth" && cred.NeedsRefresh() && cred.RefreshToken != "" {
			oauthCfg := auth.OpenAIOAuthConfig()
			refreshed, err := auth.RefreshAccessToken(cred, oauthCfg)
			if err != nil {
				return "", "", fmt.Errorf("refreshing token: %w", err)
			}
			if err := auth.SetCredential("openai", refreshed); err != nil {
				return "", "", fmt.Errorf("saving refreshed token: %w", err)
			}
			return refreshed.AccessToken, refreshed.AccountID, nil
		}

		return cred.AccessToken, cred.AccountID, nil
	}
}
