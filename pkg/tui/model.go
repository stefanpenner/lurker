package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanpenner/issue-watcher/pkg/watcher"
)

const maxLogLines = 500

// focus tracks which panel has keyboard focus.
type focus int

const (
	focusList focus = iota
	focusLogs
)

// Model is the Bubbletea model for the TUI dashboard.
type Model struct {
	issues      []watcher.TrackedIssue
	logs        map[int][]string // per-issue log lines
	cursor      int
	focus       focus
	logViewport viewport.Model
	spinner     spinner.Model
	width       int
	height      int
	watching    bool
	repo        string
	eventCh     <-chan watcher.Event
	lastPoll    time.Time
	pollCount   int
	activeCount int
	failCount   int
	readyCount  int
	now         time.Time // updated every tick for elapsed time display
}

// eventMsg wraps a watcher.Event for Bubbletea.
type eventMsg watcher.Event

// tickMsg triggers periodic event channel reads and time updates.
type tickMsg struct{}

// prResultMsg reports the result of an async PR creation.
type prResultMsg struct {
	issueNum int
	url      string
	err      error
}

// commitResultMsg reports the result of an async commit message generation.
type commitResultMsg struct {
	issueNum int
	message  string
	err      error
}

// NewModel creates a new TUI Model.
func NewModel(repo string, eventCh <-chan watcher.Event) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	vp := viewport.New(80, 10)

	return Model{
		issues:      nil,
		logs:        make(map[int][]string),
		logViewport: vp,
		spinner:     s,
		watching:    true,
		repo:        repo,
		eventCh:     eventCh,
		now:         time.Now(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.pollEvents(),
	)
}

// pollEvents reads from the event channel.
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

// Update implements tea.Model.
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
		m.logViewport.Width = msg.Width - 4
		logHeight := msg.Height - 14
		if logHeight < 3 {
			logHeight = 3
		}
		m.logViewport.Height = logHeight

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

	case commitResultMsg:
		m.handleCommitResult(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Global keys (work in both focus modes)
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "o":
		m.openWorkdir()
		return nil
	case "d":
		m.showDiff()
		return nil
	case "l":
		return m.launchLazygit()
	case "p":
		return m.createPR()
	case "c":
		return m.generateCommit()
	}

	if m.focus == focusLogs {
		switch key {
		case "esc":
			m.focus = focusList
		case "j", "down":
			m.logViewport.LineDown(1)
		case "k", "up":
			m.logViewport.LineUp(1)
		case "g":
			m.logViewport.GotoTop()
		case "G":
			m.logViewport.GotoBottom()
		}
	} else {
		switch key {
		case "enter":
			m.focus = focusLogs
			m.logViewport.GotoBottom()
		case "j", "down":
			if m.cursor < len(m.issues)-1 {
				m.cursor++
				m.updateLogViewport()
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				m.updateLogViewport()
			}
		}
	}

	return nil
}

func (m *Model) selectedIssue() *watcher.TrackedIssue {
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		return &m.issues[m.cursor]
	}
	return nil
}

// launchLazygit suspends the TUI and opens lazygit in the issue's workdir.
func (m *Model) launchLazygit() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	c := exec.Command("lazygit")
	c.Dir = iss.Workdir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return nil // just resume the TUI
	})
}

// createPR pushes the branch and opens a PR via gh CLI.
func (m *Model) createPR() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	num := iss.Number
	title := iss.Title
	workdir := iss.Workdir
	repo := m.repo

	m.appendLog(num, "")
	m.appendLog(num, "🚀 Creating PR...")
	m.updateLogViewport()

	return func() tea.Msg {
		// Push the branch
		cmd := exec.Command("git", "push", "-u", "origin", "HEAD")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("git push failed: %s: %w", string(out), err)}
		}

		// Get branch name
		cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = workdir
		branchOut, err := cmd.Output()
		if err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("getting branch: %w", err)}
		}
		branch := strings.TrimSpace(string(branchOut))

		// Get commit log for body
		cmd = exec.Command("git", "log", "--oneline", "main.."+branch)
		cmd.Dir = workdir
		logOut, _ := cmd.Output()

		body := fmt.Sprintf("Fixes #%d\n\n## Commits\n```\n%s```\n\n🤖 Generated by issue-watcher agent", num, string(logOut))

		// Create PR
		prTitle := fmt.Sprintf("Fix #%d: %s", num, title)
		cmd = exec.Command("gh", "pr", "create",
			"--repo", repo,
			"--title", prTitle,
			"--body", body,
			"--head", branch,
		)
		cmd.Dir = workdir

		// Strip ANTHROPIC_API_KEY from env to avoid leaking
		env := os.Environ()
		filtered := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
				filtered = append(filtered, e)
			}
		}
		cmd.Env = filtered

		out, err := cmd.CombinedOutput()
		if err != nil {
			return prResultMsg{issueNum: num, err: fmt.Errorf("gh pr create failed: %s: %w", string(out), err)}
		}

		url := strings.TrimSpace(string(out))
		return prResultMsg{issueNum: num, url: url}
	}
}

