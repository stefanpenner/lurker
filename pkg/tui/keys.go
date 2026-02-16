package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Escape  key.Binding
	OpenDir key.Binding
	Diff    key.Binding
	Lazygit key.Binding
	PR      key.Binding
	Commit  key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "focus logs"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back to list"),
	),
	OpenDir: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open dir"),
	),
	Diff: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "diff"),
	),
	Lazygit: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "lazygit"),
	),
	PR: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "open PR"),
	),
	Commit: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "commit msg"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

func helpLineList() string {
	return "j/k navigate  enter scroll logs  l lazygit  c commit  p PR  o open  d diff  q quit"
}

func helpLineLogs() string {
	return "j/k scroll  esc back to list  l lazygit  c commit  p PR  o open  d diff  q quit"
}
