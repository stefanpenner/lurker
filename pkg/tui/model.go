package tui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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
	focusInput        // text input for adding repo
	focusFocus        // full-screen focus view of a single issue
)

// itemKind distinguishes tree items.
type itemKind int

const (
	itemRepo  itemKind = iota
	itemIssue
)

// listItem is one selectable row in the tree.
type listItem struct {
	kind     itemKind
	repo     string
	issueIdx int // index into Model.issues; -1 for repo items
}

func issueKey(repo string, num int) string {
	return watcher.IssueKey(repo, num)
}

// Model is the Bubbletea model for the TUI dashboard.
type Model struct {
	issues       []watcher.TrackedIssue
	logs         map[string][]string // per-issue log lines, keyed by "owner/repo#42"
	expanded     map[string]bool     // which issues have logs toggled open
	repoExpanded map[string]bool     // which repo folders are open
	repoErrors   map[string]string   // latest poll error per repo

	cursor    int   // index into visibleItems()
	focus     focus // current focus mode
	logScroll int   // scroll offset within focused issue's logs

	listScroll int // scroll offset for the entire issue list
	listHeight int // how many lines available for the issue list

	spinner   spinner.Model
	textInput textinput.Model
	width     int
	height    int
	manager   *watcher.Manager
	eventCh   <-chan watcher.Event

	// Dialog state
	dialogIssue *watcher.TrackedIssue

	// Focus view state
	focusIssue  *watcher.TrackedIssue
	focusScroll int

	// Counters
	lastPoll  time.Time
	pollCount int
	now       time.Time
}

// Messages
type eventMsg watcher.Event
type tickMsg struct{}

type prResultMsg struct {
	repo     string
	issueNum int
	url      string
	err      error
}

// NewModel creates a new TUI Model.
func NewModel(manager *watcher.Manager) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "owner/repo"
	ti.CharLimit = 100
	ti.Width = 40

	return Model{
		logs:         make(map[string][]string),
		expanded:     make(map[string]bool),
		repoExpanded: make(map[string]bool),
		repoErrors:   make(map[string]string),
		spinner:      s,
		textInput:    ti,
		manager:      manager,
		eventCh:      manager.EventCh(),
		now:          time.Now(),
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
		m.refreshFocusIssue()
		cmds = append(cmds, m.pollEvents())

	case tickMsg:
		m.now = time.Now()
		cmds = append(cmds, m.pollEvents())

	case prResultMsg:
		m.handlePRResult(msg)
	}

	// Forward messages to textinput when focused (for cursor blink etc.)
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// --- Tree helpers ---

func (m *Model) visibleItems() []listItem {
	var items []listItem
	repos := m.manager.Repos()
	for _, repo := range repos {
		items = append(items, listItem{kind: itemRepo, repo: repo, issueIdx: -1})
		if m.repoExpanded[repo] {
			for i, iss := range m.issues {
				if iss.Repo == repo {
					items = append(items, listItem{kind: itemIssue, repo: repo, issueIdx: i})
				}
			}
		}
	}
	return items
}

func (m *Model) cursorItem() *listItem {
	items := m.visibleItems()
	if m.cursor >= 0 && m.cursor < len(items) {
		item := items[m.cursor]
		return &item
	}
	return nil
}

func (m *Model) selectedIssue() *watcher.TrackedIssue {
	item := m.cursorItem()
	if item == nil || item.kind != itemIssue {
		return nil
	}
	if item.issueIdx >= 0 && item.issueIdx < len(m.issues) {
		return &m.issues[item.issueIdx]
	}
	return nil
}

func (m *Model) selectedRepo() string {
	item := m.cursorItem()
	if item == nil {
		return ""
	}
	return item.repo
}