func (m *Model) handlePRResult(msg prResultMsg) {
	if msg.err != nil {
		m.appendLog(msg.issueNum, "❌ PR failed: "+msg.err.Error())
	} else {
		m.appendLog(msg.issueNum, "")
		m.appendLog(msg.issueNum, "━━━ PR Created ━━━")
		m.appendLog(msg.issueNum, "  "+msg.url)
		m.appendLog(msg.issueNum, "━━━━━━━━━━━━━━━━━━")
	}
	m.updateLogViewport()
}

// generateCommit runs claude to write a commit message and commits.
func (m *Model) generateCommit() tea.Cmd {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	num := iss.Number
	workdir := iss.Workdir

	m.appendLog(num, "")
	m.appendLog(num, "📝 Generating commit message...")
	m.updateLogViewport()

	return func() tea.Msg {
		// Get the diff
		cmd := exec.Command("git", "diff")
		cmd.Dir = workdir
		diffOut, err := cmd.Output()
		if err != nil {
			return commitResultMsg{issueNum: num, err: fmt.Errorf("git diff: %w", err)}
		}

		// Also get staged diff
		cmd = exec.Command("git", "diff", "--cached")
		cmd.Dir = workdir
		stagedOut, _ := cmd.Output()

		diff := string(diffOut) + string(stagedOut)
		if strings.TrimSpace(diff) == "" {
			// Maybe everything is already committed, check for untracked
			cmd = exec.Command("git", "status", "--short")
			cmd.Dir = workdir
			statusOut, _ := cmd.Output()
			if strings.TrimSpace(string(statusOut)) == "" {
				return commitResultMsg{issueNum: num, err: fmt.Errorf("no changes to commit")}
			}
			diff = "Untracked files:\n" + string(statusOut)
		}

		// Truncate diff if very long
		if len(diff) > 8000 {
			diff = diff[:8000] + "\n... (truncated)"
		}

		prompt := fmt.Sprintf(`Write a concise git commit message for these changes.
Return ONLY the commit message, nothing else. No markdown, no explanation.
First line: short summary (max 72 chars).
Then a blank line, then a brief body if needed.

Diff:
%s`, diff)

		// Run claude to generate the message
		claudeCmd := exec.Command("claude", "-p")
		claudeCmd.Dir = workdir

		// Strip env vars
		env := os.Environ()
		filtered := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") &&
				!strings.HasPrefix(e, "CLAUDECODE=") {
				filtered = append(filtered, e)
			}
		}
		claudeCmd.Env = filtered

		claudeCmd.Stdin = strings.NewReader(prompt)
		msgOut, err := claudeCmd.Output()
		if err != nil {
			return commitResultMsg{issueNum: num, err: fmt.Errorf("claude commit msg: %w", err)}
		}

		commitMsg := strings.TrimSpace(string(msgOut))
		if commitMsg == "" {
			return commitResultMsg{issueNum: num, err: fmt.Errorf("claude returned empty message")}
		}

		// Stage all changes
		cmd = exec.Command("git", "add", "-A")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return commitResultMsg{issueNum: num, err: fmt.Errorf("git add: %s: %w", string(out), err)}
		}

		// Commit
		cmd = exec.Command("git", "commit", "-m", commitMsg)
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return commitResultMsg{issueNum: num, err: fmt.Errorf("git commit: %s: %w", string(out), err)}
		}

		return commitResultMsg{issueNum: num, message: commitMsg}
	}
}

