package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/stefanpenner/lurker/pkg/watcher"
)

// hyperlink wraps text in an OSC 8 terminal hyperlink.
func hyperlink(url, text string) string {
	if url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x07%s\x1b]8;;\x07", url, text)
}

// padOrTruncate pads s (right) or truncates it to exactly width visual columns.
func padOrTruncate(s string, width int) string {
	w := lipgloss.Width(s)
	if w == width {
		return s
	}
	if w > width {
		// Brute truncate: remove runes from end until we fit
		runes := []rune(s)
		for lipgloss.Width(string(runes)) > width && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		truncated := string(runes)
		// pad if we overshot
		if lipgloss.Width(truncated) < width {
			truncated += strings.Repeat(" ", width-lipgloss.Width(truncated))
		}
		return truncated
	}
	return s + strings.Repeat(" ", width-w)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if m.focus == focusFocus && m.focusIssue != nil {
		return m.renderFocusView()
	}

	var b strings.Builder

	// Header bar
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Status bar (counts)
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// Tree list with inline logs
	b.WriteString(m.renderTree())

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// Footer
	b.WriteString(m.renderFooter())

	// Dialog overlay
	if m.focus == focusDialog && m.dialogIssue != nil {
		return m.renderWithDialog(b.String())
	}

	// Confirm overlay
	if m.focus == focusConfirm && m.confirmRepo != "" {
		return m.renderConfirmDialog()
	}

	// Help overlay
	if m.focus == focusHelp {
		return m.renderHelpScreen()
	}

	return b.String()
}

func (m Model) renderHeader() string {
	// Left side:  lurker  <repo count>
	title := headerStyle.Render("lurker")

	repos := m.manager.Repos()
	var repoStr string
	if len(repos) == 0 {
		repoStr = headerDimStyle.Render("no repos -- press r to add")
	} else {
		repoStr = headerDimStyle.Render(fmt.Sprintf("%d repos", len(repos)))
	}

	left := fmt.Sprintf(" %s  %s", title, repoStr)

	// Right side: mode indicator (vim-style)
	var modeTag string
	switch m.focus {
	case focusLogs:
		modeTag = lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(" LOGS ")
	case focusInput:
		modeTag = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(" INSERT ")
	case focusFocus:
		modeTag = lipgloss.NewStyle().Foreground(colorMagenta).Bold(true).Render(" FOCUS ")
	default:
		modeTag = lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render(" NORMAL ")
	}

	right := modeTag + " "

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderStatusBar() string {
	parts := []string{}

	active := m.countActive()
	pending := m.countByStatus(watcher.StatusPending)
	ready := m.countByStatus(watcher.StatusReady)
	failed := m.countByStatus(watcher.StatusFailed)

	pendingStr := fmt.Sprintf("%d pending", pending)
	activeStr := fmt.Sprintf("%d active", active)
	readyStr := fmt.Sprintf("%d ready", ready)
	failedStr := fmt.Sprintf("%d failed", failed)

	sep := footerSepStyle.Render(" | ")

	if pending > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorBlue).Render(pendingStr))
	} else {
		parts = append(parts, headerDimStyle.Render(pendingStr))
	}
	if active > 0 {
		parts = append(parts, statusRunningStyle.Render(m.spinner.View()+" "+activeStr))
	} else {
		parts = append(parts, headerDimStyle.Render(activeStr))
	}
	if ready > 0 {
		parts = append(parts, statusReadyBoldStyle.Render(readyStr))
	} else {
		parts = append(parts, headerDimStyle.Render(readyStr))
	}
	if failed > 0 {
		parts = append(parts, statusFailedStyle.Render(failedStr))
	} else {
		parts = append(parts, headerDimStyle.Render(failedStr))
	}

	return "  " + strings.Join(parts, sep)
}

