package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePR(t *testing.T) {
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(PullRequest{
			Number:  99,
			HTMLURL: "https://github.com/owner/repo/pull/99",
		})
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	origBase := apiBase
	defer func() { setAPIBase(origBase) }()
	setAPIBase(srv.URL)

	pr, err := c.CreatePR(context.Background(), CreatePRRequest{
		Repo:  "owner/repo",
		Title: "Fix #1: bug fix",
		Body:  "Fixes #1",
		Head:  "agent/issue-1",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}

	if pr.Number != 99 {
		t.Errorf("PR number = %d, want 99", pr.Number)
	}
	if pr.HTMLURL != "https://github.com/owner/repo/pull/99" {
		t.Errorf("PR URL = %q", pr.HTMLURL)
	}
	if gotBody["title"] != "Fix #1: bug fix" {
		t.Errorf("body title = %q", gotBody["title"])
	}
	if gotBody["head"] != "agent/issue-1" {
		t.Errorf("body head = %q", gotBody["head"])
	}
	if gotBody["base"] != "main" {
		t.Errorf("body base = %q", gotBody["base"])
	}
}

func TestCreatePR_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	origBase := apiBase
	defer func() { setAPIBase(origBase) }()
	setAPIBase(srv.URL)

	_, err := c.CreatePR(context.Background(), CreatePRRequest{
		Repo:  "owner/repo",
		Title: "test",
		Head:  "branch",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error for 422 response")
	}
}
