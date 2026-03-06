package logging

import (
	"io"
	"log/slog"
)

func NewJSON(w io.Writer) *slog.Logger {
	if w == nil {
		w = io.Discard
	}
	return slog.New(slog.NewJSONHandler(w, nil))
}

func InitDefault(w io.Writer) *slog.Logger {
	logger := NewJSON(w)
	slog.SetDefault(logger)
	return logger
}