// renderTree renders the scrollable tree of repos and issues.
func (m Model) renderTree() string {
	var allLines []string
	items := m.visibleItems()

	for itemIdx, item := range items {
		isSelected := itemIdx == m.cursor

		switch item.kind {
		case itemRepo:
			count := m.countIssuesForRepo(item.repo)
			repoErr := m.repoErrors[item.repo]
			allLines = append(allLines, m.renderRepoLine(item.repo, isSelected, count, repoErr))
			if repoErr != "" {
				errLine := "      " + statusFailedStyle.Render(repoErr)
				allLines = append(allLines, errLine)
			}

		case itemIssue:
			iss := m.issues[item.issueIdx]
			allLines = append(allLines, m.renderIssueLine(iss, isSelected))
		}
	}

	if len(allLines) == 0 {
		repos := m.manager.Repos()
		if len(repos) == 0 {
			allLines = append(allLines, headerDimStyle.Render("  No repos watched. Press r to add one."))
		} else {
			allLines = append(allLines, headerDimStyle.Render("  Waiting for issues..."))
		}
	}

	// Apply list scrolling
	start := m.listScroll
	end := start + m.listHeight
	if start > len(allLines) {
		start = len(allLines)
	}
	if end > len(allLines) {
		end = len(allLines)
	}

	visible := allLines[start:end]

	for len(visible) < m.listHeight {
		visible = append(visible, "")
	}

	return strings.Join(visible, "\n") + "\n"
}

func (m Model) renderRepoLine(repo string, selected bool, issueCount int, repoErr string) string {
	expanded := m.repoExpanded[repo]

	// Expand indicator: fixed-width arrow
	expandIcon := headerDimStyle.Render(">")
	if expanded {
		expandIcon = headerDimStyle.Render("v")
	}

	repoURL := fmt.Sprintf("https://github.com/%s", repo)
	repoDisplay := hyperlink(repoURL, repo)

	if repoErr != "" {
		repoStyled := repoNameErrStyle.Render(repoDisplay)
		errIcon := statusFailedStyle.Render("x")
		line := fmt.Sprintf("  %s %s %s", expandIcon, errIcon, repoStyled)
		if selected {
			return selectedRowStyle.Render(padOrTruncate(line, m.width))
		}
		return normalRowStyle.Render(line)
	}

	countStr := fmt.Sprintf("%d issues", issueCount)
	if issueCount == 1 {
		countStr = "1 issue"
	}

	repoStyled := repoNameStyle.Render(repoDisplay)
	countStyled := repoCountStyle.Render(countStr)

	line := fmt.Sprintf("  %s %s  %s", expandIcon, repoStyled, countStyled)

	if selected {
		return selectedRowStyle.Render(padOrTruncate(line, m.width))
	}
	return normalRowStyle.Render(line)
}

// --- Bead pipeline rendering ------------------------------------------------

// beadStages are the 5 pipeline stages in order.
var beadStages = [5]string{"react", "clone", "claude", "review", "pr"}

// beadState describes what a single bead looks like.
type beadState int

const (
	beadStatePending  beadState = iota // empty circle
	beadStateDone                      // filled circle
	beadStateActive                    // filled + spinner
	beadStateFail                      // X mark
	beadStatePausedAt                  // paused marker
)

// issueBeads returns the 5 bead states for a given issue status.
func issueBeads(status watcher.IssueStatus) [5]beadState {
	switch status {
	case watcher.StatusPending:
		return [5]beadState{beadStatePending, beadStatePending, beadStatePending, beadStatePending, beadStatePending}
	case watcher.StatusReacted:
		return [5]beadState{beadStateDone, beadStatePending, beadStatePending, beadStatePending, beadStatePending}
	case watcher.StatusCloning:
		return [5]beadState{beadStateDone, beadStateActive, beadStatePending, beadStatePending, beadStatePending}
	case watcher.StatusCloneReady:
		return [5]beadState{beadStateDone, beadStateDone, beadStatePending, beadStatePending, beadStatePending}
	case watcher.StatusClaudeRunning:
		return [5]beadState{beadStateDone, beadStateDone, beadStateActive, beadStatePending, beadStatePending}
	case watcher.StatusReady:
		return [5]beadState{beadStateDone, beadStateDone, beadStateDone, beadStateDone, beadStatePending}
	case watcher.StatusFailed:
		return [5]beadState{beadStateDone, beadStateDone, beadStateFail, beadStatePending, beadStatePending}
	case watcher.StatusPaused:
		return [5]beadState{beadStateDone, beadStateDone, beadStatePausedAt, beadStatePending, beadStatePending}
	default:
		return [5]beadState{beadStatePending, beadStatePending, beadStatePending, beadStatePending, beadStatePending}
	}
}

