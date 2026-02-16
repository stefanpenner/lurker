package watcher

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Issue represents a GitHub issue.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []Label   `json:"labels"`
	URL       string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

type Label struct {
	Name string `json:"name"`
}

// LabelNames returns label names as a comma-separated string.
func (i Issue) LabelNames() string {
	if len(i.Labels) == 0 {
		return ""
	}
	s := ""
	for idx, l := range i.Labels {
		if idx > 0 {
			s += ", "
		}
		s += l.Name
	}
	return s
}

// FetchOpenIssues returns open issues for the given repo, excluding PRs.
func FetchOpenIssues(repo string) ([]Issue, error) {
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/issues", repo),
		"--paginate",
		"-q", `.[] | select(.pull_request == null)`,
		"--jq", ".",
	)

	// Simpler approach: fetch all, filter in Go
	cmd = exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/issues?state=open&per_page=100", repo),
	)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh api failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh api failed: %w", err)
	}

	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parsing issues: %w", err)
	}

	// Filter out pull requests
	filtered := make([]Issue, 0, len(issues))
	for _, iss := range issues {
		if iss.PullRequest == nil {
			filtered = append(filtered, iss)
		}
	}

	return filtered, nil
}

// AddReaction adds an eyes reaction to the given issue.
func AddReaction(repo string, number int) error {
	cmd := exec.Command("gh", "api",
		"--method", "POST",
		fmt.Sprintf("repos/%s/issues/%d/reactions", repo, number),
		"-f", "content=eyes",
		"--silent",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adding reaction: %s: %w", string(out), err)
	}
	return nil
}
