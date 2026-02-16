package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// RepoConfig holds per-repo configuration for lurker.
// Loaded from .lurker/config.json in the target repository.
type RepoConfig struct {
	// PromptPrefix is prepended to the Claude prompt (e.g., project-specific context)
	PromptPrefix string `json:"prompt_prefix,omitempty"`

	// AllowedTools overrides the default Claude tool permissions
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// BuildCommand is the command to run for building (default: "bazel build //...")
	BuildCommand string `json:"build_command,omitempty"`

	// TestCommand is the command to run for testing (default: "bazel test //...")
	TestCommand string `json:"test_command,omitempty"`
}

// LoadRepoConfig reads .lurker/config.json from the given workdir.
// Returns a zero-value RepoConfig if the file doesn't exist.
func LoadRepoConfig(workdir string) RepoConfig {
	p := filepath.Join(workdir, ".lurker", "config.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return RepoConfig{}
	}
	var cfg RepoConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

// ClaudeTools returns the tool permissions string, using overrides if configured.
func (c RepoConfig) ClaudeTools() string {
	if len(c.AllowedTools) > 0 {
		return strings.Join(c.AllowedTools, ",")
	}
	return claudeTools
}
