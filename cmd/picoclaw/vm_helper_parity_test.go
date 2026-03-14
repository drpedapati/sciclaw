package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMToolchainPinsCoreCompanions(t *testing.T) {
	root := repoRoot(t)
	toolchainPath := filepath.Join(root, "deploy", "toolchain.env")
	data, err := os.ReadFile(toolchainPath)
	if err != nil {
		t.Fatalf("read toolchain.env: %v", err)
	}
	txt := string(data)
	required := []string{
		"DOCX_REVIEW_VERSION=",
		"XLSX_REVIEW_VERSION=",
		"PPTX_REVIEW_VERSION=",
		"PUBMED_CLI_VERSION=",
		"SCICLAW_VERSION=0.2.3",
	}
	for _, marker := range required {
		if !strings.Contains(txt, marker) {
			t.Fatalf("toolchain.env missing %q", marker)
		}
	}
}

func TestVMCloudInitInstallsCoreCompanions(t *testing.T) {
	root := repoRoot(t)
	cloudInitPath := filepath.Join(root, "deploy", "multipass-cloud-init.yaml")
	data, err := os.ReadFile(cloudInitPath)
	if err != nil {
		t.Fatalf("read multipass-cloud-init.yaml: %v", err)
	}
	txt := string(data)
	required := []string{
		"XLSX_REVIEW_SHA256=\"$XLSX_REVIEW_SHA256_AMD64\"",
		"XLSX_REVIEW_SHA256=\"$XLSX_REVIEW_SHA256_ARM64\"",
		"PPTX_REVIEW_SHA256=\"$PPTX_REVIEW_SHA256_AMD64\"",
		"PPTX_REVIEW_SHA256=\"$PPTX_REVIEW_SHA256_ARM64\"",
		"# docx-review",
		"# xlsx-review",
		"# pptx-review",
		"# pubmed-cli",
		"/usr/local/bin/docx-review",
		"/usr/local/bin/xlsx-review",
		"/usr/local/bin/pptx-review",
		"/usr/local/bin/pubmed-cli",
		"/tmp/docx-review",
		"/tmp/xlsx-review",
		"/tmp/pptx-review",
		"/tmp/pubmed",
	}
	for _, marker := range required {
		if !strings.Contains(txt, marker) {
			t.Fatalf("multipass-cloud-init.yaml missing %q", marker)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(root, "..", ".."))
}
