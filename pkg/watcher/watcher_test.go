package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadState_NoFile(t *testing.T) {
	state := loadState("/nonexistent/path/state.json")
	if len(state.Repos) != 0 {
		t.Errorf("expected empty repos, got %v", state.Repos)
	}
	if state.Processed == nil {
		t.Error("expected initialized Processed map")
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	state := loadState(path)
	if len(state.Repos) != 0 {
		t.Errorf("expected empty repos for invalid JSON, got %v", state.Repos)
	}
}

func TestLoadState_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte(`{"repos":["owner/repo"],"processed":{"owner/repo":[1,2,3]}}`), 0o644)

	state := loadState(path)
	if len(state.Repos) != 1 || state.Repos[0] != "owner/repo" {
		t.Errorf("expected [owner/repo], got %v", state.Repos)
	}
	if len(state.Processed["owner/repo"]) != 3 {
		t.Errorf("expected 3 processed issues, got %v", state.Processed["owner/repo"])
	}
}

func TestManager_AddRemoveRepo(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	// Add a repo (will fail to actually poll, but state should be saved)
	if err := mgr.AddRepo("test/repo"); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	repos := mgr.Repos()
	if len(repos) != 1 || repos[0] != "test/repo" {
		t.Errorf("expected [test/repo], got %v", repos)
	}

	// Verify state was persisted
	state := loadState(filepath.Join(dir, "state.json"))
	if len(state.Repos) != 1 {
		t.Errorf("expected persisted repo, got %v", state.Repos)
	}

	// Remove
	if err := mgr.RemoveRepo("test/repo"); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	repos = mgr.Repos()
	if len(repos) != 0 {
		t.Errorf("expected empty repos after remove, got %v", repos)
	}
}

func TestManager_IsProcessed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	if mgr.IsProcessed("test/repo", 42) {
		t.Error("issue should not be processed initially")
	}

	mgr.MarkProcessed("test/repo", 42)

	if !mgr.IsProcessed("test/repo", 42) {
		t.Error("issue should be processed after marking")
	}
}

func TestManager_StoreAndKnow(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	key := IssueKey("test/repo", 1)
	if mgr.IsKnown(key) {
		t.Error("issue should not be known initially")
	}

	mgr.StoreIssue("test/repo", Issue{Number: 1, Title: "test"})

	if !mgr.IsKnown(key) {
		t.Error("issue should be known after storing")
	}
}
