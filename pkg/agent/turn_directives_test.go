package agent

import "testing"

func TestParseTurnDirectives(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantModel  string
		wantEffort string
		wantBody   string
		wantHad    bool
	}{
		{
			name:     "no directives",
			in:       "just a normal question",
			wantBody: "just a normal question",
		},
		{
			name:      "model alias line",
			in:        "model: luna\nsummarize notes",
			wantModel: "luna",
			wantBody:  "summarize notes",
			wantHad:   true,
		},
		{
			name:       "combined first line",
			in:         "model: sol effort: high\nDo the thing",
			wantModel:  "sol",
			wantEffort: "high",
			wantBody:   "Do the thing",
			wantHad:    true,
		},
		{
			name:       "split lines",
			in:         "model: gpt-5.6-terra\neffort: low\n\nhello",
			wantModel:  "gpt-5.6-terra",
			wantEffort: "low",
			wantBody:   "hello",
			wantHad:    true,
		},
		{
			name:       "effort only",
			in:         "effort: xhigh\nping",
			wantEffort: "xhigh",
			wantBody:   "ping",
			wantHad:    true,
		},
		{
			name:     "model mentioned mid-message ignored",
			in:       "please use model: luna for this",
			wantBody: "please use model: luna for this",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTurnDirectives(tt.in)
			if got.Model != tt.wantModel {
				t.Fatalf("Model=%q want %q", got.Model, tt.wantModel)
			}
			if got.Effort != tt.wantEffort {
				t.Fatalf("Effort=%q want %q", got.Effort, tt.wantEffort)
			}
			if got.Body != tt.wantBody {
				t.Fatalf("Body=%q want %q", got.Body, tt.wantBody)
			}
			if got.HadDirectives != tt.wantHad {
				t.Fatalf("HadDirectives=%v want %v", got.HadDirectives, tt.wantHad)
			}
		})
	}
}

func TestResolveTurnOverrides(t *testing.T) {
	model, effort, err := ResolveTurnOverrides("gpt-5.6-sol", "luna", "high")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if model != "gpt-5.6-luna" || effort != "high" {
		t.Fatalf("got model=%q effort=%q", model, effort)
	}

	_, _, err = ResolveTurnOverrides("gpt-5.6-sol", "sonnet", "")
	if err == nil {
		t.Fatal("expected cross-provider error")
	}

	_, _, err = ResolveTurnOverrides("claude-sonnet-4.6", "luna", "")
	if err == nil {
		t.Fatal("expected cross-provider error for claude workspace")
	}

	model, effort, err = ResolveTurnOverrides("claude-sonnet-4.6", "opus", "medium")
	if err != nil {
		t.Fatalf("claude alias: %v", err)
	}
	if model != "claude-opus-4-6" {
		t.Fatalf("opus alias = %q", model)
	}
	// effort still validated even on anthropic (harmless if provider ignores)
	if effort != "medium" {
		t.Fatalf("effort=%q", effort)
	}

	_, _, err = ResolveTurnOverrides("gpt-5.6-sol", "", "nope")
	if err == nil {
		t.Fatal("expected bad effort error")
	}

	model, effort, err = ResolveTurnOverrides("gpt-5.6-sol", "", "low")
	if err != nil || model != "gpt-5.6-sol" || effort != "low" {
		t.Fatalf("effort-only got model=%q effort=%q err=%v", model, effort, err)
	}
}

func TestModelProviderFamily(t *testing.T) {
	if got := modelProviderFamily("gpt-5.6-luna"); got != "openai" {
		t.Fatalf("got %q", got)
	}
	if got := modelProviderFamily("claude-opus-4-6"); got != "anthropic" {
		t.Fatalf("got %q", got)
	}
}
