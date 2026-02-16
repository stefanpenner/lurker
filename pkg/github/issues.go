package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Issue represents a GitHub issue (subset of fields).
type Issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Labels      []Label   `json:"labels"`
	URL         string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// Label represents a GitHub issue label.
type Label struct {
	Name string `json:"name"`
}

// ListOpenIssues returns open issues for the given "owner/repo", excluding PRs.
func (c *Client) ListOpenIssues(ctx context.Context, repo string) ([]Issue, error) {
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=100", apiBase, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: creating request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github: list issues: %s: %s", resp.Status, string(body))
	}

	var issues []Issue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, fmt.Errorf("github: decoding issues: %w", err)
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

// AddReaction adds a reaction to an issue.
func (c *Client) AddReaction(ctx context.Context, repo string, number int, reaction string) error {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/reactions", apiBase, repo, number)

	body := fmt.Sprintf(`{"content":%q}`, reaction)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, stringReader(body))
	if err != nil {
		return fmt.Errorf("github: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 200 = already existed, 201 = created — both are fine
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: add reaction: %s: %s", resp.Status, string(respBody))
	}

	return nil
}
