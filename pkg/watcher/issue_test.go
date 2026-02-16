package watcher

import (
	"testing"
)

func TestLabelNames(t *testing.T) {
	tests := []struct {
		name   string
		labels []Label
		want   string
	}{
		{"empty", nil, ""},
		{"one", []Label{{Name: "bug"}}, "bug"},
		{"multiple", []Label{{Name: "bug"}, {Name: "urgent"}, {Name: "help wanted"}}, "bug, urgent, help wanted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := Issue{Labels: tt.labels}
			got := issue.LabelNames()
			if got != tt.want {
				t.Errorf("LabelNames() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIssueKey(t *testing.T) {
	tests := []struct {
		repo string
		num  int
		want string
	}{
		{"owner/repo", 42, "owner/repo#42"},
		{"org/project", 1, "org/project#1"},
		{"a/b", 999, "a/b#999"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := IssueKey(tt.repo, tt.num)
			if got != tt.want {
				t.Errorf("IssueKey(%q, %d) = %q, want %q", tt.repo, tt.num, got, tt.want)
			}
		})
	}
}

func TestIssueStatusString(t *testing.T) {
	tests := []struct {
		status IssueStatus
		want   string
	}{
		{StatusPending, "pending"},
		{StatusReacted, "reacted"},
		{StatusCloning, "cloning"},
		{StatusCloneReady, "cloned"},
		{StatusClaudeRunning, "claude"},
		{StatusReady, "ready"},
		{StatusFailed, "failed"},
		{StatusPaused, "paused"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("IssueStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
