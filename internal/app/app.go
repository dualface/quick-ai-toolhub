package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"quick-ai-toolhub/internal/agentrun"
	sharedconfig "quick-ai-toolhub/internal/config"
	toolgit "quick-ai-toolhub/internal/git"
	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/githubsync"
	"quick-ai-toolhub/internal/leader"
	"quick-ai-toolhub/internal/logging"
	"quick-ai-toolhub/internal/orchestrator"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/timeline"
	"quick-ai-toolhub/internal/worktreeprep"
)

type Application struct {
	config       *sharedconfig.Config
	logger       *slog.Logger
	store        *store.Service
	github       *toolgithub.Client
	githubSync   *githubsync.Service
	git          *toolgit.Client
	worktreePrep *worktreeprep.Service
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
		logger = logging.NewJSON(io.Discard)
	}

	storeService := store.New(store.Dependencies{Logger: logger})
	githubClient := toolgithub.New(toolgithub.Dependencies{Logger: logger})
	githubSyncService := githubsync.New(githubsync.Dependencies{
		Logger: logger,
		GitHub: githubClient,
		Store:  storeService,
	})
	taskListService := tasklist.New(tasklist.Dependencies{
		Logger: logger,
		Store:  taskListStoreAdapter{service: storeService},
	})
	gitClient := toolgit.New(toolgit.Dependencies{Logger: logger})
	worktreePrepService := worktreeprep.New(worktreeprep.Dependencies{
		Logger: logger,
		Git:    gitClient,
	})
	timelineService := timeline.New(timeline.Dependencies{Logger: logger})
	agentRunner := agentrun.NewExecutor(agentrun.ExecCommandRunner{})
	orchestratorService := orchestrator.New(orchestrator.Dependencies{
		Logger:      logger,
		Store:       storeService,
		GitHub:      githubClient,
		Git:         gitClient,
		Timeline:    timelineService,
		AgentRunner: agentRunner,
	})
	leaderService := leader.New(leader.Dependencies{
		Logger:       logger,
		Store:        storeService,
		TaskList:     taskListService,
		WorktreePrep: worktreePrepService,
		Orchestrator: orchestratorService,
		Timeline:     timelineService,
	})

	return &Application{
		config:       opts.Config,
		logger:       logger,
		store:        storeService,
		github:       githubClient,
		githubSync:   githubSyncService,
		git:          gitClient,
		worktreePrep: worktreePrepService,
		timeline:     timelineService,
		orchestrator: orchestratorService,
		leader:       leaderService,
	}
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

func (a *Application) Serve(ctx context.Context) error {
	if err := a.Bootstrap(ctx); err != nil {
		return err
	}

	handler, err := a.HTTPHandler()
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", a.config.Server.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.config.Server.ListenAddr, err)
	}
	defer func() {
		_ = listener.Close()
	}()

	server := &http.Server{
		Handler: handler,
	}

	serveDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-serveDone:
			return
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	a.logger.Info("toolhub listening", slog.String("listen_addr", listener.Addr().String()))
	err = server.Serve(listener)
	close(serveDone)
	<-shutdownDone
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		if ctx.Err() != nil {
			return nil
		}
		return nil
	}
	return fmt.Errorf("serve http: %w", err)
}

func (a *Application) HTTPHandler() (http.Handler, error) {
	if a.config == nil {
		return nil, errors.New("nil config")
	}
	if a.githubSync == nil {
		return nil, errors.New("nil github sync service")
	}

	mux := http.NewServeMux()
	mux.Handle("/github/webhook", githubsync.NewWebhookHandler(a.githubSync, githubsync.ExecuteOptions{
		WorkDir:       repoWorkDirFromConfigPath(a.config.Path),
		Repo:          a.config.Repo.GitHubOwner + "/" + a.config.Repo.GitHubRepo,
		DefaultBranch: a.config.Repo.DefaultBranch,
	}))
	return mux, nil
}

func (a *Application) ComponentNames() []string {
	return []string{
		a.store.Name(),
		a.github.Name(),
		a.git.Name(),
		a.worktreePrep.Name(),
		a.timeline.Name(),
		a.orchestrator.Name(),
		a.leader.Name(),
	}
}

func repoWorkDirFromConfigPath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "."
	}

	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == "config" {
		return filepath.Dir(dir)
	}
	return dir
}
