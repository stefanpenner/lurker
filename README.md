# Lurker

**An autonomous GitHub issue watcher powered by Claude Code.**
Add repos, lurk for new issues, dispatch Claude to fix them — review and merge from your terminal. No babysitting required.

```
     ╭─────╮
     │ ◉ ◉ │
     │  ◡  │
     ╰──┬──╯
        │
```

<img width="1242" height="740" alt="image" src="https://github.com/user-attachments/assets/e842f10b-bca7-45ed-b82e-aa2427c41eb9" />

> **Warning** — Lurker is highly experimental, unstable, and unsafe. It runs AI agents that modify code, execute shell commands, and create pull requests on your behalf. Use at your own risk, in sandboxed environments, on repos you control. Do not point this at production repositories without understanding the consequences. There are no security guarantees. See [sec-ideas.md](sec-ideas.md) for the threat model.

## Install

### Build from source

```
brew install bazelisk
bazel run //:lurker
```

### Go install

```
go install github.com/stefanpenner/lurker/cmd/lurker@latest
lurker
```

## Features

- **Watch repos** — Poll GitHub for new issues on any repo you add
- **Auto-dispatch** — Start Claude Code on an issue with a single keypress
- **Live progress** — Stream Claude's tool use and thinking in real time
- **Review workflow** — Inspect changes, launch lazygit, open Claude interactively
- **One-click PRs** — Push the branch and open a PR from the TUI
- **Persistent state** — Remembers repos and processed issues across sessions
- **Focus mode** — Full-screen view for deep-diving into a single issue

## How it works

1. You add a GitHub repo (`r` to add, `owner/repo` format)
2. Lurker polls for open issues every 30 seconds
3. New issues appear in the tree — select one and press `Space` to start
4. Lurker reacts with eyes, clones the repo, creates an `agent/issue-N` branch
5. Claude Code analyzes the issue and implements a fix
6. When done, review the changes and press `a` to push & create a PR

## Keybindings

| Key | Action |
|-----|--------|
| `j`/`k` | Navigate up/down |
| `Enter`/`l` | Expand/collapse |
| `Space` | Start/pause processing |
| `f` | Focus view (full-screen) |
| `r` | Add repo |
| `R`/`d` | Remove repo |
| `g` | Launch lazygit |
| `c` | Launch Claude Code |
| `o` | Open in browser |
| `i` | Info dialog |
| `a` | Approve & create PR |
| `q` | Quit |

## Setup

### Prerequisites

- **Go 1.24+** or **Bazel** (via Bazelisk)
- **GitHub CLI** (`gh`) — authenticated with `gh auth login`
- **Claude Code** (`claude`) — authenticated via OAuth
- **lazygit** (optional) — for the `g` keybinding

### Configuration

Lurker stores state and workdirs in `~/.local/share/lurker/`. Override with `--dir`:

```
lurker --dir /tmp/lurker-sandbox --interval 60s
```

## Security

See [sec-ideas.md](sec-ideas.md) for the full threat model and planned mitigations.

**Current posture**: Lurker processes any open issue on repos you add when you press Space. There is no automatic gating — any issue author can influence what Claude does once you start processing. Planned mitigations include thumbs-up gating and author allowlists.

## Development

```
brew install bazelisk
bazel build //...                     # build
bazel run //:lurker                   # build and run
bazel test //...                      # run tests
bazel run //:gazelle                  # regenerate BUILD files
```

## Acknowledgments

- [Bubbletea](https://github.com/charmbracelet/bubbletea) — terminal UI framework (MIT)
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — terminal styling (MIT)
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components (MIT)
- [Claude Code](https://claude.ai/claude-code) — AI coding agent
- [GitHub CLI](https://cli.github.com/) — GitHub API access
