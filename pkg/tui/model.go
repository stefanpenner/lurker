package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanpenner/lurker/pkg/github"
	"github.com/stefanpenner/lurker/pkg/watcher"
)

const (
	maxLogLines    = 500
	maxVisibleLogs = 15 // max log lines shown when expanded
)

// focus tracks which panel has keyboard focus.
type focus int

const (
	focusList    focus = iota
	focusLogs          // scrolling within an expanded issue's logs (legacy, kept for focus view)
	focusDialog        // detail dialog open
	focusInput         // text input for adding repo
	focusFocus         // full-screen focus view of a single issue
	focusHelp          // help screen overlay
	focusConfirm       // confirmation dialog (e.g. remove repo)
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
	confirmRepo string // repo pending removal confirmation

	// Focus view state
	focusIssue  *watcher.TrackedIssue
	focusScroll int

	// GitHub API client
	ghClient *github.Client

	// Persistent shell sessions (PTY per issue)
	ptySessions map[string]*ptySession

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
func NewModel(manager *watcher.Manager, ghClient *github.Client) Model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot

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
		ghClient:     ghClient,
		eventCh:      manager.EventCh(),
		ptySessions:  make(map[string]*ptySession),
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
	prevFocus := m.focus

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

	case interactiveClaudeDoneMsg:
		m.handleInteractiveReturn(msg)
	}

	// Forward messages to textinput when focused, but skip the keypress
	// that activated input mode (otherwise 'r' leaks into the field).
	if m.focus == focusInput {
		_, isKey := msg.(tea.KeyMsg)
		if isKey && prevFocus != focusInput {
			// Skip — this is the keypress that triggered input mode
		} else {
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
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

	// Confirmation dialog
	if m.focus == focusConfirm {
		switch key {
		case "y", "enter":
			m.removeSelectedRepoConfirmed()
			m.focus = focusList
		case "n", "esc":
			m.confirmRepo = ""
			m.focus = focusList
		}
		return nil
	}

	// Dialog mode (info or help)
	if m.focus == focusDialog || m.focus == focusHelp {
		if key == "esc" || key == "?" {
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
		case "t":
			return m.takeoverClaudeFor(m.focusIssue)
		case "s":
			return m.launchShellFor(m.focusIssue)
		}
		return nil
	}

	// Quit always works
	if key == "q" || key == "ctrl+c" {
		return tea.Quit
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
		} else if iss := m.selectedIssue(); iss != nil {
			m.focusIssue = iss
			m.focusScroll = 999999
			m.clampFocusScroll()
			m.focus = focusFocus
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
	case "f":
		if iss := m.selectedIssue(); iss != nil {
			m.focusIssue = iss
			m.focusScroll = 999999
			m.clampFocusScroll()
			m.focus = focusFocus
		}
	case "s":
		return m.launchShellFor(m.selectedIssue())
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
		if repo := m.selectedRepo(); repo != "" {
			m.confirmRepo = repo
			m.focus = focusConfirm
		}
	case "?":
		m.focus = focusHelp
	}

	return nil
}

func (m *Model) ensurePtySession(key string, workdir string) {
	if s := m.ptySessions[key]; s != nil && !s.isDone() {
		return
	}
	session, err := newPtySession(workdir)
	if err != nil {
		m.appendLog(key, "PTY: "+err.Error())
		return
	}
	m.ptySessions[key] = session
	m.manager.SetIssuePTY(key, session)
	m.appendLog(key, fmt.Sprintf("PTY created [%s] in %s", key, workdir))
}

func (m *Model) ptyWorkdir(iss *watcher.TrackedIssue) string {
	if iss.Workdir != "" {
		return iss.Workdir
	}
	// Use the issue-specific directory so each PTY starts in its own space.
	// Create it eagerly — processIssue will create subdirs within it.
	dir := filepath.Join(m.manager.BaseDir(), iss.Repo, fmt.Sprintf("%d", iss.Number))
	os.MkdirAll(dir, 0o755)
	return dir
}

func (m *Model) toggleIssueProcessing() {
	iss := m.selectedIssue()
	if iss == nil {
		return
	}
	key := issueKey(iss.Repo, iss.Number)

	switch iss.Status {
	case watcher.StatusPending:
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Started")
		m.expanded[key] = true
	case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
		m.manager.StopIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusPaused
		m.appendLog(key, "⏸ Paused")
	case watcher.StatusPaused:
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Resumed")
		m.expanded[key] = true
	case watcher.StatusFailed:
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		iss.Error = ""
		m.appendLog(key, "▶ Retrying")
		m.expanded[key] = true
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
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Started")
	case watcher.StatusReacted, watcher.StatusCloning, watcher.StatusCloneReady, watcher.StatusClaudeRunning:
		m.manager.StopIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusPaused
		m.appendLog(key, "⏸ Paused")
	case watcher.StatusPaused:
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		m.appendLog(key, "▶ Resumed")
	case watcher.StatusFailed:
		m.ensurePtySession(key, m.ptyWorkdir(iss))
		m.manager.StartIssue(iss.Repo, iss.Number)
		iss.Status = watcher.StatusReacted
		iss.Error = ""
		m.appendLog(key, "▶ Retrying")
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
		if items[i].kind == itemRepo && m.repoErrors[items[i].repo] != "" {
			lineOffset++ // error line beneath repo
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

func (m *Model) removeSelectedRepoConfirmed() {
	repo := m.confirmRepo
	m.confirmRepo = ""
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
			if s := m.ptySessions[key]; s != nil {
				s.cmd.Process.Signal(syscall.SIGHUP)
				delete(m.ptySessions, key)
			}
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

func (m *Model) launchShellFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	key := issueKey(iss.Repo, iss.Number)

	session := m.ptySessions[key]
	if session == nil || session.isDone() {
		// No PTY exists yet — create one (starts shell automatically)
		var err error
		session, err = newPtySession(iss.Workdir)
		if err != nil {
			m.appendLog(key, "PTY: "+err.Error())
			return nil
		}
		m.ptySessions[key] = session
		m.manager.SetIssuePTY(key, session)
	}

	return tea.Exec(&ptyAttacher{session: session, label: key}, func(err error) tea.Msg {
		return nil
	})
}

func (m *Model) launchClaudeFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	key := issueKey(iss.Repo, iss.Number)
	session := m.ptySessions[key]
	if session == nil || session.isDone() {
		var err error
		session, err = newPtySession(iss.Workdir)
		if err != nil {
			m.appendLog(key, "PTY: "+err.Error())
			return nil
		}
		m.ptySessions[key] = session
		m.manager.SetIssuePTY(key, session)
	}

	// Send claude command to the PTY shell, then attach
	session.ptmx.Write([]byte("cd " + iss.Workdir + " && env -u ANTHROPIC_API_KEY -u CLAUDECODE claude\n"))

	return tea.Exec(&ptyAttacher{session: session, label: key}, func(err error) tea.Msg {
		return nil
	})
}

// interactiveClaudeDoneMsg is sent when the user exits an interactive Claude session.
type interactiveClaudeDoneMsg struct {
	repo    string
	num     int
	workdir string
}

func (m *Model) takeoverClaudeFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	key := issueKey(iss.Repo, iss.Number)

	// Stop automated processing if running
	if isActive(iss.Status) {
		m.manager.StopIssue(iss.Repo, iss.Number)
		m.appendLog(key, "⏸ Pausing automation — launching interactive session...")
	}

	session := m.ptySessions[key]
	if session == nil || session.isDone() {
		var err error
		session, err = newPtySession(iss.Workdir)
		if err != nil {
			m.appendLog(key, "PTY: "+err.Error())
			return nil
		}
		m.ptySessions[key] = session
		m.manager.SetIssuePTY(key, session)
	}

	// Send claude --continue to the PTY shell, then attach
	session.ptmx.Write([]byte("cd " + iss.Workdir + " && env -u ANTHROPIC_API_KEY -u CLAUDECODE claude --continue\n"))

	return tea.Exec(&ptyAttacher{session: session, label: key}, func(err error) tea.Msg {
		return interactiveClaudeDoneMsg{repo: iss.Repo, num: iss.Number, workdir: iss.Workdir}
	})
}

func (m *Model) approvePRFor(iss *watcher.TrackedIssue) tea.Cmd {
	if iss == nil || iss.Workdir == "" {
		return nil
	}

	num := iss.Number
	title := iss.Title
	workdir := iss.Workdir
	repo := iss.Repo
	ghClient := m.ghClient

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
		pr, err := ghClient.CreatePR(context.Background(), github.CreatePRRequest{
			Repo:  repo,
			Title: prTitle,
			Body:  body,
			Head:  branch,
			Base:  "main",
		})
		if err != nil {
			return prResultMsg{repo: repo, issueNum: num, err: fmt.Errorf("pr: %w", err)}
		}

		return prResultMsg{repo: repo, issueNum: num, url: pr.HTMLURL}
	}
}

func (m *Model) handleInteractiveReturn(msg interactiveClaudeDoneMsg) {
	key := issueKey(msg.repo, msg.num)

	// Check if the branch has commits beyond origin/main
	branch := fmt.Sprintf("agent/issue-%d", msg.num)
	cmd := exec.Command("git", "log", "--oneline", "origin/main.."+branch)
	cmd.Dir = msg.workdir
	out, err := cmd.Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		m.updateIssueStatus(msg.repo, msg.num, watcher.StatusReady)
		m.appendLog(key, "✅ Interactive session done — ready for review")
	} else {
		// No new commits — mark as clone-ready so user can restart
		m.updateIssueStatus(msg.repo, msg.num, watcher.StatusCloneReady)
		m.appendLog(key, "Interactive session ended")
	}
	m.refreshFocusIssue()
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

	// Check if focus view should auto-scroll (tail-follow)
	autoScroll := false
	if m.focus == focusFocus && m.focusIssue != nil {
		fKey := issueKey(m.focusIssue.Repo, m.focusIssue.Number)
		if fKey == key {
			visibleLines := m.height - 5
			if visibleLines < 1 {
				visibleLines = 1
			}
			maxScroll := len(m.logs[key]) - visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			autoScroll = m.focusScroll >= maxScroll
		}
	}

	m.logs[key] = append(m.logs[key], line)
	if len(m.logs[key]) > maxLogLines {
		m.logs[key] = m.logs[key][len(m.logs[key])-maxLogLines:]
	}

	if autoScroll {
		m.focusScroll = 999999
		m.clampFocusScroll()
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
