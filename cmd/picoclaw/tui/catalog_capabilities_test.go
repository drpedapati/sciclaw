package tui

import (
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
