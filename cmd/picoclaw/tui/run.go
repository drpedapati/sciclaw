package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// RunVM launches the TUI in VM mode (managing a multipass VM).
func RunVM() {
	exec := &VMExecutor{}
	p := tea.NewProgram(
		NewModel(exec),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running Control Center: %v\n", err)
		os.Exit(1)
	}
}

// RunLocal launches the TUI in local mode (direct host execution).
func RunLocal() {
	exec := NewLocalExecutor()
	p := tea.NewProgram(
		NewModel(exec),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running Control Center: %v\n", err)
		os.Exit(1)
	}
}
