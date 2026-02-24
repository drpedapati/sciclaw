//go:build darwin

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type launchdStartTestRunner struct {
	printCalls int
}

func (r *launchdStartTestRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "launchctl" || len(args) == 0 {
		return nil, nil
	}

	switch args[0] {
	case "print":
		r.printCalls++
		if r.printCalls == 1 {
			return nil, errors.New("service not loaded")
		}
		return []byte("state = running\npid = 4242\n"), nil
	case "bootstrap", "enable":
		return nil, nil
	case "kickstart":
		return nil, errors.New("exit status 1")
	default:
		return nil, nil
	}
}

func TestLaunchdStartNormalizesKickstartFailureWhenServiceRunning(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "io.sciclaw.gateway.plist")
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0644); err != nil {
		t.Fatalf("seed plist: %v", err)
	}

	runner := &launchdStartTestRunner{}
	mgr := &launchdManager{
		runner:        runner,
		domainTarget:  "gui/501",
		serviceTarget: "gui/501/io.sciclaw.gateway",
		plistPath:     plistPath,
	}

	if err := mgr.Start(); err != nil {
		t.Fatalf("Start() = %v, want success", err)
	}
}

// launchdInstallTestRunner records launchctl subcommands to verify ordering.
type launchdInstallTestRunner struct {
	calls []string
}

func (r *launchdInstallTestRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "launchctl" && len(args) > 0 {
		r.calls = append(r.calls, strings.Join(append([]string{args[0]}, args[1:]...), " "))
	}
	return nil, nil
}

func TestLaunchdInstallIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "LaunchAgents", "io.sciclaw.gateway.plist")
	logDir := filepath.Join(tmp, "logs")

	// Seed an existing plist to simulate a prior installation.
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(plistPath, []byte("<old/>"), 0644); err != nil {
		t.Fatalf("seed plist: %v", err)
	}

	runner := &launchdInstallTestRunner{}
	mgr := &launchdManager{
		runner:        runner,
		exePath:       "/tmp/test-binary",
		label:         "io.sciclaw.gateway",
		domainTarget:  "gui/501",
		serviceTarget: "gui/501/io.sciclaw.gateway",
		plistPath:     plistPath,
		stdoutPath:    filepath.Join(logDir, "gateway.log"),
		stderrPath:    filepath.Join(logDir, "gateway.err.log"),
	}

	if err := mgr.Install(); err != nil {
		t.Fatalf("Install() = %v", err)
	}

	// Plist must exist and be freshly written (not the old content).
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if strings.Contains(string(data), "<old/>") {
		t.Fatalf("plist was not overwritten")
	}

	// Verify Uninstall ran first (bootout + enable) before bootstrap.
	if len(runner.calls) < 3 {
		t.Fatalf("expected at least 3 launchctl calls, got %d: %v", len(runner.calls), runner.calls)
	}
	// Uninstall does: bootout, enable. Install then does: bootstrap, enable.
	foundBootout := false
	foundBootstrap := false
	for _, c := range runner.calls {
		if strings.HasPrefix(c, "bootout") {
			foundBootout = true
		}
		if strings.HasPrefix(c, "bootstrap") {
			if !foundBootout {
				t.Fatalf("bootstrap called before bootout — not idempotent")
			}
			foundBootstrap = true
		}
	}
	if !foundBootout {
		t.Fatalf("expected bootout call (from Uninstall), calls: %v", runner.calls)
	}
	if !foundBootstrap {
		t.Fatalf("expected bootstrap call, calls: %v", runner.calls)
	}
}

// launchdRetryTestRunner fails bootstrap on the first attempt, succeeds on retry.
type launchdRetryTestRunner struct {
	bootstrapCalls int
}

func (r *launchdRetryTestRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" {
		r.bootstrapCalls++
		if r.bootstrapCalls == 1 {
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
	}
	return nil, nil
}

func TestLaunchdInstallRetriesBootstrapOnTransientFailure(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "LaunchAgents", "io.sciclaw.gateway.plist")
	logDir := filepath.Join(tmp, "logs")

	runner := &launchdRetryTestRunner{}
	mgr := &launchdManager{
		runner:        runner,
		exePath:       "/tmp/test-binary",
		label:         "io.sciclaw.gateway",
		domainTarget:  "gui/501",
		serviceTarget: "gui/501/io.sciclaw.gateway",
		plistPath:     plistPath,
		stdoutPath:    filepath.Join(logDir, "gateway.log"),
		stderrPath:    filepath.Join(logDir, "gateway.err.log"),
	}

	if err := mgr.Install(); err != nil {
		t.Fatalf("Install() should succeed on retry, got: %v", err)
	}
	if runner.bootstrapCalls != 2 {
		t.Fatalf("expected 2 bootstrap attempts, got %d", runner.bootstrapCalls)
	}
}

