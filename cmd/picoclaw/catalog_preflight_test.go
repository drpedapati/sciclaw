package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Characterization test: records the current adapter limitation. The follow-up
// implementation must invert this assertion and preserve per-model metadata.
func TestPreflightWebCatalogCurrentlyDropsReasoningMetadata(t *testing.T) {
	execStub := &webTestExec{output: `{"provider":"openai","source":"endpoint","models":["gpt-6.1-sol"],"metadata":{"gpt-6.1-sol":{"supported_reasoning_levels":["low","medium","max"]}}}`}
	srv := newWebServer(execStub, "")
	rec := httptest.NewRecorder()
	srv.handleModelsAction(rec, httptest.NewRequest(http.MethodGet, "/api/models/catalog", nil))
	var body struct {
		Models []map[string]interface{} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(body.Models) != 1 || body.Models[0]["id"] != "gpt-6.1-sol" {
		t.Fatalf("model ID did not survive adapter: %s", rec.Body.String())
	}
	if _, exists := body.Models[0]["supported_reasoning_levels"]; exists {
		t.Fatal("adapter now preserves metadata; update preflight recommendation")
	}
}
