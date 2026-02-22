package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// RunVM launches the TUI in VM mode (managing a multipass VM).
func RunVM() {
	run(&VMExecutor{})
}

// RunLocal launches the TUI in local mode (direct host execution).
func RunLocal() {
	run(NewLocalExecutor())
}

// RunAuto detects whether a multipass VM is available and picks the
// appropriate mode automatically. Falls back to local mode.
func RunAuto() {
	info := GetVMInfo()
	if info.State != "NotFound" && info.State != "" {
		run(&VMExecutor{})
	} else {
		run(NewLocalExecutor())
	}
}

func run(exec Executor) {
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
