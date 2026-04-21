package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/core/summary"
)

var update = flag.Bool("update", false, "update golden files")

func TestPreviewGolden_Basic(t *testing.T) {
	in := PreviewInput{
		Op:          "preview",
		RunNumber:   47,
		CommitSHA:   "abc1234deadbeef",
		DurationSec: 42,
		CIRunURL:    "https://example.com/runs/47",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Counts: summary.Counts{Add: 2, Change: 1},
				Status: summary.StatusBlocked, BlockedBy: 482,
				PlanSummary: "+aws:s3:Bucket logs-2026\n~aws:iam:Role app-role",
			},
			{
				Project: "worker", Stack: "prod", Env: "prod",
				Counts:   summary.Counts{Change: 3, Replace: 1},
				Status:   summary.StatusReady,
				FullPlan: "pulumi preview output here\nline two",
			},
			{
				Project: "api", Stack: "staging", Env: "staging",
				Counts: summary.Counts{Add: 5},
				Status: summary.StatusReady,
			},
			{
				Project: "noop", Stack: "dev", Env: "dev",
				Status: summary.StatusNoOp,
			},
		},
	}
	assertGolden(t, "preview_basic.md", Preview(in))
}

func TestPreviewGolden_NoStacks(t *testing.T) {
	in := PreviewInput{Op: "preview", RunNumber: 1, CommitSHA: "0000000"}
	assertGolden(t, "preview_empty.md", Preview(in))
}

func TestPreviewGolden_AllErrors(t *testing.T) {
	in := PreviewInput{
		Op: "preview", RunNumber: 9, CommitSHA: "deadbee",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Status: summary.StatusError,
				Error:  "pulumi preview failed: snake oil",
			},
		},
	}
	assertGolden(t, "preview_error.md", Preview(in))
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run go test -update to create): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch %s\n--- want ---\n%s\n--- got ---\n%s", name, string(want), got)
	}
}

func TestMarkerPresent(t *testing.T) {
	out := Preview(PreviewInput{Op: "preview", RunNumber: 1, CommitSHA: "x"})
	if !strings.HasPrefix(out, Marker) {
		t.Fatalf("output should start with marker %q", Marker)
	}
}
