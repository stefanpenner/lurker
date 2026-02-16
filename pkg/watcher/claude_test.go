package watcher

import (
	"strings"
	"testing"
)

func TestBuildClaudePrompt(t *testing.T) {
	issue := Issue{
		Number: 42,
		Title:  "Fix login bug",
		Body:   "The login page crashes when...",
		Labels: []Label{{Name: "bug"}, {Name: "urgent"}},
	}

	prompt := BuildClaudePrompt("owner/repo", issue)

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	// Check it contains the issue details
	if !strings.Contains(prompt, "#42") {
		t.Error("prompt should contain issue number")
	}
	if !strings.Contains(prompt, "Fix login bug") {
		t.Error("prompt should contain issue title")
	}
	if !strings.Contains(prompt, "The login page crashes") {
		t.Error("prompt should contain issue body")
	}
	if !strings.Contains(prompt, "bug, urgent") {
		t.Error("prompt should contain labels")
	}
	if !strings.Contains(prompt, "owner/repo") {
		t.Error("prompt should contain repo name")
	}
}

func TestFormatStreamEvent_System(t *testing.T) {
	lines := formatStreamEvent(`{"type":"system","subtype":"init"}`)
	if len(lines) != 1 || lines[0] != "Claude session initialized" {
		t.Errorf("expected init message, got %v", lines)
	}
}

func TestFormatStreamEvent_SystemOther(t *testing.T) {
	lines := formatStreamEvent(`{"type":"system","subtype":"other"}`)
	if lines != nil {
		t.Errorf("expected nil for non-init system event, got %v", lines)
	}
}

func TestFormatStreamEvent_AssistantText(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`
	lines := formatStreamEvent(raw)
	if len(lines) != 1 || lines[0] != "Hello world" {
		t.Errorf("expected 'Hello world', got %v", lines)
	}
}

func TestFormatStreamEvent_AssistantToolUse(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/foo/bar.go"}}]}}`
	lines := formatStreamEvent(raw)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Read") || !strings.Contains(lines[0], "/foo/bar.go") {
		t.Errorf("expected Read tool use line, got %q", lines[0])
	}
}

func TestFormatStreamEvent_Result(t *testing.T) {
	raw := `{"type":"result","total_cost_usd":0.05,"duration_ms":30000,"num_turns":3}`
	lines := formatStreamEvent(raw)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Done") {
		t.Errorf("expected 'Done' in result, got %q", lines[0])
	}
}

func TestFormatStreamEvent_ResultError(t *testing.T) {
	raw := `{"type":"result","is_error":true,"result":"something broke","duration_ms":5000}`
	lines := formatStreamEvent(raw)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Failed") {
		t.Errorf("expected 'Failed' in error result, got %q", lines[0])
	}
}

func TestFormatStreamEvent_InvalidJSON(t *testing.T) {
	lines := formatStreamEvent("not json")
	if lines != nil {
		t.Errorf("expected nil for invalid JSON, got %v", lines)
	}
}

func TestFormatToolUse(t *testing.T) {
	tests := []struct {
		name     string
		block    contentBlock
		contains string
	}{
		{"Read", contentBlock{Name: "Read", Input: []byte(`{"file_path":"/a/b.go"}`)}, "Read"},
		{"Write", contentBlock{Name: "Write", Input: []byte(`{"file_path":"/a/b.go"}`)}, "Write"},
		{"Edit", contentBlock{Name: "Edit", Input: []byte(`{"file_path":"/a/b.go"}`)}, "Edit"},
		{"Glob", contentBlock{Name: "Glob", Input: []byte(`{"pattern":"**/*.go"}`)}, "Glob"},
		{"Grep", contentBlock{Name: "Grep", Input: []byte(`{"pattern":"TODO"}`)}, "Grep"},
		{"Bash", contentBlock{Name: "Bash", Input: []byte(`{"command":"git status"}`)}, "git status"},
		{"Task", contentBlock{Name: "Task"}, "sub-agent"},
		{"Unknown", contentBlock{Name: "CustomTool"}, "CustomTool"},
		{"Empty", contentBlock{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolUse(tt.block)
			if tt.contains == "" {
				if result != "" {
					t.Errorf("expected empty, got %q", result)
				}
			} else {
				if !strings.Contains(result, tt.contains) {
					t.Errorf("expected %q to contain %q", result, tt.contains)
				}
			}
		})
	}
}
