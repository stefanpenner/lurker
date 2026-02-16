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

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if m.focus == focusFocus && m.focusIssue != nil {
		return m.renderFocusView()
	}

	var b strings.Builder

	// Header + status bar
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Tree list with inline logs
	b.WriteString(m.renderTree())

	// Footer
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	// Dialog overlay
	if m.focus == focusDialog && m.dialogIssue != nil {
		return m.renderWithDialog(b.String())
	}

	return b.String()
}

func (m Model) renderHeader() string {
	title := headerStyle.Render("lurker")

	repos := m.manager.Repos()
	var repoStr string
	if len(repos) == 0 {
		repoStr = lipgloss.NewStyle().Foreground(colorSubtext).Render("no repos — press r to add")
	} else {
		repoStr = lipgloss.NewStyle().Foreground(colorSubtext).Render(fmt.Sprintf("%d repos", len(repos)))
	}

	left := fmt.Sprintf(" %s  %s", title, repoStr)

	right := lipgloss.NewStyle().Foreground(colorSubtext).Render("polling 30s") + "  "

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
	readyStr := fmt.Sprintf("★ %d ready", ready)
	failedStr := fmt.Sprintf("%d failed", failed)

	if pending > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorBlue).Render(pendingStr))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(pendingStr))
	}
	if active > 0 {
		parts = append(parts, statusRunningStyle.Render(m.spinner.View()+" "+activeStr))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(activeStr))
	}
	if ready > 0 {
		parts = append(parts, statusReadyBoldStyle.Render(readyStr))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(readyStr))
	}
	if failed > 0 {
		parts = append(parts, statusFailedStyle.Render(failedStr))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(failedStr))
	}

	return statusBarStyle.Render(" " + strings.Join(parts, "   "))
}

// renderTree renders the scrollable tree of repos and issues with inline logs.
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
				allLines = append(allLines, statusFailedStyle.Render("      "+repoErr))
			}

		case itemIssue:
			iss := m.issues[item.issueIdx]
			key := issueKey(iss.Repo, iss.Number)
			allLines = append(allLines, m.renderIssueLine(iss, isSelected))

			if m.expanded[key] {
				logLines := m.logs[key]
				visibleLogs := logLines

				if m.focus == focusLogs && isSelected {
					start := m.logScroll
					end := start + maxVisibleLogs
					if start > len(logLines) {
						start = len(logLines)
					}
					if end > len(logLines) {
						end = len(logLines)
					}
					visibleLogs = logLines[start:end]

					if start > 0 {
						allLines = append(allLines, logLineStyle.Render("         ↑ more"))
					}
				} else {
					if len(visibleLogs) > maxVisibleLogs {
						visibleLogs = visibleLogs[len(visibleLogs)-maxVisibleLogs:]
					}
				}

				isActive := iss.Status == watcher.StatusClaudeRunning
				for _, line := range visibleLogs {
					if isActive {
						allLines = append(allLines, logLineActiveStyle.Render("         "+line))
					} else {
						allLines = append(allLines, logLineStyle.Render("         "+line))
					}
				}

				if m.focus == focusLogs && isSelected {
					end := m.logScroll + maxVisibleLogs
					if end < len(logLines) {
						allLines = append(allLines, logLineStyle.Render("         ↓ more"))
					}
				}
			}
		}
	}

	if len(allLines) == 0 {
		repos := m.manager.Repos()
		if len(repos) == 0 {
			allLines = append(allLines, lipgloss.NewStyle().Foreground(colorSubtext).Render("  No repos watched. Press r to add one."))
		} else {
			allLines = append(allLines, lipgloss.NewStyle().Foreground(colorSubtext).Render("  Waiting for issues..."))
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
	expandIcon := "▸"
	if expanded {
		expandIcon = "▾"
	}

	repoURL := fmt.Sprintf("https://github.com/%s", repo)
	repoDisplay := hyperlink(repoURL, repo)

	if repoErr != "" {
		repoStyled := lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(repoDisplay)
		line := fmt.Sprintf(" %s ❌ %s", expandIcon, repoStyled)
		if selected {
			return selectedRowStyle.Render(line)
		}
		return normalRowStyle.Render(line)
	}

	countStr := fmt.Sprintf("%d issues", issueCount)
	if issueCount == 1 {
		countStr = "1 issue"
	}

	repoStyled := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(repoDisplay)
	countStyled := lipgloss.NewStyle().Foreground(colorSubtext).Render(countStr)

	line := fmt.Sprintf(" %s %s  %s", expandIcon, repoStyled, countStyled)

	if selected {
		return selectedRowStyle.Render(line)
	}
	return normalRowStyle.Render(line)
}

func (m Model) renderIssueLine(iss watcher.TrackedIssue, selected bool) string {
	icon := m.statusIcon(iss.Status)
	label := m.statusLabel(iss.Status)

	key := issueKey(iss.Repo, iss.Number)
	expandIcon := "▸"
	if m.expanded[key] {
		expandIcon = "▾"
	}
	logCount := len(m.logs[key])

	var elapsedStr string
	if iss.Status != watcher.StatusPending {
		elapsedStr = elapsed(iss.StartedAt, m.now)
		if iss.Status == watcher.StatusClaudeRunning {
			elapsedStr = m.spinner.View() + " " + elapsedStr
		}
	}

	title := iss.Title
	if len(title) > 40 {
		title = title[:40] + "…"
	}

	issueRef := fmt.Sprintf("#%d %s", iss.Number, title)
	issueRef = hyperlink(iss.URL, issueRef)

	line := fmt.Sprintf("   %s %s %s %-7s", expandIcon, issueRef, icon, label)
	if elapsedStr != "" {
		line += fmt.Sprintf(" %-7s", elapsedStr)
	}
	if logCount > 0 {
		line += fmt.Sprintf("  (%d)", logCount)
	}

	if selected {
		return selectedRowStyle.Render(line)
	}
	if iss.Status == watcher.StatusReady {
		return statusReadyBoldStyle.Render(line)
	}
	return normalRowStyle.Render(line)
}

func (m Model) statusIcon(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusPending:
		return lipgloss.NewStyle().Foreground(colorSubtext).Render("○")
	case watcher.StatusReady:
		return statusReadyBoldStyle.Render("★")
	case watcher.StatusClaudeRunning:
		return statusRunningStyle.Render("🔄")
	case watcher.StatusReacted:
		return statusReactedStyle.Render("👀")
	case watcher.StatusCloning:
		return statusRunningStyle.Render("📦")
	case watcher.StatusCloneReady:
		return statusRunningStyle.Render("📂")
	case watcher.StatusFailed:
		return statusFailedStyle.Render("❌")
	case watcher.StatusPaused:
		return statusPausedStyle.Render("⏸")
	default:
		return "  "
	}
}

