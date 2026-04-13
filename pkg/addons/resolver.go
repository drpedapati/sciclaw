package addons

import (
	"fmt"
	"os/exec"
	"strings"
)

// InstallRef expresses how a caller wants to pin an addon install.
// Exactly one of Commit, Version, or Track should be set; an empty value
// for all three means "auto-resolve to latest signed tag".
type InstallRef struct {
	Commit  string
	Version string
	Track   string
}

// NewCommitRef pins to an exact commit SHA.
func NewCommitRef(sha string) InstallRef { return InstallRef{Commit: sha} }

// NewVersionRef pins to a semantic version tag (with or without "v" prefix).
func NewVersionRef(v string) InstallRef { return InstallRef{Version: v} }

// NewTrackRef opts into tracking a branch (records branch name, still pins
// a SHA at install time).
func NewTrackRef(branch string) InstallRef { return InstallRef{Track: branch} }

// NewAutoRef asks the resolver to pick the latest signed tag.
func NewAutoRef() InstallRef { return InstallRef{} }

// ResolvedRef is the output of Resolve. Commit is always set; SignedTag and
// SignatureVerified are populated when the resolution went through a tag.
type ResolvedRef struct {
	Commit            string
	SignedTag         string
	SignatureVerified bool
}

// Resolve translates an InstallRef into an exact commit SHA using git in the
// addon working tree.
//
// TODO(signing): verify tag signatures once pkg/addons/signing.go exists.
// This wave only records whether a tag was used, not whether it was signed.
func Resolve(addonDir string, ref InstallRef) (*ResolvedRef, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not found on PATH: %w", err)
	}
	switch {
	case ref.Commit != "":
		return resolveCommit(addonDir, ref.Commit)
	case ref.Version != "":
		return resolveVersion(addonDir, ref.Version)
	case ref.Track != "":
		return resolveTrack(addonDir, ref.Track)
	default:
		tag, commit, err := LatestSignedTag(addonDir)
		if err != nil {
			return nil, err
		}
		return &ResolvedRef{Commit: commit, SignedTag: tag, SignatureVerified: false}, nil
	}
}

// rejectGitFlagLike rejects strings that would be parsed as a git flag
// instead of a ref name. `--` is supported by some git subcommands but not
// all, so the robust defense is to refuse the input at the boundary.
func rejectGitFlagLike(kind, value string) error {
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s %q must not start with '-'", kind, value)
	}
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s %q contains control characters", kind, value)
	}
	return nil
}

func resolveCommit(addonDir, sha string) (*ResolvedRef, error) {
	if err := rejectGitFlagLike("commit", sha); err != nil {
		return nil, err
	}
	if err := runGit(addonDir, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return nil, fmt.Errorf("commit %q not found in %s; run 'git fetch' or check the SHA", sha, addonDir)
	}
	full, err := gitOutput(addonDir, "rev-parse", sha+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolving commit %q: %w", sha, err)
	}
	return &ResolvedRef{Commit: full, SignedTag: "", SignatureVerified: false}, nil
}

func resolveVersion(addonDir, version string) (*ResolvedRef, error) {
	if err := rejectGitFlagLike("version", version); err != nil {
		return nil, err
	}
	candidates := []string{version}
	if !strings.HasPrefix(version, "v") {
		candidates = append([]string{"v" + version}, candidates...)
	}
	for _, tag := range candidates {
		// Use a fully-qualified refs/tags/ path so ambiguous short names
		// can't resolve to a branch or arbitrary ref, and dereference
		// annotated tags with ^{} so we always land on a commit.
		commit, err := gitOutput(addonDir, "rev-parse", "refs/tags/"+tag+"^{}")
		if err == nil {
			// TODO(signing): verify tag signature here once signing.go exists.
			return &ResolvedRef{Commit: commit, SignedTag: tag, SignatureVerified: false}, nil
		}
	}
	return nil, fmt.Errorf("version %q not found in %s (tried %s); run 'git fetch --tags' or check the version",
		version, addonDir, strings.Join(candidates, ", "))
}

func resolveTrack(addonDir, branch string) (*ResolvedRef, error) {
	if err := rejectGitFlagLike("branch", branch); err != nil {
		return nil, err
	}
	// Prefer the local branch head; fall back to origin's tracking ref.
	if commit, err := gitOutput(addonDir, "rev-parse", "refs/heads/"+branch); err == nil {
		return &ResolvedRef{Commit: commit}, nil
	}
	if commit, err := gitOutput(addonDir, "rev-parse", "refs/remotes/origin/"+branch); err == nil {
		return &ResolvedRef{Commit: commit}, nil
	}
	return nil, fmt.Errorf("branch %q not found in %s; run 'git fetch' or check the branch name", branch, addonDir)
}

// LatestSignedTag returns the most recently created tag in the repo along
// with its resolved commit SHA.
//
// Phase 1 does not actually verify signatures; "latest signed tag" is
// aspirational. The function returns the latest tag regardless of signature
// status.
// TODO(signing): actually verify signatures once pkg/addons/signing.go lands.
func LatestSignedTag(addonDir string) (tag, commit string, err error) {
	// Sort order matters: git applies --sort flags in order, so later keys
	// are more significant. We want semver ordering to beat raw creation
	// time so that "v0.2.0 committed in the same second as v0.1.0" still
	// returns v0.2.0, but we fall back to creation order for tags without
	// a parseable version.
	out, err := gitOutput(addonDir, "for-each-ref",
		"--sort=-creatordate",
		"--sort=-v:refname",
		"--format=%(refname:short)",
		"refs/tags")
	if err != nil {
		return "", "", fmt.Errorf("listing tags in %s: %w", addonDir, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		commit, err := gitOutput(addonDir, "rev-parse", line+"^{}")
		if err != nil {
			continue
		}
		return line, commit, nil
	}
	return "", "", fmt.Errorf("no signed tags found in %s; specify --commit, --version, or --track=<branch>", addonDir)
}

func runGit(dir string, args ...string) error {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, msg)
	}
	return strings.TrimSpace(string(out)), nil
}
