package leader

import (
	"log/slog"

	"quick-ai-toolhub/internal/orchestrator"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/timeline"
)

type Service struct {
	logger       *slog.Logger
	store        *store.Service
	orchestrator *orchestrator.Service
	timeline     *timeline.Service
}

type Dependencies struct {
	Logger       *slog.Logger
	Store        *store.Service
	Orchestrator *orchestrator.Service
	Timeline     *timeline.Service
}

func New(deps Dependencies) *Service {
	return &Service{
		logger:       componentLogger(deps.Logger),
		store:        deps.Store,
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
