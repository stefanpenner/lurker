package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	Repo      string
	IssueNum  int
	Text      string
	Timestamp time.Time
	// Extra fields for EventIssueFound
	IssueURL    string
	IssueBody   string
	IssueLabels string
}

// IssueStatus tracks the lifecycle of an issue being processed.
type IssueStatus int

const (
	StatusPending     IssueStatus = iota // discovered, waiting for user to start
	StatusReacted                        // processing started
	StatusCloning
	StatusCloneReady
	StatusClaudeRunning
	StatusReady
	StatusFailed
	StatusPaused // user paused processing
)

func (s IssueStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
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
	case StatusPaused:
		return "paused"
	default:
		return "unknown"
	}
}

// IssueKey returns a composite key: "owner/repo#42".
func IssueKey(repo string, num int) string {
	return fmt.Sprintf("%s#%d", repo, num)
}

// TrackedIssue represents an issue being processed by the watcher.
type TrackedIssue struct {
	Repo      string
	Number    int
	Title     string
	Body      string
	Labels    string
	URL       string
	Status    IssueStatus
	Workdir   string
	Error     string
	StartedAt time.Time
}

// State is persisted to disk to remember repos and processed issues.
type State struct {
	Repos     []string         `json:"repos"`
	Processed map[string][]int `json:"processed"`
}

// Manager manages multiple repo watchers.
type Manager struct {
	baseDir      string
	pollInterval time.Duration
	eventCh      chan Event
	mu           sync.Mutex
	watchers     map[string]context.CancelFunc
	repoWatchers map[string]*Watcher
	knownIssues  map[string]Issue
	issueCtxs    map[string]context.CancelFunc
	state        State
	statePath    string
}

// NewManager creates a Manager, loading persisted state from disk.
func NewManager(baseDir string, pollInterval time.Duration) (*Manager, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating base dir: %w", err)
	}

	statePath := filepath.Join(baseDir, "state.json")
	state := loadState(statePath)

	return &Manager{
		baseDir:      baseDir,
		pollInterval: pollInterval,
		eventCh:      make(chan Event, 100),
		watchers:     make(map[string]context.CancelFunc),
		repoWatchers: make(map[string]*Watcher),
		knownIssues:  make(map[string]Issue),
		issueCtxs:    make(map[string]context.CancelFunc),
		state:        state,
		statePath:    statePath,
	}, nil
}

// Start begins polling for all persisted repos.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, repo := range m.state.Repos {
		m.startWatcher(repo)
	}
}

// AddRepo adds a repo to the watched list and starts polling it.
func (m *Manager) AddRepo(repo string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.watchers[repo]; exists {
		return nil
	}

	repoDir := filepath.Join(m.baseDir, repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return fmt.Errorf("creating workdir: %w", err)
	}

	m.state.Repos = append(m.state.Repos, repo)
	if err := m.saveState(); err != nil {
		return err
	}

	m.startWatcher(repo)
	return nil
}

// RemoveRepo stops watching a repo and removes it from persisted state.
func (m *Manager) RemoveRepo(repo string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, exists := m.watchers[repo]; exists {
		cancel()
		delete(m.watchers, repo)
	}
	delete(m.repoWatchers, repo)

	// Cancel all issue processing for this repo
	prefix := repo + "#"
	for key, cancel := range m.issueCtxs {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			cancel()
			delete(m.issueCtxs, key)
		}
	}
	for key := range m.knownIssues {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(m.knownIssues, key)
		}
	}

	for i, r := range m.state.Repos {
		if r == repo {
			m.state.Repos = append(m.state.Repos[:i], m.state.Repos[i+1:]...)
			break
		}
	}
	delete(m.state.Processed, repo)

	return m.saveState()
}

// BaseDir returns the base directory for workdirs.
func (m *Manager) BaseDir() string {
	return m.baseDir
}

// DeriveIssueStatus checks the filesystem to determine what status an issue
// should have on restart. Returns the derived status and workdir path.
func DeriveIssueStatus(baseDir, repo string, num int) (IssueStatus, string) {
	repoName := filepath.Base(repo)
	workdir := filepath.Join(baseDir, repo, fmt.Sprintf("%d", num), repoName)

	if _, err := os.Stat(workdir); err != nil {
		return StatusPending, ""
	}

	// Workdir exists — check if branch has commits beyond origin/main
	branch := fmt.Sprintf("agent/issue-%d", num)
	cmd := exec.Command("git", "log", "--oneline", "origin/main.."+branch)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return StatusReady, workdir
	}

	return StatusCloneReady, workdir
}