// --- Key handling ---

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Text input mode
	if m.focus == focusInput {
		switch key {
		case "ctrl+c":
			return tea.Quit
		case "enter":
			repo := strings.TrimSpace(m.textInput.Value())
			if repo != "" {
				m.manager.AddRepo(repo)
				m.repoExpanded[repo] = true
			}
			m.textInput.Reset()
			m.textInput.Blur()
			m.focus = focusList
		case "esc":
			m.textInput.Reset()
			m.textInput.Blur()
			m.focus = focusList
		}
		return nil
	}

	// Dialog mode
	if m.focus == focusDialog {
		if key == "esc" {
			m.focus = focusList
			m.dialogIssue = nil
		}
		return nil
	}

	// Focus view mode
	if m.focus == focusFocus {
		switch key {
		case "ctrl+c":
			return tea.Quit
		case "esc":
			m.focus = focusList
			m.focusIssue = nil
		case "j", "down":
			m.focusScroll++
			m.clampFocusScroll()
		case "k", "up":
			if m.focusScroll > 0 {
				m.focusScroll--
			}
		case "G":
			m.focusScroll = 999999
			m.clampFocusScroll()
		case " ":
			m.toggleFocusIssueProcessing()
		case "o":
			if m.focusIssue != nil && m.focusIssue.URL != "" {
				exec.Command("open", m.focusIssue.URL).Start()
			}
		case "a":
			return m.approvePRFor(m.focusIssue)
		case "g":
			return m.launchLazygitFor(m.focusIssue)
		case "c":
			return m.launchClaudeFor(m.focusIssue)
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
			return m.approvePRFor(m.selectedIssue())
		case " ":
			m.toggleIssueProcessing()
		}
		return nil
	}

	// Normal list mode
	items := m.visibleItems()
	switch key {
	case "j", "down":
		if m.cursor < len(items)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
		}
	case "l", "enter":
		item := m.cursorItem()
		if item == nil {
			break
		}
		if item.kind == itemRepo {
			m.repoExpanded[item.repo] = !m.repoExpanded[item.repo]
		} else {
			m.toggleLogs()
		}
	case " ":
		item := m.cursorItem()
		if item == nil {
			break
		}
		if item.kind == itemRepo {
			m.repoExpanded[item.repo] = !m.repoExpanded[item.repo]
		} else {
			m.toggleIssueProcessing()
		}
	case "tab":
		if iss := m.selectedIssue(); iss != nil {
			key := issueKey(iss.Repo, iss.Number)
			if m.expanded[key] {
				m.focus = focusLogs
				m.logScroll = 999999
				m.clampLogScroll()
			}
		}
	case "f":
		if iss := m.selectedIssue(); iss != nil {
			m.focusIssue = iss
			m.focusScroll = 999999
			m.clampFocusScroll()
			m.focus = focusFocus
		}
	case "g":
		return m.launchLazygitFor(m.selectedIssue())
	case "c":
		return m.launchClaudeFor(m.selectedIssue())
	case "o":
		m.openGithubIssue()
	case "i":
		m.showDialog()
	case "a":
		return m.approvePRFor(m.selectedIssue())
	case "r":
		m.focus = focusInput
		return m.textInput.Focus()
	case "R", "d":
		m.removeSelectedRepo()
	}

	return nil
}

func (m *Model) toggleIssueProcessing() {
	iss := m.selectedIssue()
	if iss == nil {
		return
	}
	key := issueKey(iss.Repo, iss.Number)

	switch iss.Status {
	case watcher.StatusPending:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Started")
		m.expanded[key] = true
	case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
		m.manager.StopIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusPaused
		m.appendLog(key, "⏸ Paused")
	case watcher.StatusPaused:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Resumed")
		m.expanded[key] = true
	case watcher.StatusFailed:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		iss.Error = ""
		m.appendLog(key, "▶ Retrying")
		m.expanded[key] = true
	}
}

func (m *Model) toggleLogs() {
	if iss := m.selectedIssue(); iss != nil {
		key := issueKey(iss.Repo, iss.Number)
		m.expanded[key] = !m.expanded[key]
		if !m.expanded[key] {
			if m.focus == focusLogs {
				m.focus = focusList
			}
		}
	}
}

func (m *Model) refreshFocusIssue() {
	if m.focusIssue == nil {
		return
	}
	for i := range m.issues {
		if m.issues[i].Repo == m.focusIssue.Repo && m.issues[i].Number == m.focusIssue.Number {
			m.focusIssue = &m.issues[i]
			return
		}
	}
}

