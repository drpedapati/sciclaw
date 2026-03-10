package pdfform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClientInspectParsesUnsupportedJSONOnExitOne(t *testing.T) {
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		if binary != "/usr/local/bin/pdf-form-filler" {
			t.Fatalf("unexpected binary: %s", binary)
		}
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: `{"pdfPath":"form.pdf","formType":"none","isXfaForm":false,"isSupportedAcroForm":false,"canFillValues":false,"supportedFillableFieldCount":0,"validationMessage":"PDF does not contain a form.","fieldCount":0,"fields":[]}`, ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	inspection, err := client.Inspect(context.Background(), "form.pdf")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	wantArgs := []string{"inspect", "--pdf", "form.pdf", "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if inspection.IsSupportedAcroForm {
		t.Fatalf("expected unsupported inspection, got %+v", inspection)
	}
}

func TestClientInspectReturnsCLIErrorWhenExitOneJSONMissing(t *testing.T) {
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		_ = args
		return RunResult{Stderr: "bad pdf", ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	_, err := client.Inspect(context.Background(), "form.pdf")
	if err == nil {
		t.Fatal("expected error")
	}
	cliErr, ok := err.(*CLIError)
	if !ok {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if cliErr.ExitCode != 1 {
		t.Fatalf("unexpected exit code: %d", cliErr.ExitCode)
	}
}

func TestClientSchemaParsesJSON(t *testing.T) {
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: `{"pdfPath":"form.pdf","formType":"acroform","isXfaForm":false,"isSupportedAcroForm":true,"canFillValues":true,"supportedFillableFieldCount":1,"validationMessage":"ok","fieldCount":1,"fields":[{"name":"PatientName","kind":"text","toolTip":"Patient","readOnly":false,"required":true,"choices":[]}]}`}, nil
	})

	schema, err := client.Schema(context.Background(), "form.pdf")
	if err != nil {
		t.Fatalf("Schema returned error: %v", err)
	}
	wantArgs := []string{"schema", "--pdf", "form.pdf"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if len(schema.Fields) != 1 || schema.Fields[0].Name != "PatientName" {
		t.Fatalf("unexpected schema: %+v", schema)
	}
}

func TestClientFillParsesJSONAndRequiresOutput(t *testing.T) {
	workspace := t.TempDir()
	outputPath := filepath.Join(workspace, "filled.pdf")
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		if err := os.WriteFile(outputPath, []byte("%PDF-1.7"), 0o644); err != nil {
			t.Fatalf("failed to create output: %v", err)
		}
		return RunResult{Stdout: fmt.Sprintf(`{"pdfPath":"form.pdf","outputPath":%q,"formType":"acroform","flattened":true,"appliedFields":3,"skippedFields":[],"unusedInputKeys":[]}`, outputPath)}, nil
	})

	res, err := client.Fill(context.Background(), FillRequest{PDFPath: "form.pdf", ValuesPath: "values.json", OutputPath: outputPath, Flatten: true})
	if err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}
	wantArgs := []string{"fill", "--pdf", "form.pdf", "--values", "values.json", "--out", outputPath, "--json", "--flatten"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if res.AppliedFields != 3 || !res.Flattened {
		t.Fatalf("unexpected fill result: %+v", res)
	}
}

func TestClientResolveBinaryPathFallsBackToCandidates(t *testing.T) {
	workspace := t.TempDir()
	candidate := filepath.Join(workspace, "pdf-form-filler")
	if err := os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create candidate: %v", err)
	}

	client := NewClientWithOptions(ClientOptions{
		LookPathFn:       func(file string) (string, error) { return "", fmt.Errorf("not found") },
		BinaryCandidates: []string{candidate},
	})

	resolved, err := client.ResolveBinaryPath()
	if err != nil {
		t.Fatalf("ResolveBinaryPath returned error: %v", err)
	}
	if resolved != candidate {
		t.Fatalf("unexpected binary path: %s", resolved)
	}
}
