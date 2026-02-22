package tui

import "testing"

func TestAgentModel_HandleServiceAction(t *testing.T) {
	m := NewAgentModel(&routingTestExec{home: "/Users/tester"})
	m.HandleServiceAction(serviceActionMsg{
		action: "start",
		ok:     true,
		output: "service started",
	})

	if !m.logsLoaded {
		t.Fatal("logsLoaded = false, want true")
	}
	if got := m.logsViewport.View(); got == "" {
		t.Fatal("logs viewport content is empty")
	}
}