func (m *Model) handleCommitResult(msg commitResultMsg) {
	if msg.err != nil {
		m.appendLog(msg.issueNum, "❌ Commit failed: "+msg.err.Error())
	} else {
		m.appendLog(msg.issueNum, "")
		m.appendLog(msg.issueNum, "━━━ Committed ━━━")
		for _, line := range strings.Split(msg.message, "\n") {
			m.appendLog(msg.issueNum, "  "+line)
		}
		m.appendLog(msg.issueNum, "━━━━━━━━━━━━━━━━━")
		m.appendLog(msg.issueNum, "  Press 'p' to open a PR")
	}
	m.updateLogViewport()
}

func (m *Model) handleEvent(ev watcher.Event) {
	switch ev.Kind {
	case watcher.EventPollStart:
		m.pollCount++
		m.lastPoll = ev.Timestamp

	case watcher.EventIssueFound:
		m.issues = append(m.issues, watcher.TrackedIssue{
			Number:    ev.IssueNum,
			Title:     ev.Text,
			Status:    watcher.StatusReacted,
			StartedAt: ev.Timestamp,
		})
		m.logs[ev.IssueNum] = []string{}
		m.appendLog(ev.IssueNum, "Issue discovered: "+ev.Text)

	case watcher.EventReacted:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusReacted)
		m.appendLog(ev.IssueNum, "👀 "+ev.Text)

	case watcher.EventCloneStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloning)
		m.appendLog(ev.IssueNum, "📦 "+ev.Text)

	case watcher.EventCloneDone:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloneReady)
		m.setWorkdir(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "📂 Cloned to "+ev.Text)

	case watcher.EventClaudeStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusClaudeRunning)
		m.activeCount++
		m.appendLog(ev.IssueNum, "🤖 "+ev.Text)

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
		m.appendLog(ev.IssueNum, "")
		m.appendLog(ev.IssueNum, "━━━ Ready for review ━━━")
		m.appendLog(ev.IssueNum, "  l  lazygit — review & commit")
		m.appendLog(ev.IssueNum, "  c  generate commit message")
		m.appendLog(ev.IssueNum, "  d  view diff")
		m.appendLog(ev.IssueNum, "  p  push & open PR")
		m.appendLog(ev.IssueNum, "  o  open in Finder")
		m.appendLog(ev.IssueNum, "━━━━━━━━━━━━━━━━━━━━━━━━")

	case watcher.EventError:
		if m.findIssueStatus(ev.IssueNum) == watcher.StatusClaudeRunning {
			if m.activeCount > 0 {
				m.activeCount--
			}
		}
		m.updateIssueStatus(ev.IssueNum, watcher.StatusFailed)
		m.failCount++
		m.setError(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "")
		m.appendLog(ev.IssueNum, "━━━ Failed ━━━")
		m.appendLog(ev.IssueNum, "  "+ev.Text)
		m.appendLog(ev.IssueNum, "━━━━━━━━━━━━━━")

	case watcher.EventPollDone:
		m.appendLog(0, ev.Text)
	}

	m.updateLogViewport()
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

func (m *Model) updateLogViewport() {
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		num := m.issues[m.cursor].Number
		content := ""
		for _, line := range m.logs[num] {
			content += line + "\n"
		}
		m.logViewport.SetContent(content)
		m.logViewport.GotoBottom()
	}
}

func (m *Model) openWorkdir() {
	if iss := m.selectedIssue(); iss != nil && iss.Workdir != "" {
		exec.Command("open", iss.Workdir).Start()
	}
}

func (m *Model) showDiff() {
	iss := m.selectedIssue()
	if iss == nil || iss.Workdir == "" {
		return
	}

	cmd := exec.Command("git", "diff")
	cmd.Dir = iss.Workdir
	out, err := cmd.Output()
	if err == nil {
		m.appendLog(iss.Number, "--- git diff ---")
		for _, line := range splitLines(string(out)) {
			m.appendLog(iss.Number, line)
		}
		m.appendLog(iss.Number, "--- end diff ---")
		m.updateLogViewport()
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// elapsed returns a human-readable elapsed time string.
func elapsed(start time.Time, now time.Time) string {
	d := now.Sub(start)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
