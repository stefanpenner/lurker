package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanpenner/lurker/pkg/watcher"
)

const (
	maxLogLines    = 500
	maxVisibleLogs = 15 // max log lines shown when expanded
)

// focus tracks which panel has keyboard focus.
type focus int

const (
	focusList   focus = iota
	focusLogs         // scrolling within an expanded issue's logs
	focusDialog       // detail dialog open
)

// Model is the Bubbletea model for the TUI dashboard.
type Model struct {
	issues   []watcher.TrackedIssue
	logs     map[int][]string // per-issue log lines
	expanded map[int]bool     // which issues have logs toggled open

	cursor    int   // selected issue index
	focus     focus // current focus mode
	logScroll int   // scroll offset within focused issue's logs

	listScroll int // scroll offset for the entire issue list
	listHeight int // how many lines available for the issue list

	spinner spinner.Model
	width   int
	height  int
	paused  bool
	repo    string
	eventCh <-chan watcher.Event

	// Dialog state
	dialogIssue *watcher.TrackedIssue

	// Counters
	lastPoll    time.Time
	pollCount   int
	activeCount int
	failCount   int
	readyCount  int
	now         time.Time
}

// Messages
type eventMsg watcher.Event
type tickMsg struct{}

type prResultMsg struct {
	issueNum int
	url      string
	err      error
}

// NewModel creates a new TUI Model.
func NewModel(repo string, eventCh <-chan watcher.Event) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		logs:     make(map[int][]string),
		expanded: make(map[int]bool),
		spinner:  s,
		repo:     repo,
		eventCh:  eventCh,
		now:      time.Now(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.pollEvents())
}

func (m Model) pollEvents() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-m.eventCh:
			if !ok {
				return nil
			}
			return eventMsg(ev)
		case <-time.After(100 * time.Millisecond):
			return tickMsg{}
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.listHeight = msg.Height - 5 // header(1) + status(1) + sep(1) + footer(1) + sep(1)
		if m.listHeight < 3 {
			m.listHeight = 3
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case eventMsg:
		m.now = time.Now()
		m.handleEvent(watcher.Event(msg))
		cmds = append(cmds, m.pollEvents())

	case tickMsg:
		m.now = time.Now()
		cmds = append(cmds, m.pollEvents())

	case prResultMsg:
		m.handlePRResult(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Dialog mode — only esc closes
	if m.focus == focusDialog {
		if key == "esc" {
			m.focus = focusList
			m.dialogIssue = nil
		}
		return nil
	}

	// Quit always works
	if key == "q" || key == "ctrl+c" {
		return tea.Quit
	}

	// Log scroll mode
	if m.focus == focusLogs {
		switch key {
		case "esc", "l":
			m.focus = focusList
		case "j", "down":
			m.logScroll++
			m.clampLogScroll()
		case "k", "up":
			if m.logScroll > 0 {
				m.logScroll--
			}
		case "g":
			m.logScroll = 0
		case "G":
			m.logScroll = 999999
			m.clampLogScroll()
		case "o":
			m.openGithubIssue()
		case "a":
			return m.approvePR()
		}
		return nil
	}

	// Normal list mode
	switch key {
	case "j", "down":
		if m.cursor < len(m.issues)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
		}
	case "l", "enter":
		m.toggleLogs()
	case "tab":
		// If expanded, enter log scroll mode
		if iss := m.selectedIssue(); iss != nil && m.expanded[iss.Number] {
			m.focus = focusLogs
			m.logScroll = 999999
			m.clampLogScroll()
		}
	case "g":
		return m.launchLazygit()
	case "c":
		return m.launchClaude()
	case "o":
		m.openGithubIssue()
	case "i":
		m.showDialog()
	case "p":
		m.paused = !m.paused
	case "a":
		return m.approvePR()
	}

	return nil
}

func (m *Model) selectedIssue() *watcher.TrackedIssue {
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		return &m.issues[m.cursor]
	}
	return nil
}

func (m *Model) toggleLogs() {
	if iss := m.selectedIssue(); iss != nil {
		m.expanded[iss.Number] = !m.expanded[iss.Number]
		if !m.expanded[iss.Number] {
			// Collapsing — exit log focus if we were in it
			if m.focus == focusLogs {
				m.focus = focusList
			}
		}
	}
}

func (m *Model) clampLogScroll() {
	iss := m.selectedIssue()
	if iss == nil {
		return
	}
	lines := m.logs[iss.Number]
	maxScroll := len(lines) - maxVisibleLogs
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.logScroll > maxScroll {
		m.logScroll = maxScroll
	}
}

// ensureCursorVisible adjusts listScroll so the cursor's issue line is visible.
func (m *Model) ensureCursorVisible() {
	// Calculate the line offset of the cursor in the rendered list
	lineOffset := 0
	for i := 0; i < m.cursor; i++ {
		lineOffset++ // issue line
		if m.expanded[m.issues[i].Number] {
			n := len(m.logs[m.issues[i].Number])
			if n > maxVisibleLogs {
				n = maxVisibleLogs
			}
			lineOffset += n
		}
	}

	// Scroll up if cursor is above visible area
	if lineOffset < m.listScroll {
		m.listScroll = lineOffset
	}

	// Scroll down if cursor is below visible area
	if lineOffset >= m.listScroll+m.listHeight {
		m.listScroll = lineOffset - m.listHeight + 1
	}
}

func (m *Model) openGithubIssue() {
	if iss := m.selectedIssue(); iss != nil && iss.URL != "" {
		exec.Command("open", iss.URL).Start()
	}
}

