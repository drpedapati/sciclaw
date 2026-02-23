//go:build darwin

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

