package render

import (
	"testing"

	"github.com/thefynx/reeve/internal/core/summary"
)

func TestApplyGolden_Mixed(t *testing.T) {
	in := ApplyInput{
		RunNumber:   99,
		CommitSHA:   "deadbeef1234",
		DurationSec: 120,
		CIRunURL:    "https://example.com/runs/99",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Counts:      summary.Counts{Add: 2, Change: 1},
				Status:      summary.StatusReady,
				DurationMS:  47_000,
				PlanSummary: "+ s3 bucket\n~ iam role",
			},
			{
				Project: "worker", Stack: "prod", Env: "prod",
				Status:     summary.StatusError,
				DurationMS: 12_000,
				Error:      "aws:rds: Permission denied",
			},
			{
				Project: "api", Stack: "staging", Env: "staging",
				Status: summary.StatusBlocked, BlockedBy: 501,
			},
		},
	}
	assertGolden(t, "apply_mixed.md", Apply(in))
}

func TestApplyGolden_Empty(t *testing.T) {
	assertGolden(t, "apply_empty.md", Apply(ApplyInput{RunNumber: 1, CommitSHA: "x"}))
}

func TestApplyPreviewGatesRender(t *testing.T) {
	// Preview comment with a blocked stack carrying a gate trace.
	in := PreviewInput{
		Op: "preview", RunNumber: 2, CommitSHA: "1234567",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Counts: summary.Counts{Add: 1},
				Status: summary.StatusReady,
				Gates: []summary.GateTrace{
					{Gate: "up_to_date", Outcome: "pass", Reason: "branch up-to-date with base"},
					{Gate: "approvals", Outcome: "fail", Reason: "approvals not satisfied"},
				},
			},
		},
	}
	assertGolden(t, "preview_with_gates.md", Preview(in))
}
