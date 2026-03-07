package git

import "log/slog"

type Client struct {
	logger *slog.Logger
	runner Runner
}

type Dependencies struct {
	Logger *slog.Logger
	Runner Runner
}

func New(deps Dependencies) *Client {
	runner := deps.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	return &Client{
		logger: componentLogger(deps.Logger),
		runner: runner,
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
