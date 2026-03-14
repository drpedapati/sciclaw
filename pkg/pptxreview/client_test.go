package pptxreview

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

func TestClientReadParsesJSON(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		if binary != "/usr/local/bin/pptx-review" {
			t.Fatalf("unexpected binary: %s", binary)
		}
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: `{"slide_count":2,"slides":[{"slide":1,"title":"Intro"}]}`}, nil
	})

	result, err := client.Read(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	wantArgs := []string{inputPath, "--read", "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if result.SlideCount != 2 || len(result.Slides) == 0 {
		t.Fatalf("unexpected read result: %+v", result)
	}
}

func TestClientDiffParsesJSON(t *testing.T) {
	workspace := t.TempDir()
	oldPath := filepath.Join(workspace, "old.pptx")
	newPath := filepath.Join(workspace, "new.pptx")
	for _, p := range []string{oldPath, newPath} {
		if err := os.WriteFile(p, []byte("pptx"), 0o644); err != nil {
			t.Fatalf("write pptx: %v", err)
		}
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"old_file":%q,"new_file":%q,"metadata":{},"slides":[{"slide":1,"changes":1}],"summary":{"identical":false}}`, oldPath, newPath)}, nil
	})

	result, err := client.Diff(context.Background(), oldPath, newPath)
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	wantArgs := []string{"--diff", oldPath, newPath, "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if result.OldFile != oldPath || result.NewFile != newPath || len(result.Slides) == 0 {
		t.Fatalf("unexpected diff result: %+v", result)
	}
}

func TestClientApplyParsesPartialJSONOnExitOne(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		_ = args
		if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
			t.Fatalf("write output: %v", err)
		}
		return RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":2,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace_text","success":true,"message":"Applied"},{"index":1,"type":"set_notes","success":false,"message":"Missing slide"}],"success":false}`, inputPath, outputPath), ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	result, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Status != "partial" || result.ExitCode != 1 || !result.OutputWritten {
		t.Fatalf("unexpected partial result: %+v", result)
	}
}

func TestClientApplyDryRunAllowsEmptyOutputPath(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"input":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace_text","success":true,"message":"Validated"}],"success":true}`, inputPath)}, nil
	})

	result, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, DryRun: true, Author: "Reviewer"})
	if err != nil {
		t.Fatalf("Apply dry-run returned error: %v", err)
	}
	wantArgs := []string{inputPath, manifestPath, "--json", "--author", "Reviewer", "--dry-run"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if !result.DryRun || result.OutputWritten {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
}

func TestClientApplyRejectsOutputEqualToInput(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	client := NewClientWithOptions(ClientOptions{LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil }})
	_, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, OutputPath: inputPath})
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected same-path error, got %v", err)
	}
}

func TestClientResolveBinaryPathPrefersExplicitEnvOverride(t *testing.T) {
	workspace := t.TempDir()
	override := filepath.Join(workspace, "pptx-review")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create override: %v", err)
	}
	t.Setenv("PICOCLAW_PPTX_REVIEW_BINARY", override)
	client := NewClientWithOptions(ClientOptions{LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil }})
	resolved, err := client.ResolveBinaryPath()
	if err != nil {
		t.Fatalf("ResolveBinaryPath returned error: %v", err)
	}
	if resolved != override {
		t.Fatalf("expected override %q, got %q", override, resolved)
	}
}

func TestClientPropagatesTimeoutInvocationFailure(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		Timeout:    5 * time.Millisecond,
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		<-ctx.Done()
		return RunResult{ExitCode: -1}, ctx.Err()
	})

	_, err := client.Read(context.Background(), inputPath)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}
