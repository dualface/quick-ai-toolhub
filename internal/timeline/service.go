package timeline

import "log/slog"

type Service struct {
	logger *slog.Logger
}

type Dependencies struct {
	Logger *slog.Logger
}

func New(deps Dependencies) *Service {
	return &Service{
		logger: componentLogger(deps.Logger),
	}
}

func (s *Service) Name() string {
	return "timeline"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "timeline")
	}
	return logger.With("component", "timeline")
}
