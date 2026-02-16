package tui

import "github.com/charmbracelet/lipgloss"

// TokyoNight color palette (LazyVim default).
var (
	colorBg       = lipgloss.Color("#1a1b26") // night background
	colorBgDark   = lipgloss.Color("#16161e") // darker bg for status bar
	colorBgHL     = lipgloss.Color("#292e42") // cursor line / selection
	colorFg       = lipgloss.Color("#c0caf5") // main foreground
	colorComment  = lipgloss.Color("#565f89") // muted / comments
	colorDark3    = lipgloss.Color("#3b4261") // separator lines
	colorBlue     = lipgloss.Color("#7aa2f7")
	colorCyan     = lipgloss.Color("#7dcfff")
	colorGreen    = lipgloss.Color("#9ece6a")
	colorYellow   = lipgloss.Color("#e0af68")
	colorRed      = lipgloss.Color("#f7768e")
	colorMagenta  = lipgloss.Color("#bb9af7")
	colorOrange   = lipgloss.Color("#ff9e64")
	colorDimWhite = lipgloss.Color("#a9b1d6") // slightly dimmed fg
)

// -- Header / footer chrome --------------------------------------------------

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMagenta)

	headerDimStyle = lipgloss.NewStyle().
			Foreground(colorComment)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorComment)

	// The thin separator line between sections.
	separatorStyle = lipgloss.NewStyle().
			Foreground(colorDark3)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorComment)

	footerKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue)

	footerSepStyle = lipgloss.NewStyle().
			Foreground(colorDark3)
)

// -- Tree rows ---------------------------------------------------------------

var (
	selectedRowStyle = lipgloss.NewStyle().
				Background(colorBgHL).
				Foreground(colorFg)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(colorFg)

	repoNameStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	repoNameErrStyle = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)

	repoCountStyle = lipgloss.NewStyle().
			Foreground(colorComment)
)

// -- Bead pipeline -----------------------------------------------------------

var (
	beadDone    = lipgloss.NewStyle().Foreground(colorGreen)
	beadActive  = lipgloss.NewStyle().Foreground(colorYellow)
	beadPending = lipgloss.NewStyle().Foreground(colorComment)
	beadFailed  = lipgloss.NewStyle().Foreground(colorRed)
	beadPaused  = lipgloss.NewStyle().Foreground(colorOrange)
	beadLine    = lipgloss.NewStyle().Foreground(colorDark3)
	beadLabel   = lipgloss.NewStyle().Foreground(colorComment)
)

// -- Issue status badges -----------------------------------------------------

var (
	statusReadyStyle     = lipgloss.NewStyle().Foreground(colorGreen)
	statusReadyBoldStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	statusRunningStyle   = lipgloss.NewStyle().Foreground(colorYellow)
	statusFailedStyle    = lipgloss.NewStyle().Foreground(colorRed)
	statusReactedStyle   = lipgloss.NewStyle().Foreground(colorBlue)
	statusPausedStyle    = lipgloss.NewStyle().Foreground(colorOrange)
)

// -- Log lines ---------------------------------------------------------------

var (
	logLineStyle = lipgloss.NewStyle().
			Foreground(colorComment)

	logLineActiveStyle = lipgloss.NewStyle().
				Foreground(colorDimWhite)
)

// -- Dialog ------------------------------------------------------------------

var (
	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMagenta).
			Padding(1, 2).
			Width(70)

	dialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMagenta)

	dialogLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue)
)
