package agentrun

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"quick-ai-toolhub/internal/issuesync"
)

const defaultConfigFile = "config/config.yaml"

type agentSettings struct {
	DefaultModel string
	Profiles     map[AgentType]agentProfile
}

type agentProfile struct {
	Model        string
	TemplateFile string
	TemplateBody string
}

type rawAgentSettings struct {
	DefaultModel string                     `yaml:"default_model"`
	Agents       map[string]rawAgentProfile `yaml:"agents"`
}

type rawAgentProfile struct {
	Model        string `yaml:"model"`
	TemplateFile string `yaml:"template_file"`
}

type promptTemplateData struct {
	AgentType          AgentType
	Attempt            int
	Lens               string
	TaskID             string
	TaskTitle          string
	TaskGoal           string
	SprintID           string
	SprintTitle        string
	SprintGoal         string
	Reads              []string
	Dependencies       []string
	InScope            []string
	OutOfScope         []string
	Deliverables       []string
	AcceptanceCriteria []string
	Notes              []string
	ContextRefs        ContextRefs
}

func loadAgentSettings(workdir, configFile string) (agentSettings, error) {
	if strings.TrimSpace(configFile) == "" {
		configFile = defaultConfigFile
	}

	configPath := resolveAgainstWorkDir(workdir, configFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		defaultPath := resolveAgainstWorkDir(workdir, defaultConfigFile)
		if errors.Is(err, os.ErrNotExist) && filepath.Clean(configPath) == filepath.Clean(defaultPath) {
			return agentSettings{}, nil
		}
		return agentSettings{}, wrapToolError(ErrorCodeConfigLoadFailed, false, "load config: %v", err)
	}

	var raw rawAgentSettings
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return agentSettings{}, wrapToolError(ErrorCodeConfigLoadFailed, false, "decode config: %v", err)
	}

	settings := agentSettings{
		DefaultModel: strings.TrimSpace(raw.DefaultModel),
		Profiles:     make(map[AgentType]agentProfile, len(raw.Agents)),
	}
	for name, rawProfile := range raw.Agents {
		agentType := AgentType(strings.TrimSpace(name))
		switch agentType {
		case AgentDeveloper, AgentQA, AgentReviewer:
		default:
			return agentSettings{}, wrapToolError(ErrorCodeConfigLoadFailed, false, "unknown agent profile %q", name)
		}

		profile := agentProfile{
			Model:        strings.TrimSpace(rawProfile.Model),
			TemplateFile: strings.TrimSpace(rawProfile.TemplateFile),
		}
		if profile.TemplateFile != "" {
			templatePath := profile.TemplateFile
			if !filepath.IsAbs(templatePath) {
				templatePath = filepath.Join(workdir, profile.TemplateFile)
			}
			templateBytes, err := os.ReadFile(templatePath)
			if err != nil {
				return agentSettings{}, wrapToolError(ErrorCodeConfigLoadFailed, false, "load template %s: %v", profile.TemplateFile, err)
			}
			profile.TemplateBody = string(templateBytes)
		}
		settings.Profiles[agentType] = profile
	}

	return settings, nil
}

func (s agentSettings) defaultModelFor(agentType AgentType) string {
	if profile, ok := s.Profiles[agentType]; ok && profile.Model != "" {
		return profile.Model
	}
	return s.DefaultModel
}

func (s agentSettings) roleInstructions(agentType AgentType, task *issuesync.TaskBrief, sprint *issuesync.Sprint, attempt int, lens string, contextRefs ContextRefs, workdir string) (string, error) {
	profile, ok := s.Profiles[agentType]
	if !ok || strings.TrimSpace(profile.TemplateBody) == "" {
		return defaultRoleInstructions(agentType), nil
	}

	rendered, err := renderRoleTemplate(profile.TemplateBody, promptTemplateData{
		AgentType:          agentType,
		Attempt:            attempt,
		Lens:               lens,
		TaskID:             task.TaskID,
		TaskTitle:          task.Title,
		TaskGoal:           task.Goal,
		SprintID:           sprint.ID,
		SprintTitle:        sprint.Title,
		SprintGoal:         sprint.Goal,
		Reads:              buildReadList(task, workdir),
		Dependencies:       append([]string(nil), task.Dependencies...),
		InScope:            append([]string(nil), task.InScope...),
		OutOfScope:         append([]string(nil), task.OutOfScope...),
		Deliverables:       append([]string(nil), task.Deliverables...),
		AcceptanceCriteria: append([]string(nil), task.AcceptanceCriteria...),
		Notes:              append([]string(nil), task.Notes...),
		ContextRefs:        contextRefs,
	})
	if err != nil {
		return "", wrapToolError(ErrorCodePromptBuildFailed, false, "render role template for %s: %v", agentType, err)
	}
	return rendered, nil
}

func renderRoleTemplate(body string, data promptTemplateData) (string, error) {
	tmpl, err := template.New("role_prompt").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}
