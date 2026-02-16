package watcher

import (
	"bufio"
	"context"
	"fmt"
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

// LogFunc is called with each line of Claude's output.
type LogFunc func(line string)

// RunClaude invokes Claude Code in the given workdir with the given prompt.
// It streams output line-by-line via logFn. Returns the full output on completion.
func RunClaude(ctx context.Context, workdir string, prompt string, logFn LogFunc) (string, error) {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--allowedTools", claudeTools,
	)
	cmd.Dir = workdir
	cmd.Env = append(cmd.Environ(), "CLAUDE_CODE_ENTRYPOINT=cli")

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

	// Read stdout in a goroutine
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

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteString("\n")
		if logFn != nil {
			logFn(line)
		}
	}

	<-doneCh

	if err := cmd.Wait(); err != nil {
		return output.String(), fmt.Errorf("claude exited: %w", err)
	}

	return output.String(), nil
}
