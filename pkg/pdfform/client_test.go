package pdfform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientInspectParsesUnsupportedJSONOnExitOne(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		if binary != "/usr/local/bin/pdf-form-filler" {
			t.Fatalf("unexpected binary: %s", binary)
		}
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"pdfPath":%q,"formType":"none","isXfaForm":false,"isSupportedAcroForm":false,"canFillValues":false,"supportedFillableFieldCount":0,"validationMessage":"PDF does not contain a form.","fieldCount":0,"fields":[]}`, pdfPath), ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	inspection, err := client.Inspect(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	wantArgs := []string{"inspect", "--pdf", pdfPath, "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if inspection.IsSupportedAcroForm {
		t.Fatalf("expected unsupported inspection, got %+v", inspection)
	}
}

func TestClientInspectReturnsCLIErrorWhenExitOneJSONMissing(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		_ = args
		return RunResult{Stderr: "bad pdf", ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	_, err := client.Inspect(context.Background(), pdfPath)
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
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"pdfPath":%q,"formType":"acroform","isXfaForm":false,"isSupportedAcroForm":true,"canFillValues":true,"supportedFillableFieldCount":1,"validationMessage":"ok","fieldCount":1,"fields":[{"name":"PatientName","kind":"text","toolTip":"Patient","readOnly":false,"required":true,"choices":[]}]}`, pdfPath)}, nil
	})

	schema, err := client.Schema(context.Background(), pdfPath)
	if err != nil {
		t.Fatalf("Schema returned error: %v", err)
	}
	wantArgs := []string{"schema", "--pdf", pdfPath}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if len(schema.Fields) != 1 || schema.Fields[0].Name != "PatientName" {
		t.Fatalf("unexpected schema: %+v", schema)
	}
}

func TestClientFillParsesJSONAndRequiresOutput(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	valuesPath := filepath.Join(workspace, "values.json")
	outputPath := filepath.Join(workspace, "filled.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if err := os.WriteFile(valuesPath, []byte(`{"Field":"Value"}`), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
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
		return RunResult{Stdout: fmt.Sprintf(`{"pdfPath":%q,"outputPath":%q,"formType":"acroform","flattened":true,"appliedFields":3,"skippedFields":[],"unusedInputKeys":[]}`, pdfPath, outputPath)}, nil
	})

	res, err := client.Fill(context.Background(), FillRequest{PDFPath: pdfPath, ValuesPath: valuesPath, OutputPath: outputPath, Flatten: true})
	if err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}
	wantArgs := []string{"fill", "--pdf", pdfPath, "--values", valuesPath, "--out", outputPath, "--json", "--flatten"}
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

func TestClientResolveBinaryPathPrefersExplicitEnvOverride(t *testing.T) {
	workspace := t.TempDir()
	override := filepath.Join(workspace, "pdf-form-filler")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create override binary: %v", err)
	}
	t.Setenv("PICOCLAW_PDF_FORM_FILLER_BINARY", override)

	client := NewClientWithOptions(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	})

	resolved, err := client.ResolveBinaryPath()
	if err != nil {
		t.Fatalf("ResolveBinaryPath returned error: %v", err)
	}
	if resolved != override {
		t.Fatalf("expected explicit override %q, got %q", override, resolved)
	}
}

func TestClientFillErrorsWhenOutputFileIsStale(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	valuesPath := filepath.Join(workspace, "values.json")
	outputPath := filepath.Join(workspace, "filled.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if err := os.WriteFile(valuesPath, []byte(`{"Field":"Value"}`), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("failed to write stale output: %v", err)
	}
	beforeInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat stale output: %v", err)
	}

	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		_ = args
		return RunResult{Stdout: fmt.Sprintf(`{"pdfPath":%q,"outputPath":%q,"formType":"acroform","flattened":false,"appliedFields":1,"skippedFields":[],"unusedInputKeys":[]}`, pdfPath, outputPath)}, nil
	})

	_, err = client.Fill(context.Background(), FillRequest{
		PDFPath:    pdfPath,
		ValuesPath: valuesPath,
		OutputPath: outputPath,
	})
	if err == nil {
		t.Fatal("expected stale output error")
	}
	afterInfo, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("stat output after fill: %v", statErr)
	}
	if !sameFileSnapshot(beforeInfo, afterInfo) {
		t.Fatalf("expected stale output snapshot to remain unchanged")
	}
}

func TestClientPropagatesTimeoutInvocationFailure(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
		Timeout:    5 * time.Millisecond,
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		<-ctx.Done()
		return RunResult{ExitCode: -1}, ctx.Err()
	})

	_, err := client.Schema(context.Background(), pdfPath)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestClientFillRejectsOutputEqualToInput(t *testing.T) {
	workspace := t.TempDir()
	pdfPath := filepath.Join(workspace, "form.pdf")
	valuesPath := filepath.Join(workspace, "values.json")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if err := os.WriteFile(valuesPath, []byte(`{"Field":"Value"}`), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}

	client := NewClientWithOptions(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pdf-form-filler", nil },
	})

	_, err := client.Fill(context.Background(), FillRequest{
		PDFPath:    pdfPath,
		ValuesPath: valuesPath,
		OutputPath: pdfPath,
	})
	if err == nil {
		t.Fatal("expected same-path rejection")
	}
	if !strings.Contains(err.Error(), "output path must differ") {
		t.Fatalf("unexpected error: %v", err)
	}
}
