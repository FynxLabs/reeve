package pulumi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/thefynx/reeve/internal/core/summary"
)

// previewJSON is the subset of `pulumi preview --json` output we consume.
// Pulumi emits a top-level object with "steps" (plan steps) and
// "changeSummary" (totals by op). We use changeSummary when present and
// fall back to counting steps.
type previewJSON struct {
	ChangeSummary map[string]int `json:"changeSummary"`
	Steps         []previewStep  `json:"steps"`
	Diagnostics   []diag         `json:"diagnostics"`
}

type previewStep struct {
	Op       string   `json:"op"`
	URN      string   `json:"urn"`
	Type     string   `json:"type"`
	Provider string   `json:"provider"`
	OldState any      `json:"oldState"`
	NewState any      `json:"newState"`
	Keys     []string `json:"keys"`
}

type diag struct {
	Severity string `json:"severity"` // "error" | "warning" | "info"
	URN      string `json:"urn"`
	Message  string `json:"message"`
}

// parsePreview converts a `pulumi preview --json` stdout blob into counts
// and a short summary. Errors from diagnostics float into the returned
// error string (caller decides whether that's fatal).
func parsePreview(stdout []byte) (summary.Counts, string, string, error) {
	var p previewJSON
	if err := json.Unmarshal(stdout, &p); err != nil {
		return summary.Counts{}, "", "", fmt.Errorf("parse pulumi preview json: %w", err)
	}

	counts := countsFromSummary(p.ChangeSummary)
	if counts.Total() == 0 && len(p.Steps) > 0 {
		counts = countsFromSteps(p.Steps)
	}

	short := shortSummary(p.Steps, 10)
	var diagMsg string
	for _, d := range p.Diagnostics {
		if d.Severity == "error" {
			if diagMsg != "" {
				diagMsg += "\n"
			}
			diagMsg += d.Message
		}
	}
	return counts, short, diagMsg, nil
}

func countsFromSummary(cs map[string]int) summary.Counts {
	var c summary.Counts
	// Pulumi uses "create", "update", "delete", "replace", "same", "read",
	// "import", "discard", "create-replacement", "delete-replaced", etc.
	c.Add += cs["create"] + cs["import"]
	c.Change += cs["update"]
	c.Delete += cs["delete"]
	c.Replace += cs["replace"] + cs["create-replacement"]
	return c
}

func countsFromSteps(steps []previewStep) summary.Counts {
	var c summary.Counts
	for _, s := range steps {
		switch s.Op {
		case "create", "import":
			c.Add++
		case "update":
			c.Change++
		case "delete":
			c.Delete++
		case "replace", "create-replacement":
			c.Replace++
		}
	}
	return c
}

func shortSummary(steps []previewStep, limit int) string {
	if len(steps) == 0 {
		return ""
	}
	var b strings.Builder
	shown := 0
	for _, s := range steps {
		prefix := opPrefix(s.Op)
		if prefix == "" {
			continue
		}
		if shown >= limit {
			fmt.Fprintf(&b, "...and %d more\n", len(steps)-shown)
			break
		}
		fmt.Fprintf(&b, "%s %s  %s\n", prefix, s.Type, displayName(s.URN))
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

func opPrefix(op string) string {
	switch op {
	case "create", "import":
		return "+"
	case "update":
		return "~"
	case "delete":
		return "-"
	case "replace", "create-replacement":
		return "±"
	}
	return ""
}

// displayName returns the resource name portion of a Pulumi URN, or the
// full URN if parsing fails.
func displayName(urn string) string {
	// URN: urn:pulumi:<stack>::<project>::<type>::<name>
	idx := strings.LastIndex(urn, "::")
	if idx < 0 {
		return urn
	}
	return urn[idx+2:]
}
