package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"garbage", slog.LevelInfo},
	}
	for _, c := range cases {
		if got := Level(c.in); got != c.want {
			t.Errorf("Level(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseFormat(t *testing.T) {
	if ParseFormat("json") != FormatJSON {
		t.Error("ParseFormat(json) should be JSON")
	}
	if ParseFormat("JSON") != FormatJSON {
		t.Error("ParseFormat is case-insensitive")
	}
	if ParseFormat("text") != FormatText {
		t.Error("ParseFormat(text) should be Text")
	}
	if ParseFormat("") != FormatText {
		t.Error("empty defaults to Text")
	}
}

func TestInstallEmitsAtLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Install(&buf, slog.LevelWarn, FormatText)
	logger.Info("hidden")
	logger.Warn("shown")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Errorf("info-level message leaked at warn threshold: %s", out)
	}
	if !strings.Contains(out, "shown") {
		t.Errorf("warn-level message missing: %s", out)
	}
}

func TestInstallSetsDefault(t *testing.T) {
	var buf bytes.Buffer
	Install(&buf, slog.LevelInfo, FormatJSON)
	slog.Info("hello", "k", "v")
	if !strings.Contains(buf.String(), `"msg":"hello"`) {
		t.Errorf("default logger did not pick up JSON handler: %s", buf.String())
	}
}
