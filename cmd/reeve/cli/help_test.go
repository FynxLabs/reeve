package cli

import (
	"strings"
	"testing"
)

func TestBuildHelpComment_ContainsCommands(t *testing.T) {
	body := buildHelpComment(false)
	for _, want := range []string{
		"<!-- reeve:help -->",
		"/reeve apply",
		"/reeve ready",
		"/reeve help",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("help comment missing %q", want)
		}
	}
	if strings.Contains(body, "auto_ready") {
		t.Error("auto_ready hint should not appear when disabled")
	}
}

func TestBuildHelpComment_AutoReadyHint(t *testing.T) {
	body := buildHelpComment(true)
	if !strings.Contains(body, "auto_ready") {
		t.Error("expected auto_ready hint when enabled")
	}
}
