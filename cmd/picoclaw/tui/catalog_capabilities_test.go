package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/models"
)

func TestTerminalUsesAdvertisedEfforts(t *testing.T) {
	metadata := map[string]models.ModelMetadata{"gpt-6.1-sol": {ReasoningLevels: []string{"medium", "max"}}}
	m := ModelsModel{modelName: "gpt-6.1-sol"}
	m.HandleCatalog(modelsCatalogMsg{metadata: metadata, models: []string{"gpt-6.1-sol"}})
	if strings.Join(m.availableEfforts(), ",") != "medium,max" {
		t.Fatal("terminal ignored advertised efforts")
	}
	s := SettingsModel{defaultModel: "gpt-6.1-sol", modelMetadata: metadata}
	if strings.Join(s.availableEfforts(), ",") != "medium,max" {
		t.Fatal("settings ignored advertised efforts")
	}
}

func TestTerminalEffortSelectionSurvivesCatalogShrink(t *testing.T) {
	m := ModelsModel{modelName: "gpt-6.1-sol", mode: modelsSetEffort, effortIdx: 4}
	m.HandleCatalog(modelsCatalogMsg{metadata: map[string]models.ModelMetadata{"gpt-6.1-sol": {ReasoningLevels: []string{"medium", "max"}}}})
	if m.effortIdx != 1 {
		t.Fatalf("selected max should survive list reorder: %d", m.effortIdx)
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if m.mode != modelsNormal || cmd == nil {
		t.Fatal("valid selection did not apply")
	}
	// Defensive Enter handling must also survive stale indices from other updates.
	m.mode = modelsSetEffort
	m.effortIdx = 99
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if m.effortIdx != 0 {
		t.Fatal("stale index not clamped")
	}
}

func TestTerminalEmptyCapabilitiesDisableEffort(t *testing.T) {
	metadata := map[string]models.ModelMetadata{"gpt-no-reasoning": {ReasoningLevels: []string{}}}
	m := ModelsModel{modelName: "gpt-no-reasoning", mode: modelsSetEffort, effortIdx: 4}
	m.HandleCatalog(modelsCatalogMsg{metadata: metadata})
	if len(m.availableEfforts()) != 0 {
		t.Fatal("terminal invented reasoning levels")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if m.mode != modelsNormal {
		t.Fatal("empty selector stayed open")
	}
	s := SettingsModel{defaultModel: "gpt-no-reasoning", modelMetadata: metadata}
	if len(s.availableEfforts()) != 0 {
		t.Fatal("settings invented reasoning levels")
	}
}
