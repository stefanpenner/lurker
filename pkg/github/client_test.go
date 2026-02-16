package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientForTest(t *testing.T) {
	c := newClientForTest(http.DefaultClient, "test-token")
	if c.token != "test-token" {
		t.Errorf("expected token 'test-token', got %q", c.token)
	}
	if c.limiter == nil {
		t.Error("expected limiter to be initialized")
	}
}

func TestDo_SetsAuthHeaders(t *testing.T) {
	var gotAuth, gotAccept, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "my-token")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test", nil)
	resp, err := c.do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	if gotAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want 'Bearer my-token'", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotVersion != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", gotVersion)
	}
}

func TestDo_Retries5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_Returns4xxImmediately(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("should not retry 4xx, but got %d attempts", attempts)
	}
}
