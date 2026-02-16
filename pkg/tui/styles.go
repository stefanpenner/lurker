package tui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha-inspired palette.
var (
	colorBase     = lipgloss.Color("#1e1e2e")
	colorSurface0 = lipgloss.Color("#313244")
	colorSurface1 = lipgloss.Color("#45475a")
	colorText     = lipgloss.Color("#cdd6f4")
	colorSubtext  = lipgloss.Color("#a6adc8")
	colorGreen    = lipgloss.Color("#a6e3a1")
	colorYellow   = lipgloss.Color("#f9e2af")
	colorRed      = lipgloss.Color("#f38ba8")
	colorBlue     = lipgloss.Color("#89b4fa")
	colorMauve    = lipgloss.Color("#cba6f7")
	colorTeal     = lipgloss.Color("#94e2d5")
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMauve).
			PaddingLeft(1).
			PaddingRight(1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			PaddingLeft(1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue).
				PaddingLeft(1)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorSurface1).
				Foreground(colorText)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(1)

	logHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTeal).
			PaddingLeft(1)

	logLineStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			PaddingLeft(2)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorSurface1).
			PaddingLeft(1)

	borderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorSurface0)

	statusReadyStyle   = lipgloss.NewStyle().Foreground(colorGreen)
	statusRunningStyle = lipgloss.NewStyle().Foreground(colorYellow)
	statusFailedStyle  = lipgloss.NewStyle().Foreground(colorRed)
	statusReactedStyle = lipgloss.NewStyle().Foreground(colorBlue)
)
