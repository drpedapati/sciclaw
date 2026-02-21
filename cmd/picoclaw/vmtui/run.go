package vmtui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the VM Control Center TUI.
func Run() {
	p := tea.NewProgram(
		NewModel(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running VM Control Center: %v\n", err)
		os.Exit(1)
	}
}
