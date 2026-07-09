package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestPersistProviderMedia_WritesWorkspacePNG(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	got := al.persistProviderMedia([]providers.MediaAttachment{{
		Filename: "cube.png",
		MIME:     "image/png",
		Data:     png,
	}})
	if len(got) != 1 {
		t.Fatalf("attachments=%d want 1", len(got))
	}
	if got[0].Filename != "cube.png" {
		t.Fatalf("filename=%q want cube.png", got[0].Filename)
	}
	wantPath := filepath.Join(workspace, "artifacts", "generated", "cube.png")
	if got[0].Path != wantPath {
		t.Fatalf("path=%q want %q", got[0].Path, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	if string(data) != string(png) {
		t.Fatalf("file bytes mismatch")
	}
}