func (m *Model) showDialog() {
	if iss := m.selectedIssue(); iss != nil {
		m.dialogIssue = iss
		m.focus = focusDialog
	}
}

func (m *Model) launchLazygit() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}
	c := exec.Command("lazygit")
	c.Dir = iss.Workdir
	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

func (m *Model) launchClaude() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}
	c := exec.Command("claude")
	c.Dir = iss.Workdir
	// Strip env vars that would cause nesting issues
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") &&
			!strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	c.Env = filtered
	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

func (m *Model) approvePR() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	num := iss.Number
	title := iss.Title
	workdir := iss.Workdir
	repo := m.repo

	m.appendLog(num, "")
	m.appendLog(num, "🚀 Pushing branch & creating PR...")

	return func() tea.Msg {
		cmd := exec.Command("git", "push", "-u", "origin", "HEAD")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("push: %s: %w", strings.TrimSpace(string(out)), err)}
		}

		cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = workdir
		branchOut, err := cmd.Output()
		if err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("branch: %w", err)}
		}
		branch := strings.TrimSpace(string(branchOut))

		cmd = exec.Command("git", "log", "--oneline", "main.."+branch)
		cmd.Dir = workdir
		logOut, _ := cmd.Output()

		body := fmt.Sprintf("Fixes #%d\n\n## Commits\n```\n%s```\n\n🤖 Generated by lurker", num, string(logOut))

		prTitle := fmt.Sprintf("Fix #%d: %s", num, title)
		cmd = exec.Command("gh", "pr", "create",
			"--repo", repo,
			"--title", prTitle,
			"--body", body,
			"--head", branch,
		)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("pr: %s: %w", strings.TrimSpace(string(out)), err)}
		}

		return prResultMsg{issueNum: num, url: strings.TrimSpace(string(out))}
	}
}

func (m *Model) handlePRResult(msg prResultMsg) {
	if msg.err != nil {
		m.appendLog(msg.issueNum, "❌ "+msg.err.Error())
	} else {
		m.appendLog(msg.issueNum, "✅ PR: "+msg.url)
		// Auto-expand to show the result
		m.expanded[msg.issueNum] = true
	}
}

// --- Event handling ---

func (m *Model) handleEvent(ev watcher.Event) {
	switch ev.Kind {
	case watcher.EventPollStart:
		m.pollCount++
		m.lastPoll = ev.Timestamp

	case watcher.EventIssueFound:
		m.issues = append(m.issues, watcher.TrackedIssue{
			Number:    ev.IssueNum,
			Title:     ev.Text,
			Body:      ev.IssueBody,
			Labels:    ev.IssueLabels,
			URL:       ev.IssueURL,
			Status:    watcher.StatusReacted,
			StartedAt: ev.Timestamp,
		})
		m.logs[ev.IssueNum] = []string{}
		m.appendLog(ev.IssueNum, "Issue discovered")

	case watcher.EventReacted:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusReacted)
		m.appendLog(ev.IssueNum, "👀 Reacted")

	case watcher.EventCloneStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloning)
		m.appendLog(ev.IssueNum, "📦 Cloning...")

	case watcher.EventCloneDone:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloneReady)
		m.setWorkdir(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "📂 "+ev.Text)

	case watcher.EventClaudeStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusClaudeRunning)
		m.activeCount++
		m.appendLog(ev.IssueNum, "🤖 Claude working...")
		// Auto-expand active issues
		m.expanded[ev.IssueNum] = true

	case watcher.EventClaudeLog:
		m.appendLog(ev.IssueNum, "  "+ev.Text)

	case watcher.EventClaudeDone:
		if m.activeCount > 0 {
			m.activeCount--
		}
		m.appendLog(ev.IssueNum, ev.Text)

	case watcher.EventReady:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusReady)
		m.readyCount++
		m.appendLog(ev.IssueNum, "✅ Ready — press 'a' to approve & open PR")

	case watcher.EventError:
		if m.findIssueStatus(ev.IssueNum) == watcher.StatusClaudeRunning {
			if m.activeCount > 0 {
				m.activeCount--
			}
		}
		m.updateIssueStatus(ev.IssueNum, watcher.StatusFailed)
		m.failCount++
		m.setError(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "❌ "+ev.Text)

	case watcher.EventPollDone:
		// Don't log routine poll results
	}
}

func (m *Model) findIssueStatus(num int) watcher.IssueStatus {
	for _, iss := range m.issues {
		if iss.Number == num {
			return iss.Status
		}
	}
	return watcher.StatusReacted
}

func (m *Model) updateIssueStatus(num int, status watcher.IssueStatus) {
	for i := range m.issues {
		if m.issues[i].Number == num {
			m.issues[i].Status = status
			return
		}
	}
}

func (m *Model) setWorkdir(num int, dir string) {
	for i := range m.issues {
		if m.issues[i].Number == num {
			m.issues[i].Workdir = dir
			return
		}
	}
}

func (m *Model) setError(num int, errText string) {
	for i := range m.issues {
		if m.issues[i].Number == num {
			m.issues[i].Error = errText
			return
		}
	}
}

func (m *Model) appendLog(num int, line string) {
	if m.logs[num] == nil {
		m.logs[num] = []string{}
	}
	m.logs[num] = append(m.logs[num], line)
	if len(m.logs[num]) > maxLogLines {
		m.logs[num] = m.logs[num][len(m.logs[num])-maxLogLines:]
	}
}

func elapsed(start time.Time, now time.Time) string {
	d := now.Sub(start)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
