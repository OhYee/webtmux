package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Configure installs the process-wide logger used by the server and its
// workers. Credentials, request headers, terminal contents, and tmux output
// must never be added as attributes.
func Configure(level, format string, quiet bool) {
	var parsed slog.Level
	switch strings.ToLower(level) {
	case "debug":
		parsed = slog.LevelDebug
	case "warn":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	default:
		parsed = slog.LevelInfo
	}
	var output io.Writer = os.Stderr
	if quiet {
		output = io.Discard
	}
	opts := &slog.HandlerOptions{Level: parsed}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(output, opts)
	} else {
		handler = slog.NewTextHandler(output, opts)
	}
	slog.SetDefault(slog.New(handler))
}
