package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/summary"
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
				Status:   summary.StatusPlanned,
				FullPlan: "pulumi preview output here\nline two",
			},
			{
				Project: "api", Stack: "staging", Env: "staging",
				Counts: summary.Counts{Add: 5},
				Status: summary.StatusPlanned,
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

// A multi-line engine error goes in a fenced block: it is the part an
// operator acts on, and folding it into one markdown line either breaks the
// layout or loses everything after the first newline.
func TestPreviewGolden_MultilineError(t *testing.T) {
	t.Parallel()

	in := PreviewInput{
		Op: "preview", RunNumber: 9, CommitSHA: "deadbee",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Status: summary.StatusError,
				Error: "aws:s3:Bucket (data):\n" +
					"  error: creating S3 bucket: AccessDenied: not authorized\n" +
					"aws:iam:Role (task): error: entity already exists",
			},
		},
	}
	assertGolden(t, "preview_error_multiline.md", Preview(in))
}

func TestWriteError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"empty", "", ""},
		{"single line stays inline", "boom", "  **Error:** boom\n\n"},
		{"trailing newline is not multi-line", "boom\n", "  **Error:** boom\n\n"},
		{"multi-line is fenced", "one\ntwo", "  **Error:**\n\n```\none\ntwo\n```\n\n"},
	}
	for _, tt := range tests {
		var b strings.Builder
		writeError(&b, tt.msg)
		if got := b.String(); got != tt.want {
			t.Errorf("%s: writeError = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Engine errors quote resource names and properties that come from the PR, so
// a ``` run in the message must not close the fence and let the rest render as
// markup.
func TestWriteErrorFenceSafe(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	writeError(&b, "line one\n```\n### not a heading")
	got := b.String()
	if strings.Contains(got, "\n```\n### not a heading") {
		t.Fatalf("payload fence escaped the block: %q", got)
	}
	if !strings.HasSuffix(got, "\n```\n\n") {
		t.Fatalf("block must still be closed: %q", got)
	}
	if !strings.Contains(got, "### not a heading") {
		t.Fatalf("content lost: %q", got)
	}
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

// TestPreviewSizeLimit_DropsFullPlan verifies that when the body exceeds
// GitHub's 65,536-char comment limit, FullPlan is dropped silently --
// reviewers don't read it from the PR comment (CI logs have it), and the
// diff -- which IS what reviewers read -- survives intact. No truncation
// notice should fire because no reviewer-visible content was lost.
func TestPreviewSizeLimit_DropsFullPlan(t *testing.T) {
	// 18 stacks each with a 5KB FullPlan = 90KB, well over the limit.
	bigPlan := strings.Repeat("x", 5*1024)
	stacks := make([]summary.StackSummary, 18)
	for i := range stacks {
		stacks[i] = summary.StackSummary{
			Project:  "infra",
			Stack:    "stack-" + string(rune('a'+i)),
			Env:      "prod",
			Counts:   summary.Counts{Add: 1},
			Status:   summary.StatusPlanned,
			FullPlan: bigPlan,
			PlanDiff: "+ resource foo\n+ resource bar",
		}
	}
	in := PreviewInput{
		Op:        "preview",
		RunNumber: 14,
		CommitSHA: "61964c0",
		CIRunURL:  "https://example.com/runs/14",
		Stacks:    stacks,
	}
	out := Preview(in)

	if len(out) > githubCommentMaxLen {
		t.Fatalf("body %d chars exceeds limit %d", len(out), githubCommentMaxLen)
	}
	if strings.Contains(out, "Full plan output") {
		t.Errorf("expected FullPlan section to be dropped, but it appears in the output")
	}
	// No truncation notice when only FullPlan is dropped -- diffs are intact.
	if strings.Contains(out, "Output trimmed") {
		t.Errorf("did not expect truncation notice when only FullPlan was dropped (diffs are intact)")
	}
	// Per-stack diff is lighter and should survive when only FullPlan is dropped.
	if !strings.Contains(out, "+ resource foo") {
		t.Errorf("expected PlanDiff to survive when only FullPlan needed to be dropped")
	}
}

// TestPreviewSizeLimit_DropsDiffToo verifies the second tier: when even
// dropping FullPlan isn't enough, PlanDiff is dropped as well.
func TestPreviewSizeLimit_DropsDiffToo(t *testing.T) {
	// 40 stacks, each with a 2KB PlanDiff: ~80KB of diff alone, plus
	// table rows and headings push us comfortably past the limit even
	// without FullPlan.
	bigDiff := strings.Repeat("+ resource line\n", 128) // ~2KB
	stacks := make([]summary.StackSummary, 40)
	for i := range stacks {
		stacks[i] = summary.StackSummary{
			Project:  "infra",
			Stack:    "stack-" + strings.Repeat("x", 30) + "-" + string(rune('a'+i%26)),
			Env:      "env",
			Counts:   summary.Counts{Add: 1},
			Status:   summary.StatusPlanned,
			PlanDiff: bigDiff,
		}
	}
	in := PreviewInput{
		Op: "preview", RunNumber: 1, CommitSHA: "abc",
		CIRunURL: "https://example.com/runs/1",
		Stacks:   stacks,
	}
	out := Preview(in)

	if len(out) > githubCommentMaxLen {
		t.Fatalf("body %d chars exceeds limit %d", len(out), githubCommentMaxLen)
	}
	if strings.Contains(out, "<details><summary>Diff</summary>") {
		t.Errorf("expected per-stack Diff section to be dropped at second tier")
	}
	// The raw plan blob is dropped silently on a preview - the diff a reviewer
	// reads survived that rung - so the note names only what was actually lost.
	if !strings.Contains(out, "omitted: per-stack diff") {
		t.Errorf("expected the notice to name the dropped diff; got:\n%s", out[:min(400, len(out))])
	}
	if strings.Contains(out, "full plan output") {
		t.Errorf("the silently-dropped plan blob must not be named:\n%s", out[:min(400, len(out))])
	}
}

// A pathologically long table must still fit - and must stay a well-formed
// document. This used to end in a byte truncate, which sliced through the table
// and left GitHub rendering the remainder as garbage.
func TestPreviewSizeLimit_OversizeTableStaysWellFormed(t *testing.T) {
	// 5,000 stacks — each row is short but cumulatively past 65KB.
	stacks := make([]summary.StackSummary, 5000)
	for i := range stacks {
		stacks[i] = summary.StackSummary{
			Project: "p", Stack: "s" + string(rune('a'+i%26)),
			Counts: summary.Counts{Add: 1},
			Status: summary.StatusPlanned,
		}
	}
	out, trim := PreviewTrimmed(PreviewInput{Op: "preview", RunNumber: 1, CommitSHA: "x", Stacks: stacks})
	if len(out) > githubCommentMaxLen {
		t.Fatalf("body over the limit: %d chars > %d", len(out), githubCommentMaxLen)
	}
	if strings.Contains(out, "hard-truncated") {
		t.Error("the blind byte truncate must be gone")
	}
	// Rows had to go, and the reader must be told rather than left assuming the
	// table is complete.
	if trim.DroppedRows == 0 {
		t.Fatalf("expected table rows to be dropped: %+v", trim)
	}
	if !strings.Contains(out, "more stacks") {
		t.Errorf("dropped rows not accounted for; tail:\n%s", out[max(0, len(out)-400):])
	}
	// A truncated table row would leave a ragged final line.
	if !strings.HasSuffix(strings.TrimSpace(out), ".") && !strings.Contains(out, "|") {
		t.Error("table looks malformed")
	}
}

// TestPreviewSizeLimit_UnderBudgetUnchanged verifies that bodies safely
// under the limit are emitted verbatim (no truncation notice added).
func TestPreviewSizeLimit_UnderBudgetUnchanged(t *testing.T) {
	in := PreviewInput{
		Op: "preview", RunNumber: 1, CommitSHA: "x",
		Stacks: []summary.StackSummary{
			{
				Project: "p", Stack: "s", Env: "e",
				Counts:   summary.Counts{Add: 1},
				Status:   summary.StatusPlanned,
				FullPlan: "small plan output",
				PlanDiff: "+ one line",
			},
		},
	}
	out := Preview(in)
	if strings.Contains(out, "Output trimmed") {
		t.Errorf("under-budget body should not have a truncation notice")
	}
	if !strings.Contains(out, "small plan output") {
		t.Errorf("under-budget body should contain the full plan verbatim")
	}
}

// Dropping FullPlan while the diff survives loses nothing a reviewer reads, so
// it stays silent - and the caller must not be told to log it.
func TestPreviewTrimmedFullPlanOnlyIsSilent(t *testing.T) {
	in := PreviewInput{
		RunNumber: 3, CommitSHA: "abc1234", CIRunURL: "https://example.com/runs/3",
		Stacks: []summary.StackSummary{{
			Project: "api", Stack: "prod", Env: "prod",
			Counts: summary.Counts{Change: 1}, Status: summary.StatusPlanned,
			PlanDiff: "~ iam role",
			FullPlan: strings.Repeat("x", githubCommentMaxLen),
		}},
	}
	body, trim := PreviewTrimmed(in)
	if !trim.DroppedFullPlan {
		t.Fatal("FullPlan was dropped and must be reported as dropped")
	}
	if trim.DroppedDiff {
		t.Fatal("the diff survived; it must not be reported as dropped")
	}
	if !strings.Contains(body, "~ iam role") {
		t.Fatal("the diff reviewers read must survive the trim")
	}
	if strings.Contains(body, "Output trimmed") {
		t.Fatal("dropping FullPlan alone must not stamp the trim note")
	}
}

// `section` keys the board to the commit: preview and apply of one SHA share
// it, a new SHA mints a new one. Keying on the operation (the original
// `section`) made two permanent boards and overwrote both every run.
func TestDashboardMarkerSectionIsPerCommit(t *testing.T) {
	a := DashboardMarker(StyleSection, "abc1234def5678")
	b := DashboardMarker(StyleSection, "999888777666")
	if a == b {
		t.Fatalf("two commits must not share a board: %q", a)
	}
	if a != "<!-- reeve:pr-comment:v1:abc1234 -->" {
		t.Fatalf("unexpected section marker: %q", a)
	}
	// Preview and apply of one commit must land on one comment.
	if got := DashboardMarker(StyleSection, "abc1234def5678"); got != a {
		t.Fatalf("marker not stable for one commit: %q vs %q", got, a)
	}
}

// A PR already running under replace must keep having its board edited. A
// changed marker orphans the comment instead.
func TestDashboardMarkerReplaceUnchanged(t *testing.T) {
	for _, style := range []string{StyleReplace, StyleAppend, ""} {
		if got := DashboardMarker(style, "abc1234def5678"); got != Marker {
			t.Fatalf("style %q must use the PR-wide marker, got %q", style, got)
		}
	}
	if Marker != "<!-- reeve:pr-comment:v1 -->" {
		t.Fatalf("existing dashboard comments would be orphaned: %q", Marker)
	}
}

// The rendered body and the upsert target have to agree, so the body must open
// with the same marker the run path upserts against.
func TestPreviewBodyOpensWithSectionMarker(t *testing.T) {
	body := Preview(PreviewInput{
		RunNumber: 5, CommitSHA: "abc1234def5678", Style: StyleSection,
	})
	want := DashboardMarker(StyleSection, "abc1234def5678")
	if !strings.HasPrefix(body, want) {
		t.Fatalf("body must open with %q:\n%s", want, body[:min(120, len(body))])
	}
}