// renderBeads produces the bead pipeline string for an issue.
// Line 1: dots connected by lines   e.g.  "  ● ── ● ── ● ── ○ ── ○"
// Line 2: labels beneath the dots   e.g.  "  react clone claude review pr"
func (m Model) renderBeads(status watcher.IssueStatus) (string, string) {
	beads := issueBeads(status)
	connector := beadLine.Render("--")

	var dotParts []string
	var lblParts []string

	for i, bs := range beads {
		var dot string
		switch bs {
		case beadStateDone:
			dot = beadDone.Render("*")
		case beadStateActive:
			dot = beadActive.Render(m.spinner.View())
		case beadStateFail:
			dot = beadFailed.Render("x")
		case beadStatePausedAt:
			dot = beadPaused.Render("~")
		default: // pending
			dot = beadPending.Render("o")
		}
		dotParts = append(dotParts, dot)

		lbl := beadStages[i]
		// Pad label to 6 chars to match dot + connector width
		lblParts = append(lblParts, beadLabel.Render(fmt.Sprintf("%-6s", lbl)))

		if i < 4 {
			dotParts = append(dotParts, connector)
		}
	}

	dotsLine := strings.Join(dotParts, "")
	lblLine := strings.Join(lblParts, "")
	return dotsLine, lblLine
}

// renderBeadsCompact produces a single-line bead string: "*--*--*--o--o"
func (m Model) renderBeadsCompact(status watcher.IssueStatus) string {
	beads := issueBeads(status)
	connector := beadLine.Render("-")

	var parts []string
	for i, bs := range beads {
		var dot string
		switch bs {
		case beadStateDone:
			dot = beadDone.Render("*")
		case beadStateActive:
			dot = beadActive.Render(m.spinner.View())
		case beadStateFail:
			dot = beadFailed.Render("x")
		case beadStatePausedAt:
			dot = beadPaused.Render("~")
		default:
			dot = beadPending.Render("o")
		}
		parts = append(parts, dot)
		if i < 4 {
			parts = append(parts, connector)
		}
	}
	return strings.Join(parts, "")
}

func (m Model) renderIssueLine(iss watcher.TrackedIssue, selected bool) string {
	key := issueKey(iss.Repo, iss.Number)
	logCount := len(m.logs[key])

	// Compact bead pipeline (9 chars visual: "x-x-x-o-o")
	beadStr := m.renderBeadsCompact(iss.Status)

	// Elapsed time — only shown for actively running statuses
	var elapsedStr string
	if isActive(iss.Status) {
		el := elapsed(iss.StartedAt, m.now)
		elapsedStr = m.spinner.View() + " " + el
	}

	// Issue reference
	title := iss.Title
	if len(title) > 40 {
		title = title[:40] + "..."
	}
	issueRef := fmt.Sprintf("#%d %s", iss.Number, title)
	issueRef = hyperlink(iss.URL, issueRef)

	// Build the line:
	//   col 1: indent (4 chars)
	//   col 2: beads (compact, ~9 chars)
	//   col 3: issue ref (variable)
	//   col 4: elapsed + spinner (right side)
	//   col 5: log count

	var line strings.Builder
	line.WriteString("      ")
	line.WriteString(beadStr)
	line.WriteString("  ")
	line.WriteString(issueRef)

	if elapsedStr != "" {
		line.WriteString("  ")
		line.WriteString(headerDimStyle.Render(elapsedStr))
	}
	if logCount > 0 {
		line.WriteString("  ")
		line.WriteString(headerDimStyle.Render(fmt.Sprintf("[%d]", logCount)))
	}

	result := line.String()

	if selected {
		return selectedRowStyle.Render(padOrTruncate(result, m.width))
	}
	if iss.Status == watcher.StatusReady {
		return statusReadyBoldStyle.Render(result)
	}
	return normalRowStyle.Render(result)
}

