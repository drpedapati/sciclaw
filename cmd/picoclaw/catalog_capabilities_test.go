package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebCatalogPreservesCapabilitiesAndProvider(t *testing.T) {
	execStub := &webTestExec{output: `{"provider":"anthropic","source":"endpoint+builtin","models":["gpt-6.1-sol"],"metadata":{"gpt-6.1-sol":{"name":"Sol","provider":"openai","source":"endpoint","reasoning_levels":["medium","max"]}}}`}
	srv := newWebServer(execStub, "")
	rec := httptest.NewRecorder()
	srv.handleModelsAction(rec, httptest.NewRequest(http.MethodGet, "/api/models/catalog", nil))
	var body struct {
		Models []struct {
			Provider string   `json:"provider"`
			Name     string   `json:"name"`
			Levels   []string `json:"reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Models) != 1 || body.Models[0].Provider != "openai" || body.Models[0].Name != "Sol" || len(body.Models[0].Levels) != 2 {
		t.Fatalf("capabilities lost: %s", rec.Body.String())
	}
}