func (m *Model) clampFocusScroll() {
	if m.focusIssue == nil {
		return
	}
	key := issueKey(m.focusIssue.Repo, m.focusIssue.Number)
	lines := m.logs[key]
	visibleLines := m.height - 5 // header(1) + title(1) + sep(1) + sep(1) + footer(1)
	if visibleLines < 1 {
		visibleLines = 1
	}
	maxScroll := len(lines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.focusScroll > maxScroll {
		m.focusScroll = maxScroll
	}
}

func (m *Model) toggleFocusIssueProcessing() {
	iss := m.focusIssue
	if iss == nil {
		return
	}
	key := issueKey(iss.Repo, iss.Number)

	switch iss.Status {
	case watcher.StatusPending:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Started")
	case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
		m.manager.StopIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusPaused
		m.appendLog(key, "⏸ Paused")
	case watcher.StatusPaused:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Resumed")
	case watcher.StatusFailed:
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		iss.Error = ""
		m.appendLog(key, "▶ Retrying")
	}
}

func (m *Model) clampLogScroll() {
	iss := m.selectedIssue()
	if iss == nil {
		return
	}
	key := issueKey(iss.Repo, iss.Number)
	lines := m.logs[key]
	maxScroll := len(lines) - maxVisibleLogs
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.logScroll > maxScroll {
		m.logScroll = maxScroll
	}
}

func (m *Model) ensureCursorVisible() {
	items := m.visibleItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return
	}

	lineOffset := 0
	for i := 0; i < m.cursor; i++ {
		lineOffset++ // the item line itself
		if items[i].kind == itemRepo {
			if m.repoErrors[items[i].repo] != "" {
				lineOffset++ // error line beneath repo
			}
		} else if items[i].kind == itemIssue {
			iss := m.issues[items[i].issueIdx]
			key := issueKey(iss.Repo, iss.Number)
			if m.expanded[key] {
				n := len(m.logs[key])
				if n > maxVisibleLogs {
					n = maxVisibleLogs
				}
				lineOffset += n
			}
		}
	}

	if lineOffset < m.listScroll {
		m.listScroll = lineOffset
	}
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

func (m *Model) removeSelectedRepo() {
	repo := m.selectedRepo()
	if repo == "" {
		return
	}
	m.manager.RemoveRepo(repo)

	var remaining []watcher.TrackedIssue
	for _, issue := range m.issues {
		if issue.Repo != repo {
			remaining = append(remaining, issue)
		} else {
			key := issueKey(issue.Repo, issue.Number)
			delete(m.logs, key)
			delete(m.expanded, key)
		}
	}
	m.issues = remaining
	delete(m.repoExpanded, repo)
	delete(m.repoErrors, repo)

	items := m.visibleItems()
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) launchLazygitFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}
	c := exec.Command("lazygit")
	c.Dir = iss.Workdir
	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

func (m *Model) launchClaudeFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}
	c := exec.Command("claude")
	c.Dir = iss.Workdir
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

func (m *Model) approvePRFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	num := iss.Number
	title := iss.Title
	workdir := iss.Workdir
	repo := iss.Repo

	key := issueKey(repo, num)
	m.appendLog(key, "")
	m.appendLog(key, "🚀 Pushing branch & creating PR...")

	return func() tea.Msg {
		cmd := exec.Command("git", "push", "-u", "origin", "HEAD")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return prResultMsg{repo: repo, issueNum: num, err: fmt.Errorf("push: %s: %w", strings.TrimSpace(string(out)), err)}
		}

		cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = workdir
		branchOut, err := cmd.Output()
		if err != nil {
			return prResultMsg{repo: repo, issueNum: num, err: fmt.Errorf("branch: %w", err)}
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
			return prResultMsg{repo: repo, issueNum: num, err: fmt.Errorf("pr: %s: %w", strings.TrimSpace(string(out)), err)}
		}

		return prResultMsg{repo: repo, issueNum: num, url: strings.TrimSpace(string(out))}
	}
}

func (m *Model) handlePRResult(msg prResultMsg) {
	key := issueKey(msg.repo, msg.issueNum)
	if msg.err != nil {
		m.appendLog(key, "❌ "+msg.err.Error())
	} else {
		m.appendLog(key, "✅ PR: "+msg.url)
		m.expanded[key] = true
	}
}

// --- Event handling ---

