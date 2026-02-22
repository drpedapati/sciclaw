package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — clean, calm, scientific.
var (
	colorAccent  = lipgloss.Color("#7B68EE") // medium slate blue
	colorSuccess = lipgloss.Color("#50C878") // emerald
	colorWarning = lipgloss.Color("#FFB347") // pastel orange
	colorError   = lipgloss.Color("#FF6961") // pastel red
	colorMuted   = lipgloss.Color("#808080") // gray
	colorBorder  = lipgloss.Color("#3A3A5C") // subtle border
	colorTitle   = lipgloss.Color("#C4B5FD") // light purple for titles
)

// Tab bar styles.
var (
	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorAccent).
			Padding(0, 2)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 2)

	styleTabBar = lipgloss.NewStyle().
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottomForeground(colorBorder).
			MarginBottom(1)
)

// Panel styles.
var (
	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			MarginBottom(1)

	stylePanelTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTitle).
			Padding(0, 1)

	styleSuggestedPanel = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(0, 1).
				MarginBottom(1)
)

// Status indicator styles.
var (
	styleOK   = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	styleErr  = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	styleDim  = lipgloss.NewStyle().Foreground(colorMuted)
)

// Text styles.
var (
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleLabel  = lipgloss.NewStyle().Foreground(colorMuted).Width(12)
	styleValue  = lipgloss.NewStyle().Bold(true)
	styleHint   = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	styleBadge  = lipgloss.NewStyle().Padding(0, 1).Bold(true)
	styleKey    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorTitle).MarginBottom(1)
)

// Status bar (bottom of screen).
var styleStatusBar = lipgloss.NewStyle().
	Foreground(colorMuted).
	Padding(0, 1)

// Badge renders.
func badgeReady() string {
	return styleBadge.Foreground(colorSuccess).Render("[Ready]")
}

func badgeNotReady() string {
	return styleBadge.Foreground(colorError).Render("[Not Ready]")
}

func badgeWarning() string {
	return styleBadge.Foreground(colorWarning).Render("[Needs Attention]")
}

func statusIcon(status string) string {
	switch status {
	case "ready", "yes", "ok", "active", "running":
		return styleOK.Render("✓")
	case "open", "partial", "warn", "needs refresh":
		return styleWarn.Render("!")
	case "missing", "no", "error", "expired", "broken", "off":
		return styleErr.Render("✗")
	default:
		return styleDim.Render("•")
	}
}
