package addons

import (
	"os/exec"
	"strings"
	"testing"
)

// initResolverRepo creates a temp git repo with two commits tagged v0.1.0
// and v0.2.0 and a main branch pointing at HEAD.
func initResolverRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	return initTestRepo(t)
}

func TestResolve_ExplicitCommit(t *testing.T) {
	dir := initResolverRepo(t)
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(dir, NewCommitRef(head))
	if err != nil {
		t.Fatalf("Resolve commit: %v", err)
	}
	if r.Commit != head {
		t.Errorf("commit = %s, want %s", r.Commit, head)
	}
	if r.SignedTag != "" {
		t.Errorf("SignedTag should be empty for commit resolution, got %q", r.SignedTag)
	}
}

func TestResolve_VersionWithVPrefix(t *testing.T) {
	dir := initResolverRepo(t)
	want, err := gitOutput(dir, "rev-parse", "v0.1.0^{}")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(dir, NewVersionRef("v0.1.0"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Commit != want {
		t.Errorf("commit = %s, want %s", r.Commit, want)
	}
	if r.SignedTag != "v0.1.0" {
		t.Errorf("SignedTag = %q, want v0.1.0", r.SignedTag)
	}
}

func TestResolve_VersionWithoutVPrefix(t *testing.T) {
	dir := initResolverRepo(t)
	want, err := gitOutput(dir, "rev-parse", "v0.1.0^{}")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(dir, NewVersionRef("0.1.0"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Commit != want {
		t.Errorf("commit = %s, want %s", r.Commit, want)
	}
}

func TestResolve_Track(t *testing.T) {
	dir := initResolverRepo(t)
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(dir, NewTrackRef("main"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Commit != head {
		t.Errorf("commit = %s, want %s", r.Commit, head)
	}
}

func TestResolve_AutoReturnsLatestTag(t *testing.T) {
	dir := initResolverRepo(t)
	wantCommit, err := gitOutput(dir, "rev-parse", "v0.2.0^{}")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(dir, NewAutoRef())
	if err != nil {
		t.Fatalf("Resolve auto: %v", err)
	}
	if r.Commit != wantCommit {
		t.Errorf("commit = %s, want %s (v0.2.0)", r.Commit, wantCommit)
	}
	if r.SignedTag != "v0.2.0" {
		t.Errorf("SignedTag = %q, want v0.2.0", r.SignedTag)
	}
}

func TestResolve_UnknownCommit(t *testing.T) {
	dir := initResolverRepo(t)
	_, err := Resolve(dir, NewCommitRef("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestResolve_UnknownVersion(t *testing.T) {
	dir := initResolverRepo(t)
	_, err := Resolve(dir, NewVersionRef("99.0.0"))
	if err == nil || !strings.Contains(err.Error(), "99.0.0") {
		t.Errorf("expected version error, got %v", err)
	}
}

func TestResolve_UnknownBranch(t *testing.T) {
	dir := initResolverRepo(t)
	_, err := Resolve(dir, NewTrackRef("does-not-exist"))
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected branch error, got %v", err)
	}
}

func TestResolve_AutoNoTagsError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, r := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "commit", "--allow-empty", "-q", "-m", "empty"},
	} {
		cmd := exec.Command(r[0], r[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(r, " "), err, out)
		}
	}
	_, err := Resolve(dir, NewAutoRef())
	if err == nil {
		t.Fatal("expected error for repo with no tags")
	}
	if !strings.Contains(err.Error(), "no signed tags found") {
		t.Errorf("error should mention 'no signed tags found', got: %v", err)
	}
	if !strings.Contains(err.Error(), "--commit") || !strings.Contains(err.Error(), "--version") || !strings.Contains(err.Error(), "--track") {
		t.Errorf("error should suggest --commit/--version/--track, got: %v", err)
	}
}

func TestLatestSignedTag_ReturnsMostRecent(t *testing.T) {
	dir := initResolverRepo(t)
	tag, commit, err := LatestSignedTag(dir)
	if err != nil {
		t.Fatalf("LatestSignedTag: %v", err)
	}
	if tag != "v0.2.0" {
		t.Errorf("tag = %q, want v0.2.0", tag)
	}
	want, err := gitOutput(dir, "rev-parse", "v0.2.0^{}")
	if err != nil {
		t.Fatal(err)
	}
	if commit != want {
		t.Errorf("commit = %s, want %s", commit, want)
	}
}
