package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanpenner/lurker/pkg/tui"
	"github.com/stefanpenner/lurker/pkg/watcher"
)

func main() {
	repo := flag.String("repo", "stefanpenner/chirp", "GitHub repo to watch (owner/name)")
	interval := flag.Duration("interval", 30*time.Second, "Poll interval")
	baseDir := flag.String("dir", "", "Base directory for workdirs (default: ~/.local/share/issue-watcher)")
	flag.Parse()

	if *baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		*baseDir = filepath.Join(home, ".local", "share", "lurker")
	}

	cfg := watcher.Config{
		Repo:         *repo,
		PollInterval: *interval,
		BaseDir:      *baseDir,
	}

	w, err := watcher.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating watcher: %v\n", err)
		os.Exit(1)
	}

	eventCh := make(chan watcher.Event, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher in background
	go w.Run(ctx, eventCh)

	// Start TUI
	model := tui.NewModel(*repo, eventCh)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cancel()
}