func isActive(status watcher.IssueStatus) bool {
	switch status {
	case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
		return true
	}
	return false
}

func (m Model) statusIcon(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusPending:
		return beadPending.Render("o")
	case watcher.StatusReady:
		return statusReadyBoldStyle.Render("*")
	case watcher.StatusClaudeRunning:
		return statusRunningStyle.Render(m.spinner.View())
	case watcher.StatusReacted:
		return statusReactedStyle.Render("*")
	case watcher.StatusCloning:
		return statusRunningStyle.Render(m.spinner.View())
	case watcher.StatusCloneReady:
		return statusRunningStyle.Render("*")
	case watcher.StatusFailed:
		return statusFailedStyle.Render("x")
	case watcher.StatusPaused:
		return statusPausedStyle.Render("~")
	default:
		return " "
	}
}

func (m Model) statusLabel(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusPending:
		return beadPending.Render("pending")
	case watcher.StatusReady:
		return statusReadyBoldStyle.Render("REVIEW")
	case watcher.StatusClaudeRunning:
		return statusRunningStyle.Render("claude")
	case watcher.StatusReacted:
		return statusReactedStyle.Render("react")
	case watcher.StatusCloning:
		return statusRunningStyle.Render("clone")
	case watcher.StatusCloneReady:
		return statusRunningStyle.Render("cloned")
	case watcher.StatusFailed:
		return statusFailedStyle.Render("failed")
	case watcher.StatusPaused:
		return statusPausedStyle.Render("paused")
	default:
		return ""
	}
}

func (m Model) renderFooter() string {
	switch m.focus {
	case focusInput:
		return footerStyle.Render(" Add repo: " + m.textInput.View())
	case focusDialog, focusHelp:
		return " " + helpLineDialog()
	case focusConfirm:
		return " " + fmtHelp("y", "confirm") + "  " + fmtHelp("n/esc", "cancel")
	case focusFocus:
		return " " + helpLineFocus()
	default:
		return " " + helpLineNormal()
	}
}

