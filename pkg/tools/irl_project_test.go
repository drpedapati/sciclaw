package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/irl"
)

type fakeIRLClient struct {
	lastCreateReq irl.CreateProjectRequest
	lastAdoptReq  irl.AdoptProjectRequest
	result        *irl.OperationResult
	err           error
}

func (f *fakeIRLClient) CreateProject(ctx context.Context, req irl.CreateProjectRequest) (*irl.OperationResult, error) {
	f.lastCreateReq = req
	return f.result, f.err
}

func (f *fakeIRLClient) AdoptProject(ctx context.Context, req irl.AdoptProjectRequest) (*irl.OperationResult, error) {
	f.lastAdoptReq = req
	return f.result, f.err
}

func (f *fakeIRLClient) DiscoverProjects(ctx context.Context) (*irl.OperationResult, error) {
	return f.result, f.err
}

func (f *fakeIRLClient) GetWorkspaceContext(ctx context.Context) (*irl.OperationResult, error) {
	return f.result, f.err
}

func TestIRLProjectTool_CreateProject(t *testing.T) {
	fake := &fakeIRLClient{
		result: &irl.OperationResult{
			Operation: "create_project",
			Status:    irl.StatusSuccess,
		},
	}
	tool := newIRLProjectToolWithClient(fake)

	res := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "create_project",
		"purpose":   "meta-analysis",
		"name":      "study-a",
		"template":  "irl-basic",
		"dir":       "/tmp",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if fake.lastCreateReq.Purpose != "meta-analysis" {
		t.Fatalf("unexpected create purpose: %s", fake.lastCreateReq.Purpose)
	}
	if fake.lastCreateReq.Name != "study-a" {
		t.Fatalf("unexpected create name: %s", fake.lastCreateReq.Name)
	}
}

func TestIRLProjectTool_AdoptProject(t *testing.T) {
	fake := &fakeIRLClient{
		result: &irl.OperationResult{
			Operation: "adopt_project",
			Status:    irl.StatusSuccess,
		},
	}
	tool := newIRLProjectToolWithClient(fake)

	res := tool.Execute(context.Background(), map[string]interface{}{
		"operation":   "adopt_project",
		"source_path": "/tmp/existing",
		"rename":      true,
		"template":    "irl-basic",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if fake.lastAdoptReq.SourcePath != "/tmp/existing" {
		t.Fatalf("unexpected source_path: %s", fake.lastAdoptReq.SourcePath)
	}
	if !fake.lastAdoptReq.Rename {
		t.Fatalf("expected rename=true")
	}
}

func TestIRLProjectTool_UnsupportedOperation(t *testing.T) {
	tool := newIRLProjectToolWithClient(&fakeIRLClient{})
	res := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "open_project",
	})
	if !res.IsError {
		t.Fatalf("expected error for unsupported operation")
	}
	if !strings.Contains(res.ForLLM, "unsupported operation") {
		t.Fatalf("unexpected error message: %s", res.ForLLM)
	}
}

func TestIRLProjectTool_ErrorPayloadIncludesResult(t *testing.T) {
	fake := &fakeIRLClient{
		result: &irl.OperationResult{
			Operation: "discover_projects",
			Status:    irl.StatusFailure,
		},
		err: context.DeadlineExceeded,
	}
	tool := newIRLProjectToolWithClient(fake)

	res := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "discover_projects",
	})
	if !res.IsError {
		t.Fatalf("expected error result")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("error payload should be JSON: %v", err)
	}
	if _, ok := payload["result"]; !ok {
		t.Fatalf("expected result in error payload")
	}
}
