package tui

import (
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanpenner/issue-watcher/pkg/watcher"
)

const maxLogLines = 500

// Model is the Bubbletea model for the TUI dashboard.
type Model struct {
	issues      []watcher.TrackedIssue
	logs        map[int][]string // per-issue log lines
	cursor      int
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
}

// eventMsg wraps a watcher.Event for Bubbletea.
type eventMsg watcher.Event

// tickMsg triggers periodic event channel reads.
type tickMsg struct{}

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
		switch {
		case msg.String() == "q" || msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "j" || msg.String() == "down":
			if m.cursor < len(m.issues)-1 {
				m.cursor++
				m.updateLogViewport()
			}
		case msg.String() == "k" || msg.String() == "up":
			if m.cursor > 0 {
				m.cursor--
				m.updateLogViewport()
			}
		case msg.String() == "o":
			m.openWorkdir()
		case msg.String() == "d":
			m.showDiff()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logViewport.Width = msg.Width - 4
		logHeight := msg.Height - 14 // leave room for header, table, footer
		if logHeight < 3 {
			logHeight = 3
		}
		m.logViewport.Height = logHeight

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case eventMsg:
		m.handleEvent(watcher.Event(msg))
		cmds = append(cmds, m.pollEvents())

	case tickMsg:
		cmds = append(cmds, m.pollEvents())
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleEvent(ev watcher.Event) {
	switch ev.Kind {
	case watcher.EventPollStart:
		m.pollCount++
		m.lastPoll = ev.Timestamp

	case watcher.EventIssueFound:
		m.issues = append(m.issues, watcher.TrackedIssue{
			Number: ev.IssueNum,
			Title:  ev.Text,
			Status: watcher.StatusReacted,
		})
		m.logs[ev.IssueNum] = []string{}

	case watcher.EventReacted:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusReacted)
		m.appendLog(ev.IssueNum, ev.Text)

	case watcher.EventCloneStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloning)
		m.appendLog(ev.IssueNum, ev.Text)

	case watcher.EventCloneDone:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusCloneReady)
		m.setWorkdir(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "Clone complete: "+ev.Text)

	case watcher.EventClaudeStart:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusClaudeRunning)
		m.activeCount++
		m.appendLog(ev.IssueNum, ev.Text)

	case watcher.EventClaudeLog:
		m.appendLog(ev.IssueNum, "> "+ev.Text)

	case watcher.EventClaudeDone:
		m.activeCount--
		m.appendLog(ev.IssueNum, ev.Text)

	case watcher.EventReady:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusReady)
		m.appendLog(ev.IssueNum, "Ready for review!")

	case watcher.EventError:
		m.updateIssueStatus(ev.IssueNum, watcher.StatusFailed)
		m.failCount++
		m.setError(ev.IssueNum, ev.Text)
		m.appendLog(ev.IssueNum, "ERROR: "+ev.Text)

	case watcher.EventPollDone:
		m.appendLog(0, ev.Text)
	}

	m.updateLogViewport()
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
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		dir := m.issues[m.cursor].Workdir
		if dir != "" {
			exec.Command("open", dir).Start()
		}
	}
}

func (m *Model) showDiff() {
	if m.cursor >= 0 && m.cursor < len(m.issues) {
		dir := m.issues[m.cursor].Workdir
		if dir != "" {
			cmd := exec.Command("git", "diff")
			cmd.Dir = dir
			out, err := cmd.Output()
			if err == nil {
				num := m.issues[m.cursor].Number
				m.appendLog(num, "--- git diff ---")
				for _, line := range splitLines(string(out)) {
					m.appendLog(num, line)
				}
				m.appendLog(num, "--- end diff ---")
				m.updateLogViewport()
			}
		}
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
