package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListOpenIssues(t *testing.T) {
	issues := []Issue{
		{Number: 1, Title: "Bug report", Labels: []Label{{Name: "bug"}}},
		{Number: 2, Title: "PR title", PullRequest: &struct{}{}},
		{Number: 3, Title: "Feature request"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "open" {
			t.Error("expected state=open")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(issues)
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	// Override apiBase for test
	origBase := apiBase
	defer func() { setAPIBase(origBase) }()
	setAPIBase(srv.URL)

	result, err := c.ListOpenIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 issues (PRs filtered), got %d", len(result))
	}
	if result[0].Number != 1 {
		t.Errorf("first issue number = %d, want 1", result[0].Number)
	}
	if result[1].Number != 3 {
		t.Errorf("second issue number = %d, want 3", result[1].Number)
	}
}

func TestListOpenIssues_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	origBase := apiBase
	defer func() { setAPIBase(origBase) }()
	setAPIBase(srv.URL)

	_, err := c.ListOpenIssues(context.Background(), "bad/repo")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestAddReaction(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClientForTest(srv.Client(), "tok")
	origBase := apiBase
	defer func() { setAPIBase(origBase) }()
	setAPIBase(srv.URL)

	err := c.AddReaction(context.Background(), "owner/repo", 42, "eyes")
	if err != nil {
		t.Fatalf("AddReaction: %v", err)
	}

	if gotPath != "/repos/owner/repo/issues/42/reactions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q", gotMethod)
	}
	if gotBody["content"] != "eyes" {
		t.Errorf("body content = %q, want 'eyes'", gotBody["content"])
	}
}
