package tui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha-inspired palette.
var (
	colorBase     = lipgloss.Color("#1e1e2e")
	colorSurface0 = lipgloss.Color("#313244")
	colorSurface1 = lipgloss.Color("#45475a")
	colorOverlay0 = lipgloss.Color("#6c7086")
	colorText     = lipgloss.Color("#cdd6f4")
	colorSubtext  = lipgloss.Color("#a6adc8")
	colorGreen    = lipgloss.Color("#a6e3a1")
	colorYellow   = lipgloss.Color("#f9e2af")
	colorRed      = lipgloss.Color("#f38ba8")
	colorBlue     = lipgloss.Color("#89b4fa")
	colorMauve    = lipgloss.Color("#cba6f7")
	colorTeal     = lipgloss.Color("#94e2d5")
	colorPeach    = lipgloss.Color("#fab387")
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMauve).
			PaddingLeft(1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			PaddingLeft(1)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorSurface1).
				Foreground(colorText)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(colorText)

	logLineStyle = lipgloss.NewStyle().
			Foreground(colorOverlay0)

	logLineActiveStyle = lipgloss.NewStyle().
				Foreground(colorSubtext)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorSurface1).
			PaddingLeft(1)

	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMauve).
			Padding(1, 2).
			Width(70)

	dialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMauve)

	dialogLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue)

	statusReadyStyle     = lipgloss.NewStyle().Foreground(colorGreen)
	statusReadyBoldStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	statusRunningStyle = lipgloss.NewStyle().Foreground(colorYellow)
	statusFailedStyle  = lipgloss.NewStyle().Foreground(colorRed)
	statusReactedStyle = lipgloss.NewStyle().Foreground(colorBlue)
	statusPausedStyle  = lipgloss.NewStyle().Foreground(colorPeach)
)
