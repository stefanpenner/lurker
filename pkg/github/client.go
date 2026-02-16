package github

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client is a GitHub API client with rate limiting and retry logic.
// It is safe for concurrent use across multiple watchers.
type Client struct {
	httpClient *http.Client
	token      string
	limiter    *rateLimiter
}

// NewClient creates a Client, resolving the API token from GITHUB_TOKEN
// or falling back to `gh auth token`.
func NewClient() (*Client, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		out, err := exec.Command("gh", "auth", "token").Output()
		if err != nil {
			return nil, fmt.Errorf("github: no GITHUB_TOKEN and `gh auth token` failed: %w", err)
		}
		token = strings.TrimSpace(string(out))
	}
	if token == "" {
		return nil, fmt.Errorf("github: empty token")
	}

	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		limiter:    newRateLimiter(),
	}, nil
}

// newClientForTest creates a Client pointing at a custom HTTP client (for httptest).
func newClientForTest(httpClient *http.Client, token string) *Client {
	return &Client{
		httpClient: httpClient,
		token:      token,
		limiter:    newRateLimiter(),
	}
}

var apiBase = "https://api.github.com"

// setAPIBase overrides the API base URL (for testing).
func setAPIBase(url string) { apiBase = url }

// do executes an HTTP request with auth, rate limiting, and retry.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			// Exponential backoff for retries
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}

		// Wait for rate limiter before sending
		c.limiter.wait()

		resp, err = c.httpClient.Do(req)
		if err != nil {
			continue
		}

		c.limiter.update(resp.Header)

		if resp.StatusCode == 429 || (resp.StatusCode == 403 && isRateLimitError(resp)) {
			resp.Body.Close()
			c.limiter.handleRateLimit(resp.Header)
			continue
		}

		if resp.StatusCode >= 500 {
			resp.Body.Close()
			continue
		}

		// 2xx or 4xx (non-rate-limit) — return immediately
		return resp, nil
	}

	if err != nil {
		return nil, fmt.Errorf("github: request failed after retries: %w", err)
	}
	return resp, nil
}

func isRateLimitError(resp *http.Response) bool {
	return resp.Header.Get("X-RateLimit-Remaining") == "0"
}
