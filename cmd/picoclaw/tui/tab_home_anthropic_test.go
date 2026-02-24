package tui

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type testExecForConfig struct {
	mode               Mode
	configData         []byte
	readError          error
	interactiveExitErr bool
}

func (e *testExecForConfig) Mode() Mode { return e.mode }
func (e *testExecForConfig) ExecShell(_ time.Duration, _ string) (string, error) {
	return "", nil
}
func (e *testExecForConfig) ExecCommand(_ time.Duration, _ ...string) (string, error) {
	return "", nil
}
func (e *testExecForConfig) ReadFile(_ string) (string, error) {
	if e.readError != nil {
		return "", e.readError
	}
	return string(e.configData), nil
}
func (e *testExecForConfig) WriteFile(_ string, data []byte, _ os.FileMode) error {
	e.configData = data
	return nil
}
func (e *testExecForConfig) ConfigPath() string { return "/tmp/config.json" }
func (e *testExecForConfig) AuthPath() string   { return "/tmp/auth.json" }
func (e *testExecForConfig) HomePath() string   { return "/tmp" }
func (e *testExecForConfig) BinaryPath() string { return "sciclaw" }
func (e *testExecForConfig) AgentVersion() string {
	return "vtest"
}
func (e *testExecForConfig) ServiceInstalled() bool { return false }
func (e *testExecForConfig) ServiceActive() bool    { return false }
func (e *testExecForConfig) InteractiveProcess(_ ...string) *exec.Cmd {
	if e.interactiveExitErr {
		return exec.Command("sh", "-c", "exit 1")
	}
	return exec.Command("true")
}

func (e *testExecForConfig) setConfigMap(cfg map[string]interface{}) {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	e.configData = append(data, '\n')
}

func (e *testExecForConfig) readConfigMapForTest(t *testing.T) map[string]interface{} {
	cfg, err := readConfigMap(e)
	if err != nil {
		t.Fatalf("readConfigMap failed: %v", err)
	}
	return cfg
}

func TestResolveSmokeTestModelPrefersAnthropicForGptModel(t *testing.T) {
	exec := &testExecForConfig{
		mode: ModeLocal,
	}
	exec.setConfigMap(map[string]interface{}{
		"agents": map[string]interface{}{
			"defaults": map[string]interface{}{
				"model": "gpt-5.2",
			},
		},
		"providers": map[string]interface{}{
			"anthropic": map[string]interface{}{
				"api_key": "ops-should-work",
			},
		},
	})

	model, err := resolveSmokeTestModel(exec)
	if err != nil {
		t.Fatalf("resolveSmokeTestModel error: %v", err)
	}
	if model != anthropicDefaultModel {
		t.Fatalf("model=%q, want %q", model, anthropicDefaultModel)
	}
}

func TestResolveSmokeTestModelKeepsOpenAIForOpenAIModel(t *testing.T) {
	exec := &testExecForConfig{
		mode: ModeLocal,
	}
	exec.setConfigMap(map[string]interface{}{
		"agents": map[string]interface{}{
			"defaults": map[string]interface{}{
				"model": "gpt-5.2",
			},
		},
		"providers": map[string]interface{}{
			"openai": map[string]interface{}{
				"api_key": "sk-openai-key",
			},
		},
	})

	model, err := resolveSmokeTestModel(exec)
	if err != nil {
		t.Fatalf("resolveSmokeTestModel error: %v", err)
	}
	if model != "" {
		t.Fatalf("model=%q, want empty", model)
	}
}

func TestSaveAnthropicKeySetsDefaultModel(t *testing.T) {
	exec := &testExecForConfig{mode: ModeLocal}
	exec.setConfigMap(map[string]interface{}{
		"agents": map[string]interface{}{
			"defaults": map[string]interface{}{
				"model": "gpt-5.2",
			},
		},
		"providers": map[string]interface{}{},
	})

	if err := saveAPIKey(exec, "anthropic", "ops-token"); err != nil {
		t.Fatalf("saveAPIKey error: %v", err)
	}

	cfg := exec.readConfigMapForTest(t)
	agents := mapValue(cfg, "agents")
	defaults := mapValue(agents, "defaults")
	if got := asString(defaults["model"]); got != anthropicDefaultModel {
		t.Fatalf("defaults.model=%q, want %q", got, anthropicDefaultModel)
	}
	providers := mapValue(cfg, "providers")
	anthropic := mapValue(providers, "anthropic")
	if got := strings.TrimSpace(asString(anthropic["api_key"])); got != "ops-token" {
		t.Fatalf("anthropic api_key=%q, want ops-token", got)
	}
}
