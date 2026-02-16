package main

import (
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
	interval := flag.Duration("interval", 30*time.Second, "Poll interval")
	baseDir := flag.String("dir", "", "Base directory for workdirs (default: ~/.local/share/lurker)")
	flag.Parse()

	if *baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		*baseDir = filepath.Join(home, ".local", "share", "lurker")
	}

	mgr, err := watcher.NewManager(*baseDir, *interval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating manager: %v\n", err)
		os.Exit(1)
	}

	mgr.Start()
	defer mgr.Stop()

	model := tui.NewModel(mgr)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
