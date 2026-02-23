package main

import (
	"errors"
	"os"
	"testing"

	svcmgr "github.com/sipeed/picoclaw/pkg/service"
)

func TestParseServiceLogsOptions(t *testing.T) {
	opts, showHelp, err := parseServiceLogsOptions([]string{"--lines", "250"})
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if opts.Lines != 250 {
		t.Fatalf("expected Lines=250, got %d", opts.Lines)
	}
}

func TestParseServiceLogsOptionsDefault(t *testing.T) {
	opts, showHelp, err := parseServiceLogsOptions(nil)
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if opts.Lines != 100 {
		t.Fatalf("expected default Lines=100, got %d", opts.Lines)
	}
}

func TestParseServiceLogsOptionsHelp(t *testing.T) {
	_, showHelp, err := parseServiceLogsOptions([]string{"--help"})
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if !showHelp {
		t.Fatalf("expected showHelp=true")
	}
}

func TestParseServiceLogsOptionsInvalid(t *testing.T) {
	_, _, err := parseServiceLogsOptions([]string{"--lines", "0"})
	if err == nil {
		t.Fatalf("expected error for invalid line count")
	}
}

func TestResolveServiceExecutablePath_PrefersExplicitArgv0Path(t *testing.T) {
	got, err := resolveServiceExecutablePath(
		"/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.1.39/bin/sciclaw",
		func(file string) (string, error) {
			t.Fatalf("lookPath should not be called for explicit argv0 path")
			return "", nil
		},
		func() (string, error) {
			t.Fatalf("executable fallback should not be called for explicit argv0 path")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveServiceExecutablePath returned error: %v", err)
	}
	if got != "/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.1.39/bin/sciclaw" {
		t.Fatalf("expected argv0 path, got %q", got)
	}
}

func TestResolveServiceExecutablePath_PrefersLookPathForBareCommand(t *testing.T) {
	got, err := resolveServiceExecutablePath(
		"sciclaw",
		func(file string) (string, error) {
			if file != "sciclaw" {
				t.Fatalf("expected lookup for sciclaw, got %q", file)
			}
			return "/home/linuxbrew/.linuxbrew/bin/sciclaw", nil
		},
		func() (string, error) {
			t.Fatalf("executable fallback should not be called when lookPath succeeds")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveServiceExecutablePath returned error: %v", err)
	}
	if got != "/home/linuxbrew/.linuxbrew/bin/sciclaw" {
		t.Fatalf("expected lookPath result, got %q", got)
	}
}

func TestResolveServiceExecutablePath_FallbackToExecutable(t *testing.T) {
	got, err := resolveServiceExecutablePath(
		"sciclaw",
		func(string) (string, error) { return "", errors.New("not found") },
		func() (string, error) { return "/opt/tools/sciclaw", nil },
	)
	if err != nil {
		t.Fatalf("resolveServiceExecutablePath returned error: %v", err)
	}
	if got != "/opt/tools/sciclaw" {
		t.Fatalf("expected executable fallback, got %q", got)
	}
}

func TestPrependPathEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	prependPathEnv("/custom/bin")
	if got := os.Getenv("PATH"); got != "/custom/bin:/usr/bin:/bin" {
		t.Fatalf("unexpected PATH after prepend: %q", got)
	}
}

func TestPrependPathEnv_Dedupes(t *testing.T) {
	t.Setenv("PATH", "/custom/bin:/usr/bin:/bin")
	prependPathEnv("/custom/bin")
	if got := os.Getenv("PATH"); got != "/custom/bin:/usr/bin:/bin" {
		t.Fatalf("unexpected PATH with duplicate prepend: %q", got)
	}
}

type fakeServiceManager struct {
	installed  bool
	restarted  bool
	installErr error
	restartErr error
}

func (m *fakeServiceManager) Backend() string { return svcmgr.BackendSystemdUser }
func (m *fakeServiceManager) Install() error {
	m.installed = true
	return m.installErr
}
func (m *fakeServiceManager) Uninstall() error { return nil }
func (m *fakeServiceManager) Start() error     { return nil }
func (m *fakeServiceManager) Stop() error      { return nil }
func (m *fakeServiceManager) Restart() error {
	m.restarted = true
	return m.restartErr
}
func (m *fakeServiceManager) Status() (svcmgr.Status, error) { return svcmgr.Status{}, nil }
func (m *fakeServiceManager) Logs(int) (string, error)       { return "", nil }

func TestRunServiceRefresh(t *testing.T) {
	mgr := &fakeServiceManager{}
	if err := runServiceRefresh(mgr); err != nil {
		t.Fatalf("runServiceRefresh returned error: %v", err)
	}
	if !mgr.installed {
		t.Fatalf("expected Install to be called")
	}
	if !mgr.restarted {
		t.Fatalf("expected Restart to be called")
	}
}
