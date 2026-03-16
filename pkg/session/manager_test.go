package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestSessionManagerListKeysSorted(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:z", "user", "z")
	sm.AddMessage("discord:a", "user", "a")
	sm.AddMessage("discord:m", "user", "m")

	keys := sm.ListKeys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "discord:a" || keys[1] != "discord:m" || keys[2] != "discord:z" {
		t.Fatalf("unexpected key order: %#v", keys)
	}
}

func TestSessionManagerSnapshotDeepCopy(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:test", "user", "hello")
	sm.AddMessage("discord:test", "assistant", "world")
	sm.RegisterArtifacts("discord:test", Artifact{
		Role:   "input",
		Path:   filepath.Join(t.TempDir(), "input.docx"),
		Label:  "input.docx",
		Source: "test",
	})

	snap, ok := sm.Snapshot("discord:test")
	if !ok {
		t.Fatal("expected snapshot to exist")
	}
	if len(snap.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(snap.Messages))
	}

	// Mutate snapshot and ensure manager state is unchanged.
	snap.Messages[0].Content = "mutated"
	history := sm.GetHistory("discord:test")
	if history[0].Content != "hello" {
		t.Fatalf("manager history should remain unchanged, got %q", history[0].Content)
	}
	snap.Artifacts[0].Path = "mutated"
	artifacts := sm.GetArtifacts("discord:test")
	if artifacts[0].Path == "mutated" {
		t.Fatalf("manager artifacts should remain unchanged, got %#v", artifacts)
	}
}

func TestSessionManagerReplaceHistory(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:test", "user", "old")

	newHistory := []providers.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	sm.ReplaceHistory("discord:test", newHistory)

	history := sm.GetHistory("discord:test")
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Content != "u1" || history[1].Content != "a1" {
		t.Fatalf("unexpected replaced history: %#v", history)
	}

	// Mutating caller slice should not mutate stored history.
	newHistory[0].Content = "changed"
	history = sm.GetHistory("discord:test")
	if history[0].Content != "u1" {
		t.Fatalf("stored history mutated by caller slice change: %#v", history)
	}
}

func TestSessionManagerPreloadFastPath(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "input.docx")
	if err := os.WriteFile(artifactPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	payload := Session{
		Key: "discord:123@abc",
		Messages: []providers.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
		Artifacts: []Artifact{
			{Role: "input", Path: artifactPath, Label: "input.docx", Source: "test", Updated: time.Now().UTC()},
		},
		Created: time.Now().UTC(),
		Updated: time.Now().UTC(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, payload.Key+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sm := NewSessionManager(dir)
	history := sm.GetHistory(payload.Key)
	if len(history) != 2 {
		t.Fatalf("expected 2 messages preloaded, got %d", len(history))
	}
	if history[0].Content != "hello" || history[1].Content != "world" {
		t.Fatalf("unexpected preloaded history: %#v", history)
	}
	artifacts := sm.GetArtifacts(payload.Key)
	if len(artifacts) != 1 || artifacts[0].Path != artifactPath {
		t.Fatalf("unexpected preloaded artifacts: %#v", artifacts)
	}
}

func TestSessionManagerPreloadTimeoutDoesNotBlockConstructor(t *testing.T) {
	dir := t.TempDir()

	prevTimeout := sessionLoadTimeout
	prevReadDir := readDir
	prevReadFile := readFile
	defer func() {
		sessionLoadTimeout = prevTimeout
		readDir = prevReadDir
		readFile = prevReadFile
	}()

	sessionLoadTimeout = 20 * time.Millisecond
	release := make(chan struct{})
	readDir = func(string) ([]os.DirEntry, error) {
		<-release
		return nil, nil
	}

	start := time.Now()
	_ = NewSessionManager(dir)
	elapsed := time.Since(start)
	if elapsed > 120*time.Millisecond {
		t.Fatalf("constructor blocked too long: %v", elapsed)
	}

	// Let background preload goroutine finish before restoring globals.
	close(release)
	time.Sleep(5 * time.Millisecond)
}

func TestSessionManagerRegisterArtifactsDedupsAndBuildsContext(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, ".sciclaw", "inbound", "discord", "123", "source.docx")
	outputPath := filepath.Join(dir, "outputs", "report.docx")
	if err := os.MkdirAll(filepath.Dir(inputPath), 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	if err := os.WriteFile(inputPath, []byte("input"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("output"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	sm := NewSessionManager("")
	sm.RegisterArtifacts("discord:test",
		Artifact{Role: "input", Path: inputPath, Label: "source.docx", Source: "inbound_media"},
		Artifact{Role: "output", Path: outputPath, Label: "report.docx", Source: "write_file"},
		Artifact{Role: "output", Path: outputPath, Label: "report.docx", Source: "message"},
	)

	artifacts := sm.GetArtifacts("discord:test")
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts after dedup, got %#v", artifacts)
	}

	context := sm.BuildArtifactContext("discord:test", dir)
	if !strings.Contains(context, "## Working Artifacts") {
		t.Fatalf("expected working artifact header, got %q", context)
	}
	if !strings.Contains(context, "source.docx -> .sciclaw/inbound/discord/123/source.docx") {
		t.Fatalf("expected relative input path, got %q", context)
	}
	if !strings.Contains(context, "report.docx -> outputs/report.docx") {
		t.Fatalf("expected relative output path, got %q", context)
	}
	if !strings.Contains(context, "canonical local paths") {
		t.Fatalf("expected canonical-path guidance, got %q", context)
	}
}
