package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultGHCRImage = "ghcr.io/drpedapati/sciclaw"
const githubRawBaseURL = "https://raw.githubusercontent.com/drpedapati/sciclaw"

type vmHelperFiles struct {
	scriptPath    string
	templatePath  string
	toolchainPath string
	cleanupDir    string
}

func vmCmd() {
	helper, err := resolveVMHelperFiles()
	if err != nil {
		fmt.Printf("Error: VM helper setup failed: %v\n", err)
		os.Exit(1)
	}
	if helper.cleanupDir != "" {
		defer os.RemoveAll(helper.cleanupDir)
	}

	cmd := exec.Command("bash", append([]string{helper.scriptPath}, os.Args[2:]...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"SCICLAW_VM_CLOUD_INIT_TEMPLATE="+helper.templatePath,
		"SCICLAW_VM_TOOLCHAIN_ENV="+helper.toolchainPath,
		"SCICLAW_VM_CMD_LABEL="+invokedCLIName()+" vm",
	)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Printf("Error: failed to execute VM helper: %v\n", err)
		os.Exit(1)
	}
}

func dockerCmd() {
	args := os.Args[2:]
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printDockerHelp()
		return
	}

	image := defaultContainerImageRef()
	sub := args[0]
	switch sub {
	case "pull":
		if len(args) > 1 {
			image = args[1]
		}
		runAndExit("docker", "pull", image)
	case "doctor":
		if len(args) > 1 {
			image = args[1]
		}
		runAndExit("docker", "run", "--rm", image, "doctor")
	case "gateway":
		if len(args) > 1 {
			image = args[1]
		}
		home, _ := os.UserHomeDir()
		workspace := filepath.Join(home, "sciclaw")
		configHome := filepath.Join(home, ".picoclaw")
		_ = os.MkdirAll(workspace, 0755)
		_ = os.MkdirAll(configHome, 0755)

		dockerArgs := []string{
			"run", "--rm", "-it",
			"-v", workspace + ":/root/sciclaw",
			"-v", configHome + ":/root/.picoclaw",
			"-p", "8080:8080",
			image,
			"gateway",
		}
		runAndExit("docker", dockerArgs...)
	default:
		// passthrough mode: `sciclaw docker <docker-args...>`
		runAndExit("docker", args...)
	}
}

func printDockerHelp() {
	image := defaultContainerImageRef()
	fmt.Println("Usage: sciclaw docker <subcommand>")
	fmt.Println("")
	fmt.Println("Subcommands:")
	fmt.Println("  pull [image]      Pull sciClaw container image")
	fmt.Println("  doctor [image]    Run doctor in a disposable container")
	fmt.Println("  gateway [image]   Run gateway with ~/sciclaw and ~/.picoclaw mounted")
	fmt.Println("")
	fmt.Println("Passthrough:")
	fmt.Println("  sciclaw docker <any docker args...>")
	fmt.Println("")
	fmt.Println("Default image:")
	fmt.Printf("  %s\n", image)
}

func runAndExit(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Printf("Error: failed to execute %s: %v\n", name, err)
		os.Exit(1)
	}
}

func resolveVMHelperFiles() (vmHelperFiles, error) {
	// 1) Use explicitly provided helper paths.
	if root := strings.TrimSpace(os.Getenv("SCICLAW_VM_HELPER_DIR")); root != "" {
		if hf, ok := helperFromRoot(root); ok {
			return hf, nil
		}
	}

	// 2) Use local repo helper if present.
	if wd, err := os.Getwd(); err == nil {
		if hf, ok := helperFromRoot(filepath.Join(wd, "deploy")); ok {
			return hf, nil
		}
	}

	// 3) Use packaged Homebrew share helper if present.
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if hf, ok := helperFromRoot(filepath.Clean(filepath.Join(exeDir, "..", "share", "sciclaw", "deploy"))); ok {
			return hf, nil
		}
		if hf, ok := helperFromRoot(filepath.Clean(filepath.Join(exeDir, "..", "share", "picoclaw", "deploy"))); ok {
			return hf, nil
		}
	}

	// 4) Fallback: download helper artifacts from GitHub for this version.
	tmpDir, err := os.MkdirTemp("", "sciclaw-vm-helper-*")
	if err != nil {
		return vmHelperFiles{}, err
	}

	scriptPath := filepath.Join(tmpDir, "vm")
	templatePath := filepath.Join(tmpDir, "multipass-cloud-init.yaml")
	toolchainPath := filepath.Join(tmpDir, "toolchain.env")

	var lastErr error
	for _, ref := range helperCandidateRefs() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(templatePath)
		_ = os.Remove(toolchainPath)

		if err := downloadRawFile(ref, "deploy/vm", scriptPath, 0755); err != nil {
			lastErr = err
			continue
		}
		if err := downloadRawFile(ref, "deploy/multipass-cloud-init.yaml", templatePath, 0644); err != nil {
			lastErr = err
			continue
		}
		if err := downloadRawFile(ref, "deploy/toolchain.env", toolchainPath, 0644); err != nil {
			lastErr = err
			continue
		}
		return vmHelperFiles{
			scriptPath:    scriptPath,
			templatePath:  templatePath,
			toolchainPath: toolchainPath,
			cleanupDir:    tmpDir,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("unable to resolve helper artifacts")
	}
	_ = os.RemoveAll(tmpDir)
	return vmHelperFiles{}, lastErr
}

func helperFromRoot(root string) (vmHelperFiles, bool) {
	scriptPath := filepath.Join(root, "vm")
	templatePath := filepath.Join(root, "multipass-cloud-init.yaml")
	toolchainPath := filepath.Join(root, "toolchain.env")
	if fileExists(scriptPath) && fileExists(templatePath) && fileExists(toolchainPath) {
		return vmHelperFiles{
			scriptPath:    scriptPath,
			templatePath:  templatePath,
			toolchainPath: toolchainPath,
		}, true
	}
	return vmHelperFiles{}, false
}

func helperGitRef() string {
	v := strings.TrimSpace(version)
	if v == "" || v == "dev" {
		return "main"
	}
	if strings.Contains(strings.ToLower(v), "dev") {
		return "development"
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func helperCandidateRefs() []string {
	refs := []string{helperGitRef()}
	for _, extra := range []string{"development", "main"} {
		found := false
		for _, r := range refs {
			if r == extra {
				found = true
				break
			}
		}
		if !found {
			refs = append(refs, extra)
		}
	}
	return refs
}

func downloadRawFile(ref, relPath, dst string, mode os.FileMode) error {
	url := fmt.Sprintf("%s/%s/%s", githubRawBaseURL, ref, relPath)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", relPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %d", relPath, resp.StatusCode)
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

func defaultContainerImageRef() string {
	v := strings.TrimSpace(version)
	if v != "" && v != "dev" && !strings.Contains(strings.ToLower(v), "dev") {
		return fmt.Sprintf("%s:v%s", defaultGHCRImage, strings.TrimPrefix(v, "v"))
	}
	// development is published for integration flows.
	return defaultGHCRImage + ":development"
}
