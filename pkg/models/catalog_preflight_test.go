package models

import (
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestPreflightExplicitSolMediumSurvivesConfigReload(t *testing.T) {
	cfg := config.DefaultConfig()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SetModel(cfg, path, "gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	if err := SetEffort(cfg, path, "medium"); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agents.Defaults.Model != "gpt-6.1-sol" || loaded.Agents.Defaults.ReasoningEffort != "medium" || loaded.Agents.Defaults.Provider != "openai" {
		t.Fatal("explicit selection lost on reload")
	}
}
