package leader

import (
	"context"
	"log/slog"

	"quick-ai-toolhub/internal/orchestrator"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/timeline"
	"quick-ai-toolhub/internal/worktreeprep"
)

type Service struct {
	logger       *slog.Logger
	store        *store.Service
	tasklist     TaskListTool
	worktreePrep WorktreePrepTool
	orchestrator *orchestrator.Service
	timeline     *timeline.Service
}

type Dependencies struct {
	Logger       *slog.Logger
	Store        *store.Service
	TaskList     TaskListTool
	WorktreePrep WorktreePrepTool
	Orchestrator *orchestrator.Service
	Timeline     *timeline.Service
}

type TaskListTool interface {
	Execute(context.Context, tasklist.Request) tasklist.Response
}

type WorktreePrepTool interface {
	Execute(context.Context, worktreeprep.Request, worktreeprep.ExecuteOptions) worktreeprep.Response
}

func New(deps Dependencies) *Service {
	return &Service{
		logger:       componentLogger(deps.Logger),
		store:        deps.Store,
		tasklist:     deps.TaskList,
		worktreePrep: deps.WorktreePrep,
		orchestrator: deps.Orchestrator,
		timeline:     deps.Timeline,
	}
}

func (s *Service) Name() string {
	return "leader"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "leader")
	}
	return logger.With("component", "leader")
}
