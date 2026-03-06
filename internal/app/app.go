package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	sharedconfig "quick-ai-toolhub/internal/config"
	toolgit "quick-ai-toolhub/internal/git"
	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/leader"
	"quick-ai-toolhub/internal/orchestrator"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/timeline"
)

type Application struct {
	config       *sharedconfig.Config
	logger       *slog.Logger
	store        *store.Service
	github       *toolgithub.Client
	git          *toolgit.Client
	timeline     *timeline.Service
	orchestrator *orchestrator.Service
	leader       *leader.Service
}

type Options struct {
	Logger *slog.Logger
	Config *sharedconfig.Config
}

func New(opts Options) *Application {
	logger := opts.Logger
	if logger == nil {
		logger = NewLogger(io.Discard)
	}

	storeService := store.New(store.Dependencies{Logger: logger})
	githubClient := toolgithub.New(toolgithub.Dependencies{Logger: logger})
	gitClient := toolgit.New(toolgit.Dependencies{Logger: logger})
	timelineService := timeline.New(timeline.Dependencies{Logger: logger})
	orchestratorService := orchestrator.New(orchestrator.Dependencies{
		Logger:   logger,
		Store:    storeService,
		GitHub:   githubClient,
		Git:      gitClient,
		Timeline: timelineService,
	})
	leaderService := leader.New(leader.Dependencies{
		Logger:       logger,
		Store:        storeService,
		Orchestrator: orchestratorService,
		Timeline:     timelineService,
	})

	return &Application{
		config:       opts.Config,
		logger:       logger,
		store:        storeService,
		github:       githubClient,
		git:          gitClient,
		timeline:     timelineService,
		orchestrator: orchestratorService,
		leader:       leaderService,
	}
}

func NewLogger(w io.Writer) *slog.Logger {
	if w == nil {
		w = io.Discard
	}
	return slog.New(slog.NewJSONHandler(w, nil))
}

func (a *Application) Bootstrap(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.config == nil {
		return errors.New("nil config")
	}

	if err := a.store.Open(ctx, store.OpenOptions{
		ConfigPath:   a.config.Path,
		DatabasePath: a.config.Database.Path,
	}); err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	a.logger.Info("toolhub bootstrapped", slog.Any("components", a.ComponentNames()))
	return nil
}

func (a *Application) ComponentNames() []string {
	return []string{
		a.store.Name(),
		a.github.Name(),
		a.git.Name(),
		a.timeline.Name(),
		a.orchestrator.Name(),
		a.leader.Name(),
	}
}
