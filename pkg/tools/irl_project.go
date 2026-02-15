package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/irl"
)

type irlProjectOperations interface {
	CreateProject(ctx context.Context, req irl.CreateProjectRequest) (*irl.OperationResult, error)
	AdoptProject(ctx context.Context, req irl.AdoptProjectRequest) (*irl.OperationResult, error)
	DiscoverProjects(ctx context.Context) (*irl.OperationResult, error)
	GetWorkspaceContext(ctx context.Context) (*irl.OperationResult, error)
}

type IRLProjectTool struct {
	client irlProjectOperations
}

func NewIRLProjectTool(workspace string) *IRLProjectTool {
	return &IRLProjectTool{
		client: irl.NewClient(workspace),
	}
}

func newIRLProjectToolWithClient(client irlProjectOperations) *IRLProjectTool {
	return &IRLProjectTool{client: client}
}

func (t *IRLProjectTool) Name() string {
	return "irl_project"
}

func (t *IRLProjectTool) Description() string {
	return "Manage IRL projects via the bundled irl companion app (create/adopt/discover/context)."
}

func (t *IRLProjectTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "IRL operation name",
				"enum":        []string{"create_project", "adopt_project", "discover_projects", "get_workspace_context"},
			},
			"purpose": map[string]interface{}{
				"type":        "string",
				"description": "Project purpose for create_project",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Optional exact project name for create_project",
			},
			"template": map[string]interface{}{
				"type":        "string",
				"description": "Optional IRL template name for create/adopt",
			},
			"dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional target directory for create_project",
			},
			"source_path": map[string]interface{}{
				"type":        "string",
				"description": "Source directory path for adopt_project",
			},
			"rename": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether adopt_project should apply --rename",
			},
		},
		"required": []string{"operation"},
	}
}

func (t *IRLProjectTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	operation, ok := getStringArg(args, "operation")
	if !ok || strings.TrimSpace(operation) == "" {
		return ErrorResult("operation is required")
	}

	var (
		res *irl.OperationResult
		err error
	)

	switch operation {
	case "create_project":
		req := irl.CreateProjectRequest{
			Purpose:  getString(args, "purpose"),
			Name:     getString(args, "name"),
			Template: getString(args, "template"),
			Dir:      getString(args, "dir"),
		}
		res, err = t.client.CreateProject(ctx, req)
	case "adopt_project":
		req := irl.AdoptProjectRequest{
			SourcePath: getString(args, "source_path"),
			Template:   getString(args, "template"),
			Rename:     getBool(args, "rename"),
		}
		res, err = t.client.AdoptProject(ctx, req)
	case "discover_projects":
		res, err = t.client.DiscoverProjects(ctx)
	case "get_workspace_context":
		res, err = t.client.GetWorkspaceContext(ctx)
	default:
		return ErrorResult(fmt.Sprintf("unsupported operation: %s", operation))
	}

	if err != nil {
		if res != nil {
			payload := map[string]interface{}{
				"error":  err.Error(),
				"result": res,
			}
			return ErrorResult(mustJSON(payload))
		}
		return ErrorResult(err.Error())
	}

	return NewToolResult(mustJSON(res))
}

func getStringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getString(args map[string]interface{}, key string) string {
	v, _ := getStringArg(args, key)
	return v
}

func getBool(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func mustJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal result: %s"}`, err)
	}
	return string(data)
}
