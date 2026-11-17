package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recordingMCPCaller struct {
	sender string
	args   map[string]any
	result *mcp.CallToolResult
	err    error
}

func (c *recordingMCPCaller) CallTool(_ context.Context, _, _, senderKey string, args map[string]any) (*mcp.CallToolResult, error) {
	c.sender = senderKey
	c.args = args
	return c.result, c.err
}

func TestMCPToolName(t *testing.T) {
	tests := []struct {
		server, tool, want string
	}{
		{"atomicqms", "issue_read", "mcp_atomicqms_issue_read"},
		{"AtomicQMS", "get_me", "mcp_atomicqms_get_me"},
	}
	for _, tt := range tests {
		got := NewMCPTool(nil, tt.server, &mcp.Tool{Name: tt.tool}).Name()
		if got != tt.want {
			t.Errorf("Name(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}

	// Lossy sanitizing appends a hash so distinct originals stay distinct.
	a := NewMCPTool(nil, "srv", &mcp.Tool{Name: "a.b"}).Name()
	b := NewMCPTool(nil, "srv", &mcp.Tool{Name: "a/b"}).Name()
	if a == b || !strings.HasPrefix(a, "mcp_srv_a_b_") {
		t.Errorf("lossy names = %q, %q", a, b)
	}
	long := NewMCPTool(nil, "server", &mcp.Tool{Name: strings.Repeat("x", 80)}).Name()
	if len(long) > 64 {
		t.Errorf("long name has %d chars", len(long))
	}
}

func TestMCPToolExecuteActsAsSender(t *testing.T) {
	caller := &recordingMCPCaller{result: &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "issue 1"}}}}
	tool := NewMCPTool(caller, "atomicqms", &mcp.Tool{Name: "issue_read"})

	ctx := WithSenderKey(context.Background(), "discord:1001")
	result := tool.Execute(ctx, map[string]interface{}{"issue_number": 1})
	if result.IsError || result.ForLLM != "issue 1" {
		t.Fatalf("result = %+v", result)
	}
	if caller.sender != "discord:1001" || caller.args["issue_number"] != 1 {
		t.Errorf("call = sender %q args %v", caller.sender, caller.args)
	}
}

func TestMCPToolExecuteNotConnected(t *testing.T) {
	caller := &recordingMCPCaller{err: ErrMCPNotConnected}
	tool := NewMCPTool(caller, "atomicqms", &mcp.Tool{Name: "issue_read"})
	tool.NotConnectedMessage = "connect first"

	result := tool.Execute(context.Background(), nil)
	if !result.IsError || result.ForLLM != "connect first" || !errors.Is(result.Err, ErrMCPNotConnected) {
		t.Fatalf("result = %+v", result)
	}
	if caller.sender != "" {
		t.Errorf("sender = %q, want none", caller.sender)
	}
}

func TestMCPToolExecuteServerError(t *testing.T) {
	caller := &recordingMCPCaller{result: &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "not found"}}}}
	result := NewMCPTool(caller, "atomicqms", &mcp.Tool{Name: "issue_read"}).Execute(context.Background(), nil)
	if !result.IsError || !strings.Contains(result.ForLLM, "not found") {
		t.Fatalf("result = %+v", result)
	}
}

func TestMCPToolParametersFromSchema(t *testing.T) {
	tool := NewMCPTool(nil, "s", &mcp.Tool{Name: "t", InputSchema: map[string]any{
		"type":       "object",
		"properties": map[string]any{"owner": map[string]any{"type": "string"}},
	}})
	params := tool.Parameters()
	if params["type"] != "object" || params["properties"].(map[string]any)["owner"] == nil {
		t.Fatalf("parameters = %v", params)
	}
	if NewMCPTool(nil, "s", &mcp.Tool{Name: "t"}).Parameters()["type"] != "object" {
		t.Fatal("nil schema did not default to an object")
	}
}
