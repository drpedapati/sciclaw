package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "picoclaw-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	os.MkdirAll(workspace, 0755)

	tests := []struct {
		name      string
		path      string
		workspace string
		restrict  bool
		wantErr   bool
	}{
		{
			name:      "Valid relative path",
			path:      "test.txt",
			workspace: workspace,
			restrict:  true,
			wantErr:   false,
		},
		{
			name:      "Valid nested path",
			path:      "dir/test.txt",
			workspace: workspace,
			restrict:  true,
			wantErr:   false,
		},
		{
			name:      "Path traversal attempt (restricted)",
			path:      "../test.txt",
			workspace: workspace,
			restrict:  true,
			wantErr:   true,
		},
		{
			name:      "Path traversal attempt (unrestricted)",
			path:      "../test.txt",
			workspace: workspace,
			restrict:  false,
			wantErr:   false,
		},
		{
			name:      "Absolute path inside workspace",
			path:      filepath.Join(workspace, "test.txt"),
			workspace: workspace,
			restrict:  true,
			wantErr:   false,
		},
		{
			name:      "Absolute path outside workspace (restricted)",
			path:      "/etc/passwd",
			workspace: workspace,
			restrict:  true,
			wantErr:   true,
		},
		{
			name:      "Absolute path outside workspace (unrestricted)",
			path:      "/etc/passwd",
			workspace: workspace,
			restrict:  false,
			wantErr:   false,
		},
		{
			name:      "Empty workspace (no restriction)",
			path:      "/etc/passwd",
			workspace: "",
			restrict:  true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePath(tt.path, tt.workspace, tt.restrict)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
