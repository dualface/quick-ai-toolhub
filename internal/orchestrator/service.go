package orchestrator

import (
	"log/slog"

	toolgit "quick-ai-toolhub/internal/git"
	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/timeline"
)

type Service struct {
	logger   *slog.Logger
	store    *store.Service
	github   *toolgithub.Client
	git      *toolgit.Client
	timeline *timeline.Service
}

type Dependencies struct {
	Logger   *slog.Logger
	Store    *store.Service
	GitHub   *toolgithub.Client
	Git      *toolgit.Client
	Timeline *timeline.Service
}

func New(deps Dependencies) *Service {
	return &Service{
		logger:   componentLogger(deps.Logger),
		store:    deps.Store,
		github:   deps.GitHub,
		git:      deps.Git,
		timeline: deps.Timeline,
	}
}

func (s *Service) Name() string {
	return "orchestrator"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "orchestrator")
	}
	return logger.With("component", "orchestrator")
}
