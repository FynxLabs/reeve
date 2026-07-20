package run

import (
	"testing"

	"github.com/thefynx/reeve/internal/core/summary"
)

// aggregateOutcome is what ApplyOutput.Failed is derived from. A stack that
// errors during apply must win over a blocked stack: the CLI keys its non-zero
// exit on "failed", and swallowing an error behind a "blocked" verdict is
// exactly the bug that let a pulumi lock failure ship a green CI job.
func TestAggregateOutcome(t *testing.T) {
	err := summary.StackSummary{Status: summary.StatusError}
	blocked := summary.StackSummary{Status: summary.StatusBlocked}
	planned := summary.StackSummary{Status: summary.StatusPlanned}

	tests := []struct {
		name       string
		ss         []summary.StackSummary
		anyBlocked bool
		want       string
	}{
		{"clean apply", []summary.StackSummary{planned}, false, "success"},
		{"one errored", []summary.StackSummary{planned, err}, false, "failed"},
		{"only blocked", []summary.StackSummary{blocked}, true, "blocked"},
		{"errored beats blocked", []summary.StackSummary{blocked, err}, true, "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aggregateOutcome(tt.ss, tt.anyBlocked); got != tt.want {
				t.Fatalf("aggregateOutcome = %q, want %q", got, tt.want)
			}
		})
	}
}
