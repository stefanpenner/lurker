package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CreatePRRequest contains the fields needed to create a pull request.
type CreatePRRequest struct {
	Repo  string // "owner/repo"
	Title string
	Body  string
	Head  string // branch name
	Base  string // target branch (e.g. "main")
}

// PullRequest is the response from creating a PR.
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// CreatePR creates a pull request on the given repo.
func (c *Client) CreatePR(ctx context.Context, pr CreatePRRequest) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls", apiBase, pr.Repo)

	payload := map[string]string{
		"title": pr.Title,
		"body":  pr.Body,
		"head":  pr.Head,
		"base":  pr.Base,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("github: marshaling PR request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("github: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github: create PR: %s: %s", resp.Status, string(body))
	}

	var result PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("github: decoding PR response: %w", err)
	}

	return &result, nil
}

// stringReader is a helper to create an io.Reader from a string.
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}
