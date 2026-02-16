package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/stefanpenner/issue-watcher/pkg/watcher"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Status bar
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Issue table
	b.WriteString(m.renderTable())

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Log viewport
	b.WriteString(m.renderLogPanel())

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Footer
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	title := headerStyle.Render("issue-watcher")
	repo := lipgloss.NewStyle().Foreground(colorTeal).Render(m.repo)
	interval := lipgloss.NewStyle().Foreground(colorSubtext).Render("polling 30s")

	left := fmt.Sprintf("  %s  %s", title, repo)
	right := fmt.Sprintf("%s  ", interval)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderStatusBar() string {
	var status string
	if m.watching {
		status = statusRunningStyle.Render("● watching")
	} else {
		status = statusFailedStyle.Render("● stopped")
	}

	processed := fmt.Sprintf("%d processed", len(m.issues))

	active := lipgloss.NewStyle().Foreground(colorYellow).Render(
		fmt.Sprintf("%d active", m.activeCount))

	ready := lipgloss.NewStyle().Foreground(colorGreen).Render(
		fmt.Sprintf("%d ready", m.readyCount))

	failed := lipgloss.NewStyle().Foreground(colorRed).Render(
		fmt.Sprintf("%d failed", m.failCount))

	return statusBarStyle.Render(fmt.Sprintf("  %s   %s   %s   %s   %s",
		status, processed, active, ready, failed))
}

func (m Model) renderTable() string {
	var b strings.Builder

	header := tableHeaderStyle.Render(
		fmt.Sprintf("  %-5s %-10s %-8s %-28s %s", "#", "Status", "Time", "Issue", "Workdir"))
	b.WriteString(header)
	b.WriteString("\n")

	if len(m.issues) == 0 {
		b.WriteString(normalRowStyle.Render("  Waiting for issues..."))
		b.WriteString("\n")
	}

	// Show up to 8 visible issues around the cursor
	start := 0
	end := len(m.issues)
	maxVisible := 8
	if end-start > maxVisible {
		start = m.cursor - maxVisible/2
		if start < 0 {
			start = 0
		}
		end = start + maxVisible
		if end > len(m.issues) {
			end = len(m.issues)
			start = end - maxVisible
		}
	}

	for i := start; i < end; i++ {
		iss := m.issues[i]
		statusIcon := m.statusIcon(iss.Status)
		statusLabel := m.statusLabel(iss.Status)

		// Elapsed time
		elapsedStr := elapsed(iss.StartedAt, m.now)
		if iss.Status == watcher.StatusClaudeRunning {
			// Show spinner for active issues
			elapsedStr = m.spinner.View() + " " + elapsedStr
		}

		title := iss.Title
		if len(title) > 26 {
			title = title[:26] + "…"
		}

		workdir := iss.Workdir
		if len(workdir) > 20 {
			workdir = "…" + workdir[len(workdir)-19:]
		}

		row := fmt.Sprintf("  %-5d %s %-8s %-8s %-28s %s",
			iss.Number, statusIcon, statusLabel, elapsedStr, title, workdir)

		if i == m.cursor {
			b.WriteString(selectedRowStyle.Render(row))
		} else {
			b.WriteString(normalRowStyle.Render(row))
		}
		b.WriteString("\n")
	}

	return b.String()
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

func (m Model) renderLogPanel() string {
	var b strings.Builder

	header := "Logs"
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		iss := m.issues[m.cursor]
		logCount := len(m.logs[iss.Number])
		header = fmt.Sprintf("Logs (issue #%d) — %d lines", iss.Number, logCount)
		if iss.Status == watcher.StatusClaudeRunning {
			header += " " + m.spinner.View()
		}
	}
	b.WriteString(logHeaderStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(m.logViewport.View())
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderFooter() string {
	return footerStyle.Render(keys.helpLine())
}
