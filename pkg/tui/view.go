package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/stefanpenner/lurker/pkg/watcher"
)

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// Header + status bar
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Issue list with inline logs
	b.WriteString(m.renderIssueList())

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
	repo := lipgloss.NewStyle().Foreground(colorTeal).Render(m.repo)

	left := fmt.Sprintf(" %s  %s", title, repo)

	var right string
	if m.paused {
		right = statusPausedStyle.Render("⏸ paused") + "  "
	} else {
		right = lipgloss.NewStyle().Foreground(colorSubtext).Render("polling 30s") + "  "
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderStatusBar() string {
	parts := []string{}

	active := fmt.Sprintf("%d active", m.activeCount)
	ready := fmt.Sprintf("%d ready", m.readyCount)
	failed := fmt.Sprintf("%d failed", m.failCount)

	if m.activeCount > 0 {
		parts = append(parts, statusRunningStyle.Render(m.spinner.View()+" "+active))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(active))
	}
	if m.readyCount > 0 {
		parts = append(parts, statusReadyStyle.Render(ready))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(ready))
	}
	if m.failCount > 0 {
		parts = append(parts, statusFailedStyle.Render(failed))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSubtext).Render(failed))
	}

	return statusBarStyle.Render(" " + strings.Join(parts, "   "))
}

// renderIssueList renders the scrollable list of issues with inline expandable logs.
func (m Model) renderIssueList() string {
	// Build all lines first
	var allLines []string

	for i, iss := range m.issues {
		isSelected := i == m.cursor
		allLines = append(allLines, m.renderIssueLine(iss, isSelected))

		if m.expanded[iss.Number] {
			logLines := m.logs[iss.Number]
			visibleLogs := logLines

			// Apply scroll offset if this issue has log focus
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

				// Show scroll indicator
				if start > 0 {
					allLines = append(allLines, logLineStyle.Render("     ↑ more"))
				}
			} else {
				// When not focused, show last N lines
				if len(visibleLogs) > maxVisibleLogs {
					visibleLogs = visibleLogs[len(visibleLogs)-maxVisibleLogs:]
				}
			}

			isActive := iss.Status == watcher.StatusClaudeRunning
			for _, line := range visibleLogs {
				if isActive {
					allLines = append(allLines, logLineActiveStyle.Render("     "+line))
				} else {
					allLines = append(allLines, logLineStyle.Render("     "+line))
				}
			}

			if m.focus == focusLogs && isSelected {
				end := m.logScroll + maxVisibleLogs
				if end < len(logLines) {
					allLines = append(allLines, logLineStyle.Render("     ↓ more"))
				}
			}
		}
	}

	if len(allLines) == 0 {
		allLines = append(allLines, lipgloss.NewStyle().Foreground(colorSubtext).Render("  Waiting for issues..."))
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

	// Pad to fill the list area
	for len(visible) < m.listHeight {
		visible = append(visible, "")
	}

	return strings.Join(visible, "\n") + "\n"
}

func (m Model) renderIssueLine(iss watcher.TrackedIssue, selected bool) string {
	icon := m.statusIcon(iss.Status)
	label := m.statusLabel(iss.Status)
	elapsedStr := elapsed(iss.StartedAt, m.now)

	expandIcon := "▸"
	if m.expanded[iss.Number] {
		expandIcon = "▾"
	}
	logCount := len(m.logs[iss.Number])

	// Spinner for active
	if iss.Status == watcher.StatusClaudeRunning {
		elapsedStr = m.spinner.View() + " " + elapsedStr
	}

	title := iss.Title
	if len(title) > 40 {
		title = title[:40] + "…"
	}

	line := fmt.Sprintf(" %s #%-4d %s %-7s %-7s %s  (%d)",
		expandIcon, iss.Number, icon, label, elapsedStr, title, logCount)

	if selected {
		return selectedRowStyle.Render(line)
	}
	return normalRowStyle.Render(line)
}

func (m Model) statusIcon(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusReady:
		return statusReadyStyle.Render("✅")
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
	default:
		return "  "
	}
}

func (m Model) statusLabel(status watcher.IssueStatus) string {
	switch status {
	case watcher.StatusReady:
		return statusReadyStyle.Render("ready")
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
	default:
		return ""
	}
}

func (m Model) renderFooter() string {
	switch m.focus {
	case focusLogs:
		return footerStyle.Render(helpLineLogs())
	case focusDialog:
		return footerStyle.Render(helpLineDialog())
	default:
		return footerStyle.Render(helpLineNormal())
	}
}

// renderWithDialog renders a centered dialog, replacing the base view entirely.
func (m Model) renderWithDialog(_ string) string {
	iss := m.dialogIssue
	if iss == nil {
		return ""
	}

	var d strings.Builder
	d.WriteString(dialogTitleStyle.Render(fmt.Sprintf("Issue #%d", iss.Number)))
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
