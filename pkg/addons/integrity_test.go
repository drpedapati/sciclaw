package addons

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFile_DeterministicAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ah1, err := HashFile(a)
	if err != nil {
		t.Fatal(err)
	}
	ah2, err := HashFile(a)
	if err != nil {
		t.Fatal(err)
	}
	if ah1 != ah2 {
		t.Errorf("HashFile not deterministic: %s vs %s", ah1, ah2)
	}
	bh, err := HashFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if bh != ah1 {
		t.Errorf("same contents should hash identically, got %s vs %s", bh, ah1)
	}
	if err := os.WriteFile(b, []byte("hellO"), 0o644); err != nil {
		t.Fatal(err)
	}
	bh2, err := HashFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if bh2 == ah1 {
		t.Error("content change should change hash")
	}
}

func TestHashPath_DirectoryDeterministicIndependentOfWriteOrder(t *testing.T) {
	mk := func() string {
		d := t.TempDir()
		sub := filepath.Join(d, "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}

	// Order A: write foo, then bar.
	dirA := mk()
	if err := os.WriteFile(filepath.Join(dirA, "foo"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "sub", "bar"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashA, err := HashPath(dirA)
	if err != nil {
		t.Fatal(err)
	}

	// Order B: write bar, then foo.
	dirB := mk()
	if err := os.WriteFile(filepath.Join(dirB, "sub", "bar"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "foo"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashB, err := HashPath(dirB)
	if err != nil {
		t.Fatal(err)
	}

	if hashA != hashB {
		t.Errorf("HashPath not independent of write order: %s vs %s", hashA, hashB)
	}
}

func TestHashPath_DetectsSingleByteChange(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "x"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := HashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "x"), []byte("abcdeF"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := HashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("one-byte change should change HashPath result")
	}
}

func TestHashPath_SkipsGitDir(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, ".git", "config"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "keep"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := HashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, ".git", "config"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := HashPath(d)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error(".git changes should not affect HashPath result")
	}
}

// initTestRepo creates a real git repo with two commits and tags v0.1.0,
// v0.2.0. Returns the directory.
func initTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runs := [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "config", "tag.gpgsign", "false"},
	}
	for _, r := range runs {
		cmd := exec.Command(r[0], r[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(r, " "), err, out)
		}
	}

	commit := func(file, body, msg, tag string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, r := range [][]string{
			{"git", "add", file},
			{"git", "commit", "-q", "-m", msg},
			{"git", "tag", tag},
		} {
			cmd := exec.Command(r[0], r[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", strings.Join(r, " "), err, out)
			}
		}
	}

	commit("README.md", "hello", "first", "v0.1.0")
	commit("CHANGES.md", "more", "second", "v0.2.0")
	return dir
}

func TestVerifyEntry_HappyPath(t *testing.T) {
	dir := initTestRepo(t)
	// Write a manifest-like file and hash it.
	manifestPath := filepath.Join(dir, "addon.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mh, err := HashFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	head, err := gitHeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := &RegistryEntry{
		InstalledCommit: head,
		ManifestSHA256:  mh,
	}
	if err := VerifyEntry(dir, entry, manifestPath, "", ""); err != nil {
		t.Errorf("happy path verify failed: %v", err)
	}
}

func TestVerifyEntry_TamperedManifest(t *testing.T) {
	dir := initTestRepo(t)
	manifestPath := filepath.Join(dir, "addon.json")
	if err := os.WriteFile(manifestPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	mh, err := HashFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	head, err := gitHeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := &RegistryEntry{InstalledCommit: head, ManifestSHA256: mh}

	if err := os.WriteFile(manifestPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = VerifyEntry(dir, entry, manifestPath, "", "")
	if err == nil || !strings.Contains(err.Error(), "manifest_sha256") {
		t.Errorf("expected manifest mismatch, got %v", err)
	}
}

func TestVerifyEntry_TamperedBootstrap(t *testing.T) {
	dir := initTestRepo(t)
	bpath := filepath.Join(dir, "bootstrap.sh")
	if err := os.WriteFile(bpath, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bh, err := HashPath(bpath)
	if err != nil {
		t.Fatal(err)
	}
	head, err := gitHeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := &RegistryEntry{
		InstalledCommit: head,
		BootstrapSHA256: bh,
	}
	if err := os.WriteFile(bpath, []byte("#!/bin/sh\necho hacked\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = VerifyEntry(dir, entry, "", bpath, "")
	if err == nil || !strings.Contains(err.Error(), "bootstrap_sha256") {
		t.Errorf("expected bootstrap mismatch, got %v", err)
	}
}

func TestVerifyEntry_TamperedSidecar(t *testing.T) {
	dir := initTestRepo(t)
	sp := filepath.Join(dir, "sidecar.bin")
	if err := os.WriteFile(sp, []byte("\x00\x01\x02"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh, err := HashFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	head, err := gitHeadCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := &RegistryEntry{InstalledCommit: head, SidecarSHA256: sh}
	if err := os.WriteFile(sp, []byte("\xff\xff\xff"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = VerifyEntry(dir, entry, "", "", sp)
	if err == nil || !strings.Contains(err.Error(), "sidecar_sha256") {
		t.Errorf("expected sidecar mismatch, got %v", err)
	}
}

func TestVerifyEntry_CommitDrift(t *testing.T) {
	dir := initTestRepo(t)
	entry := &RegistryEntry{InstalledCommit: "0000000000000000000000000000000000000000"}
	err := VerifyEntry(dir, entry, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "commit drift") {
		t.Errorf("expected commit drift, got %v", err)
	}
}

func TestComputeHashes_Minimal(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "addon.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires: Requirements{Sciclaw: ">=0.1.0"},
		Sidecar:  SidecarSpec{Binary: "x-sidecar"},
	}
	mh, bh, sh, err := ComputeHashes(dir, m)
	if err != nil {
		t.Fatalf("ComputeHashes: %v", err)
	}
	if mh == "" {
		t.Error("manifest hash should be set")
	}
	if bh != "" {
		t.Errorf("bootstrap hash should be empty when no bootstrap configured, got %q", bh)
	}
	if sh != "" {
		t.Errorf("sidecar hash should be empty when binary is not on disk, got %q", sh)
	}
}

func TestComputeHashes_WithBootstrapAndSidecar(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "addon.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "install.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "x-sidecar"), []byte("ELF..."), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Name: "x", Version: "0.1.0",
		Requires:  Requirements{Sciclaw: ">=0.1.0"},
		Sidecar:   SidecarSpec{Binary: "x-sidecar"},
		Bootstrap: Bootstrap{Install: "./bin/install.sh"},
	}
	mh, bh, sh, err := ComputeHashes(dir, m)
	if err != nil {
		t.Fatalf("ComputeHashes: %v", err)
	}
	if mh == "" || bh == "" || sh == "" {
		t.Errorf("expected all three hashes set, got mh=%q bh=%q sh=%q", mh, bh, sh)
	}
}
