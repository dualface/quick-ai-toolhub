package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesDefaultConfigPath(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, DefaultFile, validConfigYAML("default-owner"))

	cfg, err := Load(root, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	wantPath := filepath.Join(root, DefaultFile)
	if cfg.Path != wantPath {
		t.Fatalf("unexpected config path: got %s want %s", cfg.Path, wantPath)
	}
	if cfg.Repo.GitHubOwner != "default-owner" {
		t.Fatalf("unexpected github owner: %s", cfg.Repo.GitHubOwner)
	}
	if got := cfg.DefaultModelFor("developer"); got != "gpt-5.4" {
		t.Fatalf("unexpected developer default model: %s", got)
	}
	if got := cfg.DefaultModelFor("reviewer"); got != "gpt-5.3-codex-spark" {
		t.Fatalf("unexpected reviewer default model: %s", got)
	}
}

func TestLoadUsesConfigFileEnvOverride(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, DefaultFile, validConfigYAML("default-owner"))
	writeConfigFile(t, root, "testdata/override.yaml", validConfigYAML("override-owner"))
	t.Setenv(ConfigFileEnv, "testdata/override.yaml")

	cfg, err := Load(root, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Repo.GitHubOwner != "override-owner" {
		t.Fatalf("unexpected github owner: %s", cfg.Repo.GitHubOwner)
	}
}

func TestLoadReturnsValidationErrorForMissingRequiredFields(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, DefaultFile, strings.TrimSpace(`
repo:
  github_owner: acme

database: {}
server:
  listen_addr: ""

agents:
  developer:
    template_file: prompts/agents/developer.md
  qa: {}
  reviewer: {}
`)+"\n")

	_, err := Load(root, "")
	if err == nil {
		t.Fatal("expected validation error")
	}

	for _, needle := range []string{
		"repo.github_repo",
		"database.path",
		"default_model",
		"agents.qa.template_file",
		"agents.reviewer.template_file",
	} {
		if !strings.Contains(err.Error(), needle) {
			t.Fatalf("expected %q in error: %v", needle, err)
		}
	}
}

func writeConfigFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func validConfigYAML(owner string) string {
	return strings.TrimSpace(`
repo:
  github_owner: `+owner+`
  github_repo: quick-ai-toolhub
  default_branch: main

database:
  path: .toolhub/toolhub.db

server:
  listen_addr: 127.0.0.1:8080

default_model: gpt-5.3-codex-spark

agents:
  developer:
    model: gpt-5.4
    template_file: prompts/agents/developer.md
  qa:
    template_file: prompts/agents/qa.md
  reviewer:
    template_file: prompts/agents/reviewer.md
`) + "\n"
}
