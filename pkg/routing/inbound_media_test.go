package routing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestPrepareInboundMedia_DownloadsURLIntoWorkspaceAndAnnotatesContent(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stub doc"))
	}))
	defer server.Close()

	msg := &bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "chan-1",
		Content:    "please review this",
		SessionKey: "discord:chan-1",
		Media:      []string{server.URL + "/attachments/report.docx"},
		Metadata: map[string]string{
			"message_id": "msg-1",
		},
	}

	if err := prepareInboundMedia(context.Background(), workspace, msg); err != nil {
		t.Fatalf("prepareInboundMedia: %v", err)
	}
	if len(msg.Media) != 1 {
		t.Fatalf("expected one staged media path, got %#v", msg.Media)
	}
	stagedPath := msg.Media[0]
	if !strings.HasPrefix(stagedPath, filepath.Join(workspace, ".sciclaw", "inbound")) {
		t.Fatalf("staged path %q is not under workspace inbound dir", stagedPath)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("expected staged file to exist: %v", err)
	}
	if !strings.Contains(msg.Content, "Attachments staged locally and available to tools:") {
		t.Fatalf("expected staged attachment summary in content, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, ".sciclaw/inbound/discord/msg-1/report.docx") {
		t.Fatalf("expected relative staged path in content, got %q", msg.Content)
	}
}

func TestPrepareInboundMedia_CopiesLocalFileIntoWorkspace(t *testing.T) {
	workspace := t.TempDir()
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "input.docx")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	msg := &bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "chan-1",
		Content:    "review attached",
		SessionKey: "discord:chan-1",
		Media:      []string{sourcePath},
		Metadata: map[string]string{
			"message_id": "msg-2",
		},
	}

	if err := prepareInboundMedia(context.Background(), workspace, msg); err != nil {
		t.Fatalf("prepareInboundMedia: %v", err)
	}
	if len(msg.Media) != 1 {
		t.Fatalf("expected one staged media path, got %#v", msg.Media)
	}
	stagedPath := msg.Media[0]
	if stagedPath == sourcePath {
		t.Fatalf("expected local attachment to be copied into workspace, got original path %q", stagedPath)
	}
	data, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected staged file content: %q", string(data))
	}
	if !strings.Contains(msg.Content, ".sciclaw/inbound/discord/msg-2/input.docx") {
		t.Fatalf("expected staged path in content, got %q", msg.Content)
	}
}

func TestPrepareInboundMedia_UsesContextCancellationForDownloads(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg := &bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "chan-1",
		Content:    "please review this",
		SessionKey: "discord:chan-1",
		Media:      []string{server.URL + "/attachments/report.docx"},
		Metadata: map[string]string{
			"message_id": "msg-3",
		},
	}

	err := prepareInboundMedia(ctx, workspace, msg)
	if err == nil {
		t.Fatal("expected cancelled context to abort download")
	}
}

func TestPrepareInboundMedia_RejectsOversizedDownloads(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", inboundMediaMaxDownloadBytes+1))
		_, _ = w.Write([]byte("tiny"))
	}))
	defer server.Close()

	msg := &bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "chan-1",
		Content:    "please review this",
		SessionKey: "discord:chan-1",
		Media:      []string{server.URL + "/attachments/report.docx"},
		Metadata: map[string]string{
			"message_id": "msg-4",
		},
	}

	err := prepareInboundMedia(context.Background(), workspace, msg)
	if err == nil {
		t.Fatal("expected oversized attachment to fail")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized error, got %v", err)
	}
}
