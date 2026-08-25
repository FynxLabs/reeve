package run

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/render"
	"github.com/reeveops/reeve/internal/core/summary"
)

// captureLogs swaps the default slog handler for the duration of fn and
// returns what was written.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// The comment's trim note points the reviewer at the CI run, so the dropped
// diff has to be in the run log. Nothing else in the pipeline writes it there.
func TestLogTrimmedPlansEmitsDroppedDiff(t *testing.T) {
	stacks := []summary.StackSummary{
		{Project: "api", Stack: "prod", Env: "prod", PlanDiff: "~ iam role changed"},
		{Project: "web", Stack: "prod", Env: "prod", PlanDiff: "+ s3 bucket added"},
	}
	out := captureLogs(t, func() {
		logTrimmedPlans(stacks, render.Trim{DroppedFullPlan: true, DroppedDiff: true})
	})
	for _, want := range []string{"api/prod", "iam role changed", "web/prod", "s3 bucket added"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
}

// Dropping the raw full plan alone loses nothing a reviewer reads, so it must
// not dump hundreds of KB of engine JSON into every trimmed run's log.
func TestLogTrimmedPlansSilentWhenDiffSurvives(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", PlanDiff: "~ iam role"}}
	out := captureLogs(t, func() {
		logTrimmedPlans(stacks, render.Trim{DroppedFullPlan: true})
	})
	if out != "" {
		t.Fatalf("nothing should be logged when the diff survives:\n%s", out)
	}
}

func TestLogTrimmedPlansSilentWhenNothingDropped(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", PlanDiff: "~ iam role"}}
	out := captureLogs(t, func() { logTrimmedPlans(stacks, render.Trim{}) })
	if out != "" {
		t.Fatalf("untrimmed comment must log nothing:\n%s", out)
	}
}

func TestLogTrimmedApplyOutputEmitsDroppedOutput(t *testing.T) {
	stacks := []summary.StackSummary{
		{Project: "api", Stack: "prod", FullPlan: "apply complete: 3 changed"},
	}
	out := captureLogs(t, func() {
		logTrimmedApplyOutput(stacks, render.Trim{DroppedFullPlan: true})
	})
	if !strings.Contains(out, "apply complete: 3 changed") || !strings.Contains(out, "api/prod") {
		t.Fatalf("apply output not logged:\n%s", out)
	}
}

func TestLogTrimmedApplyOutputSilentWhenNothingDropped(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", FullPlan: "apply complete"}}
	out := captureLogs(t, func() { logTrimmedApplyOutput(stacks, render.Trim{}) })
	if out != "" {
		t.Fatalf("untrimmed comment must log nothing:\n%s", out)
	}
}

func TestLogTrimmedRefreshOutputEmitsDroppedOutput(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", FullPlan: "refreshed 12 resources"}}
	out := captureLogs(t, func() {
		logTrimmedRefreshOutput(stacks, render.Trim{DroppedFullPlan: true})
	})
	if !strings.Contains(out, "refreshed 12 resources") {
		t.Fatalf("refresh output not logged:\n%s", out)
	}
}