func (m Model) statusLabel(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusPending:
		return lipgloss.NewStyle().Foreground(colorSubtext).Render("pending")
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
		return footerStyle.Render("Add repo: " + m.textInput.View())
	case focusLogs:
		return footerStyle.Render(helpLineLogs())
	case focusDialog:
		return footerStyle.Render(helpLineDialog())
	case focusFocus:
		return footerStyle.Render(helpLineFocus())
	default:
		return footerStyle.Render(helpLineNormal())
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
	d.WriteString("\n")
	if iss.Labels != "" {
		d.WriteString(dialogLabelStyle.Render("Labels:  "))
		d.WriteString(iss.Labels)
		d.WriteString("\n")
	}
	if iss.URL != "" {
		d.WriteString(dialogLabelStyle.Render("URL:     "))
		d.WriteString(iss.URL)
		d.WriteString("\n")
	}
	if iss.Workdir != "" {
		d.WriteString(dialogLabelStyle.Render("Workdir: "))
		d.WriteString(iss.Workdir)
		d.WriteString("\n")
	}
	if iss.Error != "" {
		d.WriteString("\n")
		d.WriteString(statusFailedStyle.Render("Error: "+iss.Error))
		d.WriteString("\n")
	}
	if iss.Body != "" {
		d.WriteString("\n")
		d.WriteString(dialogLabelStyle.Render("Body:"))
		d.WriteString("\n")
		body := iss.Body
		if len(body) > 500 {
			body = body[:500] + "…"
		}
		d.WriteString(body)
		d.WriteString("\n")
	}
	d.WriteString("\n")
	d.WriteString(lipgloss.NewStyle().Foreground(colorSubtext).Render("esc close  o open in browser"))

	dialog := dialogStyle.Render(d.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m Model) renderFocusView() string {
	iss := m.focusIssue
	if iss == nil {
		return ""
	}

	var b strings.Builder

	// Line 1: repo  #num  icon label  url
	icon := m.statusIcon(iss.Status)
	label := m.statusLabel(iss.Status)
	repoStyled := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(iss.Repo)
	numStr := lipgloss.NewStyle().Foreground(colorSubtext).Render(fmt.Sprintf("#%d", iss.Number))
	urlStr := lipgloss.NewStyle().Foreground(colorSubtext).Render(hyperlink(iss.URL, iss.URL))
	b.WriteString(fmt.Sprintf(" %s  %s  %s %s  %s", repoStyled, numStr, icon, label, urlStr))
	b.WriteString("\n")

	// Line 2: title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(colorText)
	b.WriteString(" " + titleStyle.Render(iss.Title))
	b.WriteString("\n")

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
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
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Footer
	b.WriteString(footerStyle.Render(helpLineFocus()))

	return b.String()
}
