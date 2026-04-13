package addons

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// HashFile returns the hex SHA256 of the file's contents. Streams the file
// with io.Copy so large binaries don't balloon memory.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashPath returns a deterministic SHA256 hash for either a regular file or a
// directory. Directories are walked in sorted order and each file's relative
// path plus contents are folded into a single hash so filesystem walk order
// does not affect the result.
//
// The .git/ subtree is skipped. Symlinks are skipped with a warning written
// to stderr; addons should not rely on symlinks in hashed paths.
func HashPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "addons: warning: symlink at %s skipped for hashing\n", path)
		return "", fmt.Errorf("%s is a symlink; refusing to hash", path)
	}
	if info.Mode().IsRegular() {
		return HashFile(path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is neither a regular file nor a directory", path)
	}

	type entry struct {
		rel, abs string
	}
	var files []entry
	walkErr := filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if fi.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "addons: warning: symlink at %s skipped for hashing\n", p)
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		// Normalize to forward slashes so the hash is OS-independent.
		rel = filepath.ToSlash(rel)
		files = append(files, entry{rel: rel, abs: p})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walking %s: %w", path, walkErr)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	outer := sha256.New()
	for _, e := range files {
		inner, err := HashFile(e.abs)
		if err != nil {
			return "", err
		}
		// Fold "<relpath>\0<contenthash>\n" into the outer hash; the NUL
		// separator prevents ambiguity for paths that contain newlines.
		fmt.Fprintf(outer, "%s\x00%s\n", e.rel, inner)
	}
	return hex.EncodeToString(outer.Sum(nil)), nil
}

// VerifyEntry checks that the installed addon directory matches the registry
// entry's pinned commit and content hashes. Returns nil on success or a
// multi-line error enumerating all failed checks.
func VerifyEntry(addonDir string, entry *RegistryEntry, manifestPath, bootstrapPath, sidecarPath string) error {
	if entry == nil {
		return fmt.Errorf("VerifyEntry: registry entry is nil")
	}
	var problems []string

	head, err := gitHeadCommit(addonDir)
	switch {
	case err != nil:
		problems = append(problems, fmt.Sprintf("git HEAD: %v", err))
	case head != entry.InstalledCommit:
		problems = append(problems,
			fmt.Sprintf("commit drift: expected installed_commit=%s, actual HEAD=%s",
				entry.InstalledCommit, head))
	}

	if manifestPath != "" && entry.ManifestSHA256 != "" {
		got, err := HashFile(manifestPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("hashing manifest: %v", err))
		} else if got != entry.ManifestSHA256 {
			problems = append(problems,
				fmt.Sprintf("manifest_sha256 mismatch: expected %s, got %s",
					entry.ManifestSHA256, got))
		}
	}

	if bootstrapPath != "" && entry.BootstrapSHA256 != "" {
		got, err := HashPath(bootstrapPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("hashing bootstrap: %v", err))
		} else if got != entry.BootstrapSHA256 {
			problems = append(problems,
				fmt.Sprintf("bootstrap_sha256 mismatch: expected %s, got %s",
					entry.BootstrapSHA256, got))
		}
	}

	if sidecarPath != "" && entry.SidecarSHA256 != "" {
		got, err := HashFile(sidecarPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("hashing sidecar: %v", err))
		} else if got != entry.SidecarSHA256 {
			problems = append(problems,
				fmt.Sprintf("sidecar_sha256 mismatch: expected %s, got %s",
					entry.SidecarSHA256, got))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("addon integrity check failed for %s:\n  - %s\nrun 'sciclaw addon verify' or 'sciclaw addon upgrade' to resolve",
		addonDir, strings.Join(problems, "\n  - "))
}

// ComputeHashes produces the hashes needed for a fresh registry entry.
// sidecarSHA is empty if the sidecar binary is not present on disk yet
// (e.g., addon built from source as part of install).
func ComputeHashes(addonDir string, manifest *Manifest) (manifestSHA, bootstrapSHA, sidecarSHA string, err error) {
	manifestPath := filepath.Join(addonDir, "addon.json")
	manifestSHA, err = HashFile(manifestPath)
	if err != nil {
		return "", "", "", fmt.Errorf("hashing addon.json: %w", err)
	}

	if manifest != nil && strings.TrimSpace(manifest.Bootstrap.Install) != "" {
		bp := resolveUnder(addonDir, manifest.Bootstrap.Install)
		if _, statErr := os.Lstat(bp); statErr == nil {
			h, herr := HashPath(bp)
			if herr != nil {
				return "", "", "", fmt.Errorf("hashing bootstrap: %w", herr)
			}
			bootstrapSHA = h
		}
	}

	if manifest != nil && strings.TrimSpace(manifest.Sidecar.Binary) != "" {
		sp := resolveUnder(addonDir, manifest.Sidecar.Binary)
		// Common layout: sidecar binary lives under bin/<name>.
		if _, statErr := os.Lstat(sp); statErr != nil {
			alt := filepath.Join(addonDir, "bin", manifest.Sidecar.Binary)
			if _, altErr := os.Lstat(alt); altErr == nil {
				sp = alt
			}
		}
		if info, statErr := os.Lstat(sp); statErr == nil && info.Mode().IsRegular() {
			h, herr := HashFile(sp)
			if herr != nil {
				return "", "", "", fmt.Errorf("hashing sidecar: %w", herr)
			}
			sidecarSHA = h
		}
	}

	return manifestSHA, bootstrapSHA, sidecarSHA, nil
}

// resolveUnder resolves a user-specified relative path (like "./bin/install.sh")
// under addonDir. Absolute paths are returned unchanged so an addon can
// deliberately reference a system path if it ever needs to.
func resolveUnder(addonDir, p string) string {
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(addonDir, filepath.FromSlash(p))
}

// gitHeadCommit returns the current HEAD commit SHA of a git working tree.
func gitHeadCommit(addonDir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git not found on PATH: %w", err)
	}
	cmd := exec.Command("git", "-C", addonDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", addonDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}
