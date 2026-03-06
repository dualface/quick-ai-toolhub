package github

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
	return "github"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "github")
	}
	return logger.With("component", "github")
}