func (m *Model) handleEvent(ev watcher.Event) {
	key := issueKey(ev.Repo, ev.IssueNum)

	// Ignore processing events for paused issues (stale from cancelled ctx)
	if ev.IssueNum > 0 && ev.Kind != watcher.EventIssueFound {
		if m.findIssueStatus(ev.Repo, ev.IssueNum) == watcher.StatusPaused {
			return
		}
	}

	switch ev.Kind {
	case watcher.EventPollStart:
		m.pollCount++
		m.lastPoll = ev.Timestamp

	case watcher.EventIssueFound:
		// Auto-expand repo folder for first issue
		if _, ok := m.repoExpanded[ev.Repo]; !ok {
			m.repoExpanded[ev.Repo] = true
		}
		status, workdir := watcher.DeriveIssueStatus(m.manager.BaseDir(), ev.Repo, ev.IssueNum)
		m.issues = append(m.issues, watcher.TrackedIssue{
			Repo:      ev.Repo,
			Number:    ev.IssueNum,
			Title:     ev.Text,
			Body:      ev.IssueBody,
			Labels:    ev.IssueLabels,
			URL:       ev.IssueURL,
			Status:    status,
			Workdir:   workdir,
			StartedAt: ev.Timestamp,
		})
		if logs := m.loadPersistedLogs(ev.Repo, ev.IssueNum); logs != nil {
			m.logs[key] = logs
		} else {
			m.logs[key] = []string{}
		}

	case watcher.EventReacted:
		m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusReacted)
		m.appendLog(key, "👀 Reacted")

	case watcher.EventCloneStart:
		m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusCloning)
		m.appendLog(key, "📦 Cloning...")

	case watcher.EventCloneDone:
		m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusCloneReady)
		m.setWorkdir(ev.Repo, ev.IssueNum, ev.Text)
		m.appendLog(key, "📂 "+ev.Text)

	case watcher.EventClaudeStart:
		m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusClaudeRunning)
		m.appendLog(key, "🤖 Claude working...")
		m.expanded[key] = true

	case watcher.EventClaudeLog:
		m.appendLog(key, "  "+ev.Text)

	case watcher.EventClaudeDone:
		m.appendLog(key, ev.Text)

	case watcher.EventReady:
		m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusReady)
		m.appendLog(key, "✅ Ready — press 'a' to approve & open PR")

	case watcher.EventError:
		if ev.IssueNum == 0 {
			// Repo-level error (e.g. poll failure, bad repo name)
			m.repoErrors[ev.Repo] = ev.Text
		} else {
			m.updateIssueStatus(ev.Repo, ev.IssueNum, watcher.StatusFailed)
			m.setError(ev.Repo, ev.IssueNum, ev.Text)
			m.appendLog(key, "❌ "+ev.Text)
		}

	case watcher.EventPollDone:
		// Successful poll clears any repo-level error
		delete(m.repoErrors, ev.Repo)
	}
}

func (m *Model) findIssueStatus(repo string, num int) watcher.IssueStatus {
	for _, iss := range m.issues {
		if iss.Repo == repo && iss.Number == num {
			return iss.Status
		}
	}
	return watcher.StatusPending
}

func (m *Model) updateIssueStatus(repo string, num int, status watcher.IssueStatus) {
	for i := range m.issues {
		if m.issues[i].Repo == repo && m.issues[i].Number == num {
			m.issues[i].Status = status
			return
		}
	}
}

func (m *Model) setWorkdir(repo string, num int, dir string) {
	for i := range m.issues {
		if m.issues[i].Repo == repo && m.issues[i].Number == num {
			m.issues[i].Workdir = dir
			return
		}
	}
}

func (m *Model) setError(repo string, num int, errText string) {
	for i := range m.issues {
		if m.issues[i].Repo == repo && m.issues[i].Number == num {
			m.issues[i].Error = errText
			return
		}
	}
}

func (m *Model) appendLog(key string, line string) {
	if m.logs[key] == nil {
		m.logs[key] = []string{}
	}
	m.logs[key] = append(m.logs[key], line)
	if len(m.logs[key]) > maxLogLines {
		m.logs[key] = m.logs[key][len(m.logs[key])-maxLogLines:]
	}
	repo, num := parseIssueKey(key)
	if repo != "" {
		m.persistLogLine(repo, num, line)
	}
}

func parseIssueKey(key string) (string, int) {
	idx := strings.LastIndex(key, "#")
	if idx < 0 {
		return "", 0
	}
	repo := key[:idx]
	var num int
	fmt.Sscanf(key[idx+1:], "%d", &num)
	return repo, num
}

func (m *Model) logFilePath(repo string, num int) string {
	return filepath.Join(m.manager.BaseDir(), repo, fmt.Sprintf("%d", num), "lurker.log")
}

func (m *Model) persistLogLine(repo string, num int, line string) {
	p := m.logFilePath(repo, num)
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	f.WriteString(line + "\n")
	f.Close()
}

func (m *Model) loadPersistedLogs(repo string, num int) []string {
	p := m.logFilePath(repo, num)
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
	}
	return lines
}

// --- Counts (computed from issues slice) ---

func (m Model) countActive() int {
	n := 0
	for _, iss := range m.issues {
		switch iss.Status {
		case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
			n++
		}
	}
	return n
}

func (m Model) countByStatus(s watcher.IssueStatus) int {
	n := 0
	for _, iss := range m.issues {
		if iss.Status == s {
			n++
		}
	}
	return n
}

func (m Model) countIssuesForRepo(repo string) int {
	n := 0
	for _, iss := range m.issues {
		if iss.Repo == repo {
			n++
		}
	}
	return n
}

func elapsed(start time.Time, now time.Time) string {
	d := now.Sub(start)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
