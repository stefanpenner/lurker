package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// EventKind identifies the type of watcher event.
type EventKind int

const (
	EventPollStart   EventKind = iota
	EventPollDone              // found N new issues
	EventIssueFound            // new issue detected
	EventReacted               // 👀 added
	EventCloneStart            // git clone starting
	EventCloneDone             // clone finished
	EventClaudeStart           // claude invoked
	EventClaudeLog             // line of claude output
	EventClaudeDone            // claude finished (success/fail)
	EventReady                 // branch ready for review
	EventError                 // something failed
)

// Event is sent from the watcher to the TUI.
type Event struct {
	Kind      EventKind
	IssueNum  int
	Text      string
	Timestamp time.Time
}

// IssueStatus tracks the lifecycle of an issue being processed.
type IssueStatus int

const (
	StatusReacted      IssueStatus = iota
	StatusCloning
	StatusCloneReady
	StatusClaudeRunning
	StatusReady
	StatusFailed
)

func (s IssueStatus) String() string {
	switch s {
	case StatusReacted:
		return "reacted"
	case StatusCloning:
		return "cloning"
	case StatusCloneReady:
		return "cloned"
	case StatusClaudeRunning:
		return "claude"
	case StatusReady:
		return "ready"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// TrackedIssue represents an issue being processed by the watcher.
type TrackedIssue struct {
	Number  int
	Title   string
	Status  IssueStatus
	Workdir string
	Error   string
}

// persistedState is saved to state.json to remember processed issues across runs.
type persistedState struct {
	Processed []int `json:"processed"`
}

// Config holds watcher configuration.
type Config struct {
	Repo         string
	PollInterval time.Duration
	BaseDir      string // e.g. ~/.local/share/issue-watcher/
}

// Watcher polls GitHub for new issues and orchestrates processing.
type Watcher struct {
	cfg       Config
	processed map[int]bool
	stateFile string
}

// New creates a new Watcher.
func New(cfg Config) (*Watcher, error) {
	// Ensure base directory exists
	repoDir := filepath.Join(cfg.BaseDir, cfg.Repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating workdir: %w", err)
	}

	w := &Watcher{
		cfg:       cfg,
		processed: make(map[int]bool),
		stateFile: filepath.Join(repoDir, "state.json"),
	}

	w.loadState()
	return w, nil
}

func (w *Watcher) loadState() {
	data, err := os.ReadFile(w.stateFile)
	if err != nil {
		return // no state file yet, that's fine
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	for _, n := range state.Processed {
		w.processed[n] = true
	}
}

func (w *Watcher) saveState() error {
	state := persistedState{}
	for n := range w.processed {
		state.Processed = append(state.Processed, n)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.stateFile, data, 0o644)
}

func (w *Watcher) emit(ch chan<- Event, kind EventKind, issueNum int, text string) {
	ch <- Event{
		Kind:      kind,
		IssueNum:  issueNum,
		Text:      text,
		Timestamp: time.Now(),
	}
}

// Run starts the poll loop. It sends events to eventCh for the TUI to consume.
// It blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context, eventCh chan<- Event) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Do an immediate poll
	w.poll(ctx, eventCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx, eventCh)
		}
	}
}

func (w *Watcher) poll(ctx context.Context, eventCh chan<- Event) {
	w.emit(eventCh, EventPollStart, 0, "Polling for new issues...")

	issues, err := FetchOpenIssues(w.cfg.Repo)
	if err != nil {
		w.emit(eventCh, EventError, 0, fmt.Sprintf("Poll failed: %v", err))
		return
	}

	var newIssues []Issue
	for _, iss := range issues {
		if !w.processed[iss.Number] {
			newIssues = append(newIssues, iss)
		}
	}

	w.emit(eventCh, EventPollDone, 0, fmt.Sprintf("Found %d new issues (of %d open)", len(newIssues), len(issues)))

	for _, iss := range newIssues {
		if ctx.Err() != nil {
			return
		}
		w.processIssue(ctx, eventCh, iss)
	}
}

func (w *Watcher) processIssue(ctx context.Context, eventCh chan<- Event, issue Issue) {
	num := issue.Number

	// Mark as processed immediately to avoid re-processing
	w.processed[num] = true
	_ = w.saveState()

	w.emit(eventCh, EventIssueFound, num, issue.Title)

	// React with eyes
	if err := AddReaction(w.cfg.Repo, num); err != nil {
		w.emit(eventCh, EventError, num, fmt.Sprintf("React failed: %v", err))
		// Continue anyway
	} else {
		w.emit(eventCh, EventReacted, num, "Added 👀 reaction")
	}

	// Clone
	issueDir := filepath.Join(w.cfg.BaseDir, w.cfg.Repo, fmt.Sprintf("%d", num))
	repoName := filepath.Base(w.cfg.Repo)
	workdir := filepath.Join(issueDir, repoName)

	w.emit(eventCh, EventCloneStart, num, "Cloning repository...")

	if err := w.cloneRepo(ctx, issueDir, workdir, num); err != nil {
		w.emit(eventCh, EventError, num, fmt.Sprintf("Clone failed: %v", err))
		return
	}

	w.emit(eventCh, EventCloneDone, num, workdir)

	// Run Claude
	w.emit(eventCh, EventClaudeStart, num, "Running Claude Code...")

	prompt := BuildClaudePrompt(issue)
	logFn := func(line string) {
		w.emit(eventCh, EventClaudeLog, num, line)
	}

	_, err := RunClaude(ctx, workdir, prompt, logFn)
	if err != nil {
		w.emit(eventCh, EventClaudeDone, num, fmt.Sprintf("Claude failed: %v", err))
		w.emit(eventCh, EventError, num, err.Error())
		return
	}

	w.emit(eventCh, EventClaudeDone, num, "Claude finished successfully")
	w.emit(eventCh, EventReady, num, workdir)
}

func (w *Watcher) cloneRepo(ctx context.Context, issueDir, workdir string, issueNum int) error {
	// If workdir already exists, just fetch + reset
	if _, err := os.Stat(workdir); err == nil {
		cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %s: %w", string(out), err)
		}
		return nil
	}

	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "gh", "repo", "clone", w.cfg.Repo, workdir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone: %s: %w", string(out), err)
	}

	// Create a branch for this issue
	branch := fmt.Sprintf("agent/issue-%d", issueNum)
	cmd = exec.CommandContext(ctx, "git", "checkout", "-b", branch)
	cmd.Dir = workdir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("checkout: %s: %w", string(out), err)
	}

	return nil
}
