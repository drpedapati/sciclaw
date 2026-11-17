package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPCaller calls a tool on a named MCP server on behalf of senderKey.
type MCPCaller interface {
	CallTool(ctx context.Context, serverName, toolName, senderKey string, args map[string]any) (*mcp.CallToolResult, error)
}

// ErrMCPNotConnected is returned by an MCPCaller when the sender has no
// credential for the server. MCPTool turns it into NotConnectedMessage.
var ErrMCPNotConnected = errors.New("mcp: sender is not connected")

type senderKeyContextKey struct{}

// WithSenderKey records the "<channel>:<sender_id>" of the message being
// processed, so tools can act as that user.
func WithSenderKey(ctx context.Context, senderKey string) context.Context {
	return context.WithValue(ctx, senderKeyContextKey{}, senderKey)
}

// SenderKeyFromContext returns the sender key set by WithSenderKey.
func SenderKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(senderKeyContextKey{}).(string)
	return key
}

// MCPTool exposes one MCP server tool through the tool registry.
// Ported from sipeed/picoclaw pkg/tools/integration/mcp_tool.go.
type MCPTool struct {
	caller     MCPCaller
	serverName string
	tool       *mcp.Tool
	// NotConnectedMessage is returned to the model when the sender has no
	// credential for this server.
	NotConnectedMessage string
}

func NewMCPTool(caller MCPCaller, serverName string, tool *mcp.Tool) *MCPTool {
	return &MCPTool{caller: caller, serverName: serverName, tool: tool}
}

// sanitizeIdentifierComponent lowercases s and replaces characters outside
// [a-z0-9_-] with single underscores, so it is safe in provider tool names.
func sanitizeIdentifierComponent(s string) string {
	const maxLen = 64
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(s) {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !allowed {
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
			continue
		}
		prevUnderscore = false
		b.WriteRune(r)
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		result = "unnamed"
	}
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

// Name returns mcp_<server>_<tool>, capped at 64 characters. A hash of the
// original names is appended when sanitizing was lossy or the name was cut.
func (t *MCPTool) Name() string {
	server := sanitizeIdentifierComponent(t.serverName)
	tool := sanitizeIdentifierComponent(t.tool.Name)
	full := fmt.Sprintf("mcp_%s_%s", server, tool)
	lossless := strings.ToLower(t.serverName) == server && strings.ToLower(t.tool.Name) == tool
	const maxTotal = 64
	if lossless && len(full) <= maxTotal {
		return full
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.serverName + "\x00" + t.tool.Name))
	base := full
	if len(base) > maxTotal-9 {
		base = strings.TrimRight(full[:maxTotal-9], "_")
	}
	return fmt.Sprintf("%s_%08x", base, h.Sum32())
}

func (t *MCPTool) Description() string {
	desc := t.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool from %s server", t.serverName)
	}
	return fmt.Sprintf("[MCP:%s] %s", t.serverName, desc)
}

func (t *MCPTool) Parameters() map[string]interface{} {
	empty := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	if t.tool.InputSchema == nil {
		return empty
	}
	if schema, ok := t.tool.InputSchema.(map[string]any); ok {
		return schema
	}
	data, err := json.Marshal(t.tool.InputSchema)
	if err != nil {
		return empty
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return empty
	}
	return schema
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	result, err := t.caller.CallTool(ctx, t.serverName, t.tool.Name, SenderKeyFromContext(ctx), args)
	if errors.Is(err, ErrMCPNotConnected) && t.NotConnectedMessage != "" {
		return ErrorResult(t.NotConnectedMessage).WithError(err)
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP tool execution failed: %v", err)).WithError(err)
	}
	if result == nil {
		return ErrorResult("MCP tool execution failed: nil result").WithError(fmt.Errorf("nil MCP result"))
	}
	text := mcpContentText(result.Content)
	if result.IsError {
		return ErrorResult(fmt.Sprintf("MCP tool returned error: %s", text)).WithError(fmt.Errorf("MCP tool error"))
	}
	return NewToolResult(text)
}

func mcpContentText(content []mcp.Content) string {
	parts := make([]string, 0, len(content))
	for _, c := range content {
		switch v := c.(type) {
		case *mcp.TextContent:
			if s := strings.TrimSpace(v.Text); s != "" {
				parts = append(parts, s)
			}
		case *mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[Image: %s]", v.MIMEType))
		case *mcp.AudioContent:
			parts = append(parts, fmt.Sprintf("[Audio: %s]", v.MIMEType))
		case *mcp.ResourceLink:
			parts = append(parts, fmt.Sprintf("[Resource link: %s]", v.URI))
		case *mcp.EmbeddedResource:
			if v.Resource != nil && strings.TrimSpace(v.Resource.Text) != "" {
				parts = append(parts, strings.TrimSpace(v.Resource.Text))
			} else {
				parts = append(parts, "[Embedded resource]")
			}
		default:
			parts = append(parts, fmt.Sprintf("[Content: %T]", v))
		}
	}
	return strings.Join(parts, "\n")
}
