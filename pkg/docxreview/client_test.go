package docxreview

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
	inputPath := filepath.Join(workspace, "paper.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		if binary != "/usr/local/bin/docx-review" {
			t.Fatalf("unexpected binary: %s", binary)
		}
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"file":%q,"paragraphs":[],"comments":[],"metadata":{"word_count":42,"paragraph_count":3},"summary":{"total_tracked_changes":0,"insertions":0,"deletions":0,"total_comments":0,"change_authors":[],"comment_authors":[]}}`, inputPath)}, nil
	})

	result, err := client.Read(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	wantArgs := []string{inputPath, "--read", "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if result.Metadata.WordCount != 42 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestClientDiffParsesJSON(t *testing.T) {
	workspace := t.TempDir()
	oldPath := filepath.Join(workspace, "old.docx")
	newPath := filepath.Join(workspace, "new.docx")
	for _, p := range []string{oldPath, newPath} {
		if err := os.WriteFile(p, []byte("docx"), 0o644); err != nil {
			t.Fatalf("write docx: %v", err)
		}
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"old_file":%q,"new_file":%q,"metadata":{"changes":[]},"paragraphs":{"added":[],"deleted":[],"modified":[]},"comments":{"added":[],"deleted":[],"modified":[]},"tracked_changes":{"added":[],"deleted":[]},"summary":{"text_changes":1,"paragraphs_added":0,"paragraphs_deleted":0,"paragraphs_modified":1,"comment_changes":0,"tracked_change_changes":0,"formatting_changes":0,"style_changes":0,"metadata_changes":0,"identical":false}}`, oldPath, newPath)}, nil
	})

	result, err := client.Diff(context.Background(), oldPath, newPath)
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	wantArgs := []string{"--diff", oldPath, newPath, "--json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
	if result.Summary.TextChanges != 1 || result.Summary.Identical {
		t.Fatalf("unexpected diff result: %+v", result)
	}
}

func TestClientApplyParsesPartialJSONOnExitOne(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		_ = args
		if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
			t.Fatalf("write output: %v", err)
		}
		return RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":2,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace","success":true,"message":"Replaced"},{"index":1,"type":"replace","success":false,"message":"Not found"}],"success":false}`, inputPath, outputPath), ExitCode: 1}, fmt.Errorf("exit status 1")
	})

	result, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Status != "partial" || result.ExitCode != 1 {
		t.Fatalf("unexpected partial status: %+v", result)
	}
	if !result.OutputWritten {
		t.Fatalf("expected outputWritten=true, got %+v", result)
	}
}

func TestClientApplyDryRunAllowsEmptyOutputPath(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	var gotArgs []string
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	}, func(ctx context.Context, binary string, args []string) (RunResult, error) {
		_ = ctx
		_ = binary
		gotArgs = append([]string{}, args...)
		return RunResult{Stdout: fmt.Sprintf(`{"input":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace","success":true,"message":"Validated"}],"success":true}`, inputPath)}, nil
	})

	result, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, DryRun: true, AcceptExisting: true})
	if err != nil {
		t.Fatalf("Apply dry-run returned error: %v", err)
	}
	if !result.DryRun || !result.AcceptExisting {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	wantArgs := []string{inputPath, manifestPath, "--json", "--dry-run", "--accept-existing"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got %v want %v", gotArgs, wantArgs)
	}
}

func TestClientApplyRejectsOutputEqualToInput(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := NewClientWithOptions(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	})
	_, err := client.Apply(context.Background(), ApplyRequest{InputPath: inputPath, ManifestPath: manifestPath, OutputPath: inputPath})
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected same-path error, got %v", err)
	}
}

func TestClientResolveBinaryPathPrefersExplicitEnvOverride(t *testing.T) {
	workspace := t.TempDir()
	override := filepath.Join(workspace, "docx-review")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create override: %v", err)
	}
	t.Setenv("PICOCLAW_DOCX_REVIEW_BINARY", override)

	client := NewClientWithOptions(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
	})
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
	inputPath := filepath.Join(workspace, "paper.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	client := newClientWithRunner(ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
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