func (m Model) renderWithDialog(_ string) string {
	iss := m.dialogIssue
	if iss == nil {
		return ""
	}

	var d strings.Builder
	d.WriteString(dialogTitleStyle.Render(fmt.Sprintf("%s #%d", iss.Repo, iss.Number)))
	d.WriteString("\n\n")
	d.WriteString(dialogLabelStyle.Render("Title:   "))
	d.WriteString(iss.Title)
	d.WriteString("\n")
	d.WriteString(dialogLabelStyle.Render("Status:  "))
	d.WriteString(iss.Status.String())

	// Bead pipeline in the dialog
	d.WriteString("\n\n")
	dotsLine, lblLine := m.renderBeads(iss.Status)
	d.WriteString("  " + dotsLine)
	d.WriteString("\n")
	d.WriteString("  " + lblLine)
	d.WriteString("\n")

	if iss.Labels != "" {
		d.WriteString("\n")
		d.WriteString(dialogLabelStyle.Render("Labels:  "))
		d.WriteString(iss.Labels)
	}
	if iss.URL != "" {
		d.WriteString("\n")
		d.WriteString(dialogLabelStyle.Render("URL:     "))
		d.WriteString(iss.URL)
	}
	if iss.Workdir != "" {
		d.WriteString("\n")
		d.WriteString(dialogLabelStyle.Render("Workdir: "))
		d.WriteString(iss.Workdir)
	}
	if iss.Error != "" {
		d.WriteString("\n\n")
		d.WriteString(statusFailedStyle.Render("Error: " + iss.Error))
	}
	if iss.Body != "" {
		d.WriteString("\n\n")
		d.WriteString(dialogLabelStyle.Render("Body:"))
		d.WriteString("\n")
		body := iss.Body
		if len(body) > 500 {
			body = body[:500] + "..."
		}
		d.WriteString(body)
	}
	d.WriteString("\n\n")
	d.WriteString(fmtHelp("esc", "close") + "  " + fmtHelp("o", "open in browser"))

	dialog := dialogStyle.Render(d.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m Model) renderConfirmDialog() string {
	var d strings.Builder
	d.WriteString(dialogTitleStyle.Render("Remove repo"))
	d.WriteString("\n\n")
	d.WriteString("Remove ")
	d.WriteString(repoNameStyle.Render(m.confirmRepo))
	d.WriteString(" and all its issues?")
	d.WriteString("\n\n")
	d.WriteString(fmtHelp("y", "confirm") + "  " + fmtHelp("n/esc", "cancel"))

	dialog := dialogStyle.Render(d.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m Model) renderFocusView() string {
	iss := m.focusIssue
	if iss == nil {
		return ""
	}

	var b strings.Builder

	// Line 1: repo  #num  beads  url
	repoStyled := repoNameStyle.Render(iss.Repo)
	numStr := headerDimStyle.Render(fmt.Sprintf("#%d", iss.Number))
	beadStr := m.renderBeadsCompact(iss.Status)
	label := m.statusLabel(iss.Status)
	urlStr := headerDimStyle.Render(hyperlink(iss.URL, iss.URL))
	b.WriteString(fmt.Sprintf(" %s  %s  %s %s  %s", repoStyled, numStr, beadStr, label, urlStr))
	b.WriteString("\n")

	// Line 2: title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(colorFg)
	b.WriteString(" " + titleStyle.Render(iss.Title))
	b.WriteString("\n")

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// Scrollable log area
	key := issueKey(iss.Repo, iss.Number)
	logLines := m.logs[key]
	visibleLines := m.height - 5 // header(1) + title(1) + sep(1) + sep(1) + footer(1)
	if visibleLines < 1 {
		visibleLines = 1
	}

	start := m.focusScroll
	end := start + visibleLines
	if start > len(logLines) {
		start = len(logLines)
	}
	if end > len(logLines) {
		end = len(logLines)
	}

	visible := logLines[start:end]
	isActive := iss.Status == watcher.StatusClaudeRunning
	for _, line := range visible {
		if isActive {
			b.WriteString(logLineActiveStyle.Render(" " + line))
		} else {
			b.WriteString(logLineStyle.Render(" " + line))
		}
		b.WriteString("\n")
	}
	// Pad remaining lines
	for i := len(visible); i < visibleLines; i++ {
		b.WriteString("\n")
	}

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// Footer
	b.WriteString(" " + helpLineFocus())

	return b.String()
}

func (m Model) renderHelpScreen() string {
	var d strings.Builder
	d.WriteString(dialogTitleStyle.Render("Keybindings"))
	d.WriteString("\n\n")

	section := func(title string, bindings [][2]string) {
		d.WriteString(dialogLabelStyle.Render(title))
		d.WriteString("\n")
		for _, b := range bindings {
			d.WriteString(fmt.Sprintf("  %s  %s\n",
				footerKeyStyle.Render(fmt.Sprintf("%-8s", b[0])),
				b[1]))
		}
		d.WriteString("\n")
	}

	section("Navigation", [][2]string{
		{"j / k", "Move down / up"},
		{"enter/l", "Expand repo / focus issue"},
		{"f", "Focus view (full-screen logs)"},
		{"i", "Info dialog"},
		{"o", "Open in browser"},
	})

	section("Actions", [][2]string{
		{"space", "Start / pause processing"},
		{"S", "Resume all paused issues"},
		{"a", "Approve & create PR"},
		{"t", "Takeover — interactive Claude (--continue)"},
		{"s", "Shell — persistent PTY (Ctrl+] to detach)"},
		{"g", "Launch lazygit"},
		{"c", "Launch Claude Code"},
	})

	section("Repos", [][2]string{
		{"r", "Add repo"},
		{"R / d", "Remove repo"},
	})

	section("General", [][2]string{
		{"?", "Toggle this help"},
		{"esc", "Back / close"},
		{"q", "Quit"},
	})

	dialog := dialogStyle.Render(d.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}
