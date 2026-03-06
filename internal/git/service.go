package git

import "log/slog"

type Client struct {
	logger *slog.Logger
}

type Dependencies struct {
	Logger *slog.Logger
}

func New(deps Dependencies) *Client {
	return &Client{
		logger: componentLogger(deps.Logger),
	}
}

func (c *Client) Name() string {
	return "git"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "git")
	}
	return logger.With("component", "git")
}
