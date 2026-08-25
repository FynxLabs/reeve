package run

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/render"
	"github.com/reeveops/reeve/internal/core/summary"
)

// captureLogs swaps the default slog handler for the duration of fn and returns
// what was written.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// The comment's banner points the reviewer at the run output, so the dropped
// diff has to be there. Nothing else in the pipeline writes it.
func TestLogTrimmedEmitsDroppedDiff(t *testing.T) {
	stacks := []summary.StackSummary{
		{Project: "api", Stack: "prod", Env: "prod", PlanDiff: "~ iam role changed"},
		{Project: "web", Stack: "prod", Env: "prod", PlanDiff: "+ s3 bucket added"},
	}
	out := captureLogs(t, func() {
		logTrimmed("preview", stacks, render.Trim{DroppedFullPlan: true, DroppedDiff: true})
	})
	for _, want := range []string{"api/prod", "iam role changed", "web/prod", "s3 bucket added"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
}

// Dropping the raw engine blob alone loses nothing a reviewer reads, so it must
// not dump hundreds of KB of JSON into every trimmed preview's log.
func TestLogTrimmedSilentWhenOnlyFullPlanDropped(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", PlanDiff: "~ iam role", FullPlan: "{}"}}
	out := captureLogs(t, func() {
		logTrimmed("preview", stacks, render.Trim{DroppedFullPlan: true})
	})
	if out != "" {
		t.Fatalf("nothing should be logged when the diff survives:\n%s", out)
	}
}

func TestLogTrimmedSilentWhenNothingDropped(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", PlanDiff: "~ iam role"}}
	out := captureLogs(t, func() { logTrimmed("preview", stacks, render.Trim{}) })
	if out != "" {
		t.Fatalf("untrimmed comment must log nothing:\n%s", out)
	}
}

// A stack whose whole section was dropped is invisible in the comment, so its
// error has to be in the log or it is nowhere at all.
func TestLogTrimmedEmitsErrorsForDroppedSections(t *testing.T) {
	stacks := []summary.StackSummary{
		{Project: "api", Stack: "prod", Status: summary.StatusError, Error: "aws:rds permission denied"},
	}
	out := captureLogs(t, func() {
		logTrimmed("preview", stacks, render.Trim{DroppedSections: 1})
	})
	if !strings.Contains(out, "aws:rds permission denied") {
		t.Fatalf("dropped stack's error must be logged:\n%s", out)
	}
}

// Clamped errors are cut in the comment, so the full text belongs in the log.
func TestLogTrimmedEmitsFullErrorWhenClamped(t *testing.T) {
	long := strings.Repeat("stack frame\n", 500)
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", Error: long}}
	out := captureLogs(t, func() {
		logTrimmed("preview", stacks, render.Trim{ClampedErrors: true})
	})
	if !strings.Contains(out, "stack frame") {
		t.Fatalf("clamped error must be logged in full:\n%s", out)
	}
}

// A stack missing from the table entirely is named nowhere in the comment.
func TestLogTrimmedEmitsRowsDroppedFromTable(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", Error: "boom"}}
	out := captureLogs(t, func() {
		logTrimmed("preview", stacks, render.Trim{DroppedRows: 1})
	})
	if !strings.Contains(out, "stacks_not_in_table=1") {
		t.Fatalf("the summary line must report untabled stacks:\n%s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("an untabled stack's error must be logged:\n%s", out)
	}
}

// For an apply, FullPlan is the engine's account of what changed, so it is
// logged whenever the comment lost reviewer-visible content.
func TestLogTrimmedEmitsApplyOutput(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", FullPlan: "apply complete: 3 changed"}}
	out := captureLogs(t, func() {
		logTrimmed("apply", stacks, render.Trim{DroppedFullPlan: true, DroppedDiff: true})
	})
	if !strings.Contains(out, "apply complete: 3 changed") {
		t.Fatalf("apply output not logged:\n%s", out)
	}
}

func TestLogTrimmedEmitsRefreshOutput(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", FullPlan: "refreshed 12 resources"}}
	out := captureLogs(t, func() {
		logTrimmed("refresh", stacks, render.Trim{DroppedFullPlan: true, DroppedDiff: true})
	})
	if !strings.Contains(out, "refreshed 12 resources") {
		t.Fatalf("refresh output not logged:\n%s", out)
	}
}