// EventCh returns the channel that receives events from all watchers.
func (m *Manager) EventCh() <-chan Event {
	return m.eventCh
}

// Repos returns the current list of watched repos.
func (m *Manager) Repos() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	repos := make([]string, len(m.state.Repos))
	copy(repos, m.state.Repos)
	return repos
}

// IsProcessed checks whether an issue has been discovered/processed.
func (m *Manager) IsProcessed(repo string, num int) bool {
	m.mu.Lock()
	for _, n := range m.state.Processed[repo] {
		if n == num {
			m.mu.Unlock()
			return true
		}
	}
	m.mu.Unlock()

	// Backwards compat: check workdir existence
	issueDir := filepath.Join(m.baseDir, repo, fmt.Sprintf("%d", num))
	_, err := os.Stat(issueDir)
	return err == nil
}

// MarkProcessed records an issue as discovered and persists to disk.
func (m *Manager) MarkProcessed(repo string, num int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Processed == nil {
		m.state.Processed = make(map[string][]int)
	}
	m.state.Processed[repo] = append(m.state.Processed[repo], num)
	m.saveState()
}

// IsKnown checks whether an issue has already been seen this session.
func (m *Manager) IsKnown(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.knownIssues[key]
	return ok
}

// StoreIssue saves a discovered issue so StartIssue can look it up later.
func (m *Manager) StoreIssue(repo string, issue Issue) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.knownIssues[IssueKey(repo, issue.Number)] = issue
}

// StartIssue begins processing a specific issue (react, clone, claude).
func (m *Manager) StartIssue(repo string, num int) {
	m.mu.Lock()
	key := IssueKey(repo, num)
	issue, ok := m.knownIssues[key]
	if !ok {
		m.mu.Unlock()
		return
	}

	if cancel, ok := m.issueCtxs[key]; ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.issueCtxs[key] = cancel
	w := m.repoWatchers[repo]
	m.mu.Unlock()

	if w == nil {
		cancel()
		return
	}

	go w.processIssue(ctx, m.eventCh, issue)
}

// StopIssue cancels processing of a specific issue.
func (m *Manager) StopIssue(repo string, num int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := IssueKey(repo, num)
	if cancel, ok := m.issueCtxs[key]; ok {
		cancel()
		delete(m.issueCtxs, key)
	}
}

// Stop stops all watchers and issue processing.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for repo, cancel := range m.watchers {
		cancel()
		delete(m.watchers, repo)
	}
	for key, cancel := range m.issueCtxs {
		cancel()
		delete(m.issueCtxs, key)
	}
}

func (m *Manager) startWatcher(repo string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.watchers[repo] = cancel

	cfg := Config{
		Repo:         repo,
		PollInterval: m.pollInterval,
		BaseDir:      m.baseDir,
	}

	w := &Watcher{cfg: cfg, manager: m}
	m.repoWatchers[repo] = w
	go w.Run(ctx, m.eventCh)
}

func loadState(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{Repos: []string{}, Processed: make(map[string][]int)}
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{Repos: []string{}, Processed: make(map[string][]int)}
	}
	if s.Processed == nil {
		s.Processed = make(map[string][]int)
	}
	return s
}

func (m *Manager) saveState() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	// Atomic write: write to temp file then rename to avoid corruption on crash.
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp state: %w", err)
	}
	return os.Rename(tmp, m.statePath)
}

// Config holds watcher configuration.
type Config struct {
	Repo         string
	PollInterval time.Duration
	BaseDir      string // e.g. ~/.local/share/lurker/
}

// Watcher polls GitHub for new issues and orchestrates processing.
type Watcher struct {
	cfg     Config
	manager *Manager
}

