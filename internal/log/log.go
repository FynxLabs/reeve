// Package log is reeve's structured logging seam. It wraps log/slog with a
// process-wide default that subcommands install once at startup and that
// every other package reads via slog.Default().
//
// Conventions:
//   - Debug:  per-iteration trace (e.g. each check_run inspected). Off by default.
//   - Info:   user-facing progress that belongs in CI logs at normal verbosity.
//   - Warn:   recoverable adapter failures - the run continues but the operator
//     should know (Slack post failed, audit write failed, lock release
//     returned an error after work shipped).
//   - Error:  unrecoverable failures that abort the run. Almost always paired
//     with returning the error up the stack.
//
// No package-level mutable state is exported - callers use slog.Default().
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level parses a level string (case-insensitive) into a slog.Level. Unknown
// values fall back to LevelInfo. The empty string also resolves to Info so
// callers can pass an unset env var directly.
func Level(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Format selects between text (human-readable) and json output. Default is
// text on TTY-attached stderr; CI workflows can opt into json by setting
// REEVE_LOG_FORMAT=json.
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// ParseFormat maps "text" / "json" (case-insensitive) to a Format. Empty or
// unknown values resolve to FormatText.
func ParseFormat(name string) Format {
	if strings.EqualFold(strings.TrimSpace(name), "json") {
		return FormatJSON
	}
	return FormatText
}

// Install builds a handler at the given level/format writing to w (typically
// os.Stderr) and installs it as slog.Default(). Returns the installed logger
// for callers that want to pass it explicitly instead of relying on the
// default.
func Install(w io.Writer, level slog.Level, format Format) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	switch format {
	case FormatJSON:
		h = slog.NewJSONHandler(w, opts)
	default:
		h = slog.NewTextHandler(w, opts)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger
}

// FromEnv installs a logger from REEVE_LOG_LEVEL and REEVE_LOG_FORMAT,
// suitable for `cmd/reeve` PersistentPreRun and for tests that need
// deterministic output. Honours an explicit override pair when non-empty.
func FromEnv(levelOverride, formatOverride string) *slog.Logger {
	return FromConfig(levelOverride, formatOverride, "", "")
}

// FromConfig installs a logger with priority: flag > env > config file.
// cfgLevel and cfgFormat come from shared.yaml; flag/env take precedence.
func FromConfig(levelFlag, formatFlag, cfgLevel, cfgFormat string) *slog.Logger {
	level := levelFlag
	if level == "" {
		level = os.Getenv("REEVE_LOG_LEVEL")
	}
	if level == "" {
		level = cfgLevel
	}
	format := formatFlag
	if format == "" {
		format = os.Getenv("REEVE_LOG_FORMAT")
	}
	if format == "" {
		format = cfgFormat
	}
	return Install(os.Stderr, Level(level), ParseFormat(format))
}
