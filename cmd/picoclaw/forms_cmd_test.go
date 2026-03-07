package main

import "testing"

func TestParseFormsInspectOptions(t *testing.T) {
	opts, err := parseFormsInspectOptions([]string{"--pdf", "./form.pdf", "--json-out", "./out.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.PDFPath != "./form.pdf" {
		t.Fatalf("PDFPath = %q, want %q", opts.PDFPath, "./form.pdf")
	}
	if opts.JSONOut != "./out.json" {
		t.Fatalf("JSONOut = %q, want %q", opts.JSONOut, "./out.json")
	}
}

func TestParseFormsInspectOptions_RequiresPDF(t *testing.T) {
	_, err := parseFormsInspectOptions([]string{"--json-out", "./out.json"})
	if err == nil {
		t.Fatalf("expected error when --pdf missing")
	}
}

func TestParseFormsFillOptions_SourceMode(t *testing.T) {
	opts, err := parseFormsFillOptions([]string{"--pdf", "./in.pdf", "--source", "./input.txt", "--out", "./filled.pdf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Model != "qwen3.5:9b" {
		t.Fatalf("Model = %q, want default", opts.Model)
	}
	if opts.OllamaURL != "http://localhost:11434" {
		t.Fatalf("OllamaURL = %q, want default", opts.OllamaURL)
	}
}

func TestParseFormsFillOptions_ValuesMode(t *testing.T) {
	opts, err := parseFormsFillOptions([]string{"--pdf", "./in.pdf", "--values", "./values.json", "--out", "./filled.pdf", "--no-synthetic"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.NoSynthetic {
		t.Fatalf("expected NoSynthetic=true")
	}
}

func TestParseFormsFillOptions_RequiresExactlyOneInput(t *testing.T) {
	_, err := parseFormsFillOptions([]string{"--pdf", "./in.pdf", "--out", "./filled.pdf"})
	if err == nil {
		t.Fatalf("expected error when source/values missing")
	}

	_, err = parseFormsFillOptions([]string{"--pdf", "./in.pdf", "--source", "./s.txt", "--values", "./v.json", "--out", "./filled.pdf"})
	if err == nil {
		t.Fatalf("expected error when source and values both provided")
	}
}
