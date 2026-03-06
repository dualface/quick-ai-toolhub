package agentrun

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	sharedconfig "quick-ai-toolhub/internal/config"
	"quick-ai-toolhub/internal/issuesync"
)

type agentSettings struct {
	DefaultModel string
	Profiles     map[AgentType]agentProfile
}

type agentProfile struct {
	Model        string
	TemplateFile string
	TemplateBody string
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
	cfg, err := sharedconfig.Load(workdir, configFile)
	if err != nil {
		return agentSettings{}, wrapToolError(ErrorCodeConfigLoadFailed, false, "%v", err)
	}

	settings := agentSettings{
		DefaultModel: cfg.DefaultModel,
		Profiles: map[AgentType]agentProfile{
			AgentDeveloper: {
				Model:        cfg.Agents.Developer.Model,
				TemplateFile: cfg.Agents.Developer.TemplateFile,
			},
			AgentQA: {
				Model:        cfg.Agents.QA.Model,
				TemplateFile: cfg.Agents.QA.TemplateFile,
			},
			AgentReviewer: {
				Model:        cfg.Agents.Reviewer.Model,
				TemplateFile: cfg.Agents.Reviewer.TemplateFile,
			},
		},
	}
	for agentType, profile := range settings.Profiles {
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