func (w *Watcher) emit(ch chan<- Event, kind EventKind, issueNum int, text string) {
	ch <- Event{
		Kind:      kind,
		Repo:      w.cfg.Repo,
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

// poll discovers new issues and emits EventIssueFound. It does NOT start
// processing — the user triggers that via the TUI.
func (w *Watcher) poll(ctx context.Context, eventCh chan<- Event) {
	w.emit(eventCh, EventPollStart, 0, "Polling for new issues...")

	issues, err := FetchOpenIssues(w.cfg.Repo)
	if err != nil {
		w.emit(eventCh, EventError, 0, fmt.Sprintf("Poll failed: %v", err))
		return
	}

	var newCount int
	for _, iss := range issues {
		key := IssueKey(w.cfg.Repo, iss.Number)
		if w.manager != nil && w.manager.IsKnown(key) {
			continue
		}
		if w.manager != nil {
			w.manager.StoreIssue(w.cfg.Repo, iss)
		}
		eventCh <- Event{
			Kind:        EventIssueFound,
			Repo:        w.cfg.Repo,
			IssueNum:    iss.Number,
			Text:        iss.Title,
			Timestamp:   time.Now(),
			IssueURL:    iss.URL,
			IssueBody:   iss.Body,
			IssueLabels: iss.LabelNames(),
		}
		newCount++
	}

	w.emit(eventCh, EventPollDone, 0, fmt.Sprintf("Found %d new issues (of %d open)", newCount, len(issues)))
}

// processIssue does the actual work: react, clone, run claude.
// Called by Manager.StartIssue when the user triggers it.
func (w *Watcher) processIssue(ctx context.Context, eventCh chan<- Event, issue Issue) {
	num := issue.Number

	// React with eyes
	if err := AddReaction(w.cfg.Repo, num); err != nil {
		if ctx.Err() != nil {
			return
		}
		w.emit(eventCh, EventError, num, fmt.Sprintf("React failed: %v", err))
	} else {
		w.emit(eventCh, EventReacted, num, "Added 👀 reaction")
	}

	if ctx.Err() != nil {
		return
	}

	// Clone
	issueDir := filepath.Join(w.cfg.BaseDir, w.cfg.Repo, fmt.Sprintf("%d", num))
	repoName := filepath.Base(w.cfg.Repo)
	workdir := filepath.Join(issueDir, repoName)

	w.emit(eventCh, EventCloneStart, num, "Cloning repository...")

	if err := w.cloneRepo(ctx, issueDir, workdir, num); err != nil {
		if ctx.Err() != nil {
			return
		}
		w.emit(eventCh, EventError, num, fmt.Sprintf("Clone failed: %v", err))
		return
	}

	w.emit(eventCh, EventCloneDone, num, workdir)

	if ctx.Err() != nil {
		return
	}

	// Load per-repo config from .lurker/config.json if present
	repoCfg := LoadRepoConfig(workdir)

	// Run Claude
	w.emit(eventCh, EventClaudeStart, num, "Running Claude Code...")

	prompt := BuildClaudePrompt(w.cfg.Repo, issue)
	if repoCfg.PromptPrefix != "" {
		prompt = repoCfg.PromptPrefix + "\n\n" + prompt
	}
	logFn := func(line string) {
		w.emit(eventCh, EventClaudeLog, num, line)
	}

	_, err := RunClaude(ctx, workdir, prompt, repoCfg.ClaudeTools(), logFn)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		w.emit(eventCh, EventClaudeDone, num, fmt.Sprintf("Claude failed: %v", err))
		w.emit(eventCh, EventError, num, err.Error())
		return
	}

	w.emit(eventCh, EventClaudeDone, num, "Claude finished successfully")
	w.emit(eventCh, EventReady, num, workdir)
}

func (w *Watcher) cloneRepo(ctx context.Context, issueDir, workdir string, issueNum int) error {
	bareDir := filepath.Join(w.cfg.BaseDir, w.cfg.Repo, "bare.git")

	// Ensure bare clone exists
	if _, err := os.Stat(bareDir); err != nil {
		if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		cmd := exec.CommandContext(ctx, "gh", "repo", "clone", w.cfg.Repo, bareDir, "--", "--bare")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("bare clone: %s: %w", string(out), err)
		}
	} else {
		// Fetch latest
		cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
		cmd.Dir = bareDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %s: %w", string(out), err)
		}
	}

	// If worktree already exists, just fetch
	if _, err := os.Stat(workdir); err == nil {
		cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("worktree fetch: %s: %w", string(out), err)
		}
		return nil
	}

	// Create worktree with new branch
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	branch := fmt.Sprintf("agent/issue-%d", issueNum)
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, workdir)
	cmd.Dir = bareDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("worktree add: %s: %w", string(out), err)
	}

	return nil
}
