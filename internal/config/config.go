package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultFile   = "config/config.yaml"
	ConfigFileEnv = "CONFIG_FILE"
)

type Config struct {
	Path         string         `yaml:"-"`
	Repo         RepoConfig     `yaml:"repo"`
	Database     DatabaseConfig `yaml:"database"`
	Server       ServerConfig   `yaml:"server"`
	DefaultModel string         `yaml:"default_model"`
	Agents       AgentsConfig   `yaml:"agents"`
}

type RepoConfig struct {
	GitHubOwner   string `yaml:"github_owner"`
	GitHubRepo    string `yaml:"github_repo"`
	DefaultBranch string `yaml:"default_branch"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr"`
}

type AgentsConfig struct {
	Developer AgentProfile `yaml:"developer"`
	QA        AgentProfile `yaml:"qa"`
	Reviewer  AgentProfile `yaml:"reviewer"`
}

type AgentProfile struct {
	Runner       string `yaml:"runner"`
	Model        string `yaml:"model"`
	TemplateFile string `yaml:"template_file"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "missing required fields: " + strings.Join(e.Problems, ", ")
}

func Load(workdir, configFile string) (Config, error) {
	root, err := filepath.Abs(strings.TrimSpace(workdir))
	if err != nil {
		return Config{}, fmt.Errorf("resolve workdir: %w", err)
	}

	path := effectiveConfigFile(configFile)
	resolvedPath, err := resolveConfigPath(root, path)
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", resolvedPath, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", resolvedPath, err)
	}

	cfg.normalize()
	cfg.Path = resolvedPath
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %s: %w", resolvedPath, err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string
	checkRequired(&problems, "repo.github_owner", c.Repo.GitHubOwner)
	checkRequired(&problems, "repo.github_repo", c.Repo.GitHubRepo)
	checkRequired(&problems, "repo.default_branch", c.Repo.DefaultBranch)
	checkRequired(&problems, "database.path", c.Database.Path)
	checkRequired(&problems, "server.listen_addr", c.Server.ListenAddr)
	checkRequired(&problems, "default_model", c.DefaultModel)
	checkRequired(&problems, "agents.developer.template_file", c.Agents.Developer.TemplateFile)
	checkRequired(&problems, "agents.qa.template_file", c.Agents.QA.TemplateFile)
	checkRequired(&problems, "agents.reviewer.template_file", c.Agents.Reviewer.TemplateFile)
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (c Config) DefaultModelFor(agentType string) string {
	profile, ok := c.AgentProfile(agentType)
	if ok && profile.Model != "" {
		return profile.Model
	}
	return c.DefaultModel
}

func (c Config) AgentProfile(agentType string) (AgentProfile, bool) {
	switch strings.TrimSpace(agentType) {
	case "developer":
		return c.Agents.Developer, true
	case "qa":
		return c.Agents.QA, true
	case "reviewer":
		return c.Agents.Reviewer, true
	default:
		return AgentProfile{}, false
	}
}

func (c *Config) normalize() {
	c.Repo.GitHubOwner = strings.TrimSpace(c.Repo.GitHubOwner)
	c.Repo.GitHubRepo = strings.TrimSpace(c.Repo.GitHubRepo)
	c.Repo.DefaultBranch = strings.TrimSpace(c.Repo.DefaultBranch)
	c.Database.Path = strings.TrimSpace(c.Database.Path)
	c.Server.ListenAddr = strings.TrimSpace(c.Server.ListenAddr)
	c.DefaultModel = strings.TrimSpace(c.DefaultModel)
	c.Agents.Developer.normalize()
	c.Agents.QA.normalize()
	c.Agents.Reviewer.normalize()
}

func (p *AgentProfile) normalize() {
	p.Runner = strings.TrimSpace(p.Runner)
	p.Model = strings.TrimSpace(p.Model)
	p.TemplateFile = strings.TrimSpace(p.TemplateFile)
}

func effectiveConfigFile(configFile string) string {
	if trimmed := strings.TrimSpace(configFile); trimmed != "" {
		return trimmed
	}
	if envPath := strings.TrimSpace(os.Getenv(ConfigFileEnv)); envPath != "" {
		return envPath
	}
	return DefaultFile
}

func resolveConfigPath(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	current := root
	for {
		candidate := filepath.Join(current, path)
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return "", fmt.Errorf("config path %s is a directory", candidate)
			}
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat config %s: %w", candidate, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Join(root, path), nil
		}
		current = parent
	}
}

func checkRequired(problems *[]string, field, value string) {
	if strings.TrimSpace(value) == "" {
		*problems = append(*problems, field)
	}
}
