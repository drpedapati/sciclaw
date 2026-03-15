package tools

import (
	"context"
	"testing"
)

type capabilityTestTool struct {
	name string
}

func (t capabilityTestTool) Name() string                       { return t.name }
func (t capabilityTestTool) Description() string                { return "test" }
func (t capabilityTestTool) Parameters() map[string]interface{} { return map[string]interface{}{} }
func (t capabilityTestTool) Execute(_ context.Context, _ map[string]interface{}) *ToolResult {
	return nil
}

func TestAccessClassForTool(t *testing.T) {
	tests := []struct {
		name string
		want ToolAccessClass
	}{
		{name: "read_file", want: ToolAccessReadOnly},
		{name: "weather_forecast", want: ToolAccessReadOnly},
		{name: "web_search", want: ToolAccessReadOnly},
		{name: "docx_review_diff", want: ToolAccessReadOnly},
		{name: "write_file", want: ToolAccessMutating},
		{name: "message", want: ToolAccessMutating},
		{name: "irl_project", want: ToolAccessMixed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AccessClassForTool(capabilityTestTool{name: tt.name})
			if got != tt.want {
				t.Fatalf("AccessClassForTool(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
