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

// BuildClaudePrompt creates the prompt for Claude Code given an issue.
func BuildClaudePrompt(issue Issue) string {
	return fmt.Sprintf(`You are working on the Chirp project (stefanpenner/chirp), a macOS menu bar
app for offline speech-to-text.

## Task
Implement a fix or feature for GitHub issue #%d.

**Title**: %s
**Labels**: %s
**Body**:
%s

## Instructions
1. Read AGENTS.md and Architecture.md to understand the project.
2. Explore relevant source files.
3. Implement changes following existing conventions (Swift 6, @Observable, actors).
4. Run `+"`bazel test //...`"+` to verify.
5. Add tests if appropriate (Swift Testing framework).
6. Run `+"`bazel build //...`"+` to verify build.
7. Commit with message "Fix #%d: <description>". Do NOT push.

If the issue is unclear or too large, commit a PLAN.md describing your
analysis, proposed approach, and open questions.`,
		issue.Number, issue.Title, issue.LabelNames(), issue.Body, issue.Number)
}

// streamEvent represents a single JSON event from claude --output-format stream-json.
type streamEvent struct {
	Type       string          `json:"type"`
	SubType    string          `json:"subtype,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Content    string          `json:"content,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	CostUSD    float64         `json:"cost_usd,omitempty"`
	DurationMS float64         `json:"duration_ms,omitempty"`
	// For assistant messages with content blocks
	Message struct {
		Content []contentBlock `json:"content,omitempty"`
	} `json:"message,omitempty"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Name  string `json:"name,omitempty"`
	Input struct {
		Command  string `json:"command,omitempty"`
		FilePath string `json:"file_path,omitempty"`
		Pattern  string `json:"pattern,omitempty"`
		Query    string `json:"query,omitempty"`
	} `json:"input,omitempty"`
}

// LogFunc is called with each line of Claude's output.
type LogFunc func(line string)

// formatStreamEvent turns a stream-json event into a human-readable log line.
// Returns empty string if the event should be suppressed.
func formatStreamEvent(raw string) string {
	var ev streamEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return ""
	}

	switch ev.Type {
	case "assistant":
		// Text output from Claude
		if ev.SubType == "text" && ev.Content != "" {
			// Truncate long text to keep the log readable
			text := ev.Content
			if len(text) > 200 {
				text = text[:200] + "…"
			}
			return text
		}
		// Tool use
		if ev.SubType == "tool_use" {
			return formatToolUse(ev)
		}

	case "tool_result", "result":
		// Tool results can be very verbose, just note completion
		if ev.SubType == "tool_result" {
			return ""
		}
		if ev.Type == "result" {
			cost := ""
			if ev.CostUSD > 0 {
				cost = fmt.Sprintf(" ($%.4f)", ev.CostUSD)
			}
			dur := ""
			if ev.DurationMS > 0 {
				secs := ev.DurationMS / 1000
				if secs >= 60 {
					dur = fmt.Sprintf(" (%.0fm%.0fs)", secs/60, float64(int(secs)%60))
				} else {
					dur = fmt.Sprintf(" (%.1fs)", secs)
				}
			}
			return fmt.Sprintf("✓ Done%s%s", dur, cost)
		}

	case "system":
		if ev.SubType == "init" {
			return "Claude session initialized"
		}
		return ""
	}

	return ""
}

func formatToolUse(ev streamEvent) string {
	tool := ev.Tool
	if tool == "" {
		// Try to parse from content
		return ""
	}

	// Parse the input to get useful details
	var input struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
		OldStr   string `json:"old_string"`
		Content  string `json:"content"`
	}
	if ev.Input != nil {
		json.Unmarshal(ev.Input, &input)
	}

	switch tool {
	case "Read":
		return fmt.Sprintf("📖 Read %s", input.FilePath)
	case "Write":
		return fmt.Sprintf("📝 Write %s", input.FilePath)
	case "Edit":
		path := input.FilePath
		return fmt.Sprintf("✏️  Edit %s", path)
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
	default:
		return fmt.Sprintf("🔧 %s", tool)
	}
}

// RunClaude invokes Claude Code in the given workdir with the given prompt.
// It streams output line-by-line via logFn. Returns the full output on completion.
func RunClaude(ctx context.Context, workdir string, prompt string, logFn LogFunc) (string, error) {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--output-format", "stream-json",
		"--allowedTools", claudeTools,
	)
	cmd.Dir = workdir

	// Strip ANTHROPIC_API_KEY so claude -p uses OAuth/Max subscription
	// instead of API credits
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
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
			if line := formatStreamEvent(raw); line != "" {
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
