package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// claudeTools defines the scoped tools Claude is allowed to use.
var claudeTools = strings.Join([]string{
	"Read",
	"Glob",
	"Grep",
	"Edit",
	"Write",
	`Bash(bazel test:*)`,
	`Bash(bazel build:*)`,
	`Bash(git add:*)`,
	`Bash(git commit:*)`,
	`Bash(git diff:*)`,
	`Bash(git status:*)`,
	`Bash(git log:*)`,
}, ",")

// BuildClaudePrompt creates the prompt for Claude Code given a repo and issue.
func BuildClaudePrompt(repo string, issue Issue) string {
	return fmt.Sprintf(`You are working on the %s project.

## Task
Implement a fix or feature for GitHub issue #%d.

**Title**: %s
**Labels**: %s
**Body**:
%s

## Instructions
1. Read any AGENTS.md, CLAUDE.md, README.md, or Architecture.md to understand the project.
2. Explore relevant source files.
3. Implement changes following existing conventions.
4. Run tests if a test framework is configured.
5. Add tests if appropriate.
6. Commit with message "Fix #%d: <description>". Do NOT push.

If the issue is unclear or too large, commit a PLAN.md describing your
analysis, proposed approach, and open questions.`,
		repo, issue.Number, issue.Title, issue.LabelNames(), issue.Body, issue.Number)
}

// Stream-json event types from claude --output-format stream-json.
// The actual format has:
//   {"type":"system","subtype":"init",...}
//   {"type":"assistant","message":{"content":[{"type":"text","text":"..."},{"type":"tool_use","name":"Read","input":{...}}]}}
//   {"type":"result","total_cost_usd":0.03,"duration_ms":15000,"num_turns":5,...}

type streamEvent struct {
	Type    string `json:"type"`
	SubType string `json:"subtype,omitempty"`
	Error   string `json:"error,omitempty"`

	// For assistant events
	Message *assistantMessage `json:"message,omitempty"`

	// For result events
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	DurationMS   float64 `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	Result       string  `json:"result,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// LogFunc is called with each line of Claude's output.
type LogFunc func(line string)

// formatStreamEvent turns a stream-json event into human-readable log lines.
// Returns nil if the event should be suppressed.
func formatStreamEvent(raw string) []string {
	var ev streamEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return nil
	}

	switch ev.Type {
	case "system":
		if ev.SubType == "init" {
			return []string{"Claude session initialized"}
		}
		return nil

	case "assistant":
		if ev.Error != "" {
			return []string{fmt.Sprintf("⚠ Error: %s", ev.Error)}
		}
		if ev.Message == nil {
			return nil
		}
		var lines []string
		for _, block := range ev.Message.Content {
			switch block.Type {
			case "text":
				text := strings.TrimSpace(block.Text)
				if text == "" {
					continue
				}
				// Show first ~200 chars of text output
				if len(text) > 200 {
					text = text[:200] + "…"
				}
				// Split multi-line text into separate log lines
				for _, line := range strings.Split(text, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						lines = append(lines, line)
					}
				}
			case "tool_use":
				if line := formatToolUse(block); line != "" {
					lines = append(lines, line)
				}
			}
		}
		return lines

	case "result":
		cost := ""
		if ev.TotalCostUSD > 0 {
			cost = fmt.Sprintf(" ($%.4f)", ev.TotalCostUSD)
		}
		dur := ""
		if ev.DurationMS > 0 {
			secs := ev.DurationMS / 1000
			if secs >= 60 {
				dur = fmt.Sprintf(" %.0fm%.0fs", secs/60, float64(int(secs)%60))
			} else {
				dur = fmt.Sprintf(" %.1fs", secs)
			}
		}
		turns := ""
		if ev.NumTurns > 0 {
			turns = fmt.Sprintf(" %d turns", ev.NumTurns)
		}
		if ev.IsError {
			msg := ev.Result
			if len(msg) > 100 {
				msg = msg[:100] + "…"
			}
			return []string{fmt.Sprintf("✗ Failed:%s%s — %s", dur, cost, msg)}
		}
		return []string{fmt.Sprintf("✓ Done%s%s%s", dur, turns, cost)}
	}

	return nil
}

func formatToolUse(block contentBlock) string {
	tool := block.Name
	if tool == "" {
		return ""
	}

	var input struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
	}
	if block.Input != nil {
		json.Unmarshal(block.Input, &input)
	}

	switch tool {
	case "Read":
		return fmt.Sprintf("📖 Read %s", input.FilePath)
	case "Write":
		return fmt.Sprintf("📝 Write %s", input.FilePath)
	case "Edit":
		return fmt.Sprintf("✏️  Edit %s", input.FilePath)
	case "Glob":
		return fmt.Sprintf("🔍 Glob %s", input.Pattern)
	case "Grep":
		p := input.Pattern
		if len(p) > 50 {
			p = p[:50] + "…"
		}
		return fmt.Sprintf("🔍 Grep %q", p)
	case "Bash":
		cmd := input.Command
		if len(cmd) > 80 {
			cmd = cmd[:80] + "…"
		}
		return fmt.Sprintf("$ %s", cmd)
	case "Task":
		return "🤖 Spawning sub-agent"
	default:
		return fmt.Sprintf("🔧 %s", tool)
	}
}

// RunClaude invokes Claude Code in the given workdir with the given prompt.
// It streams output line-by-line via logFn. The tools parameter specifies
// the allowed tools string; pass claudeTools for the default set.
// Returns the full output on completion.
func RunClaude(ctx context.Context, workdir string, prompt string, tools string, logFn LogFunc) (string, error) {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", tools,
	)
	cmd.Dir = workdir

	// Strip ANTHROPIC_API_KEY so claude -p uses OAuth/Max subscription
	// instead of API credits
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") &&
			!strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered

	// Pass prompt via stdin
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting claude: %w", err)
	}

	// Write prompt and close stdin
	go func() {
		defer stdinPipe.Close()
		stdinPipe.Write([]byte(prompt))
	}()

	var output strings.Builder

	// Read stderr in a goroutine
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if logFn != nil {
				logFn("[stderr] " + line)
			}
		}
	}()

	// Parse stream-json events from stdout
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		output.WriteString(raw)
		output.WriteString("\n")

		if logFn != nil {
			lines := formatStreamEvent(raw)
			for _, line := range lines {
				logFn(line)
			}
		}
	}

	<-doneCh

	if err := cmd.Wait(); err != nil {
		return output.String(), fmt.Errorf("claude exited: %w", err)
	}

	return output.String(), nil
}
