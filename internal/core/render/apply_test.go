package render

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/reeveops/reeve/internal/core/summary"
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
				Status:      summary.StatusPlanned,
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

// TestApplySizeLimit_DropsFullPlan verifies Apply enforces the same
// GitHub 65,536-char comment-size cap as Preview.
func TestApplySizeLimit_DropsFullPlan(t *testing.T) {
	bigPlan := strings.Repeat("x", 5*1024)
	stacks := make([]summary.StackSummary, 18)
	for i := range stacks {
		stacks[i] = summary.StackSummary{
			Project:    "infra",
			Stack:      "stack-" + string(rune('a'+i)),
			Env:        "prod",
			Counts:     summary.Counts{Add: 1},
			Status:     summary.StatusPlanned,
			FullPlan:   bigPlan,
			DurationMS: 12_000,
		}
	}
	out := Apply(ApplyInput{
		RunNumber: 5, CommitSHA: "abc",
		CIRunURL: "https://example.com/runs/5",
		Stacks:   stacks,
	})

	if len(out) > githubCommentMaxLen {
		t.Fatalf("apply body %d chars exceeds limit %d", len(out), githubCommentMaxLen)
	}
	if strings.Contains(out, "Full apply output") {
		t.Errorf("expected FullPlan section to be dropped")
	}
	if !strings.Contains(out, "Output trimmed to fit GitHub's 65,536-char comment limit") {
		t.Errorf("expected truncation notice in apply body")
	}
	if !strings.Contains(out, "https://example.com/runs/5") {
		t.Errorf("expected truncation notice to link the CI run URL")
	}
}

func TestApplyPreviewGatesRender(t *testing.T) {
	// Preview comment with a blocked stack carrying a gate trace.
	in := PreviewInput{
		Op: "preview", RunNumber: 2, CommitSHA: "1234567",
		Stacks: []summary.StackSummary{
			{
				Project: "api", Stack: "prod", Env: "prod",
				Counts: summary.Counts{Add: 1},
				Status: summary.StatusPlanned,
				Gates: []summary.GateTrace{
					{Gate: "up_to_date", Outcome: "pass", Reason: "branch up-to-date with base"},
					{Gate: "approvals", Outcome: "fail", Reason: "approvals not satisfied"},
				},
			},
		},
	}
	assertGolden(t, "preview_with_gates.md", Preview(in))
}

func TestApplyBreakGlassSectionIsLoud(t *testing.T) {
	body := Apply(ApplyInput{
		RunNumber: 4, CommitSHA: "abcdef1234",
		Stacks: []summary.StackSummary{{Project: "api", Stack: "prod", Status: summary.StatusPlanned}},
		BreakGlass: &BreakGlassNote{
			Actor:              "alice",
			Justification:      "prod is down\nsecond line",
			AuthorizedVia:      "internal_list",
			Overridden:         []string{"approvals", "not_in_freeze"},
			ConfigModifiedInPR: true,
		},
	})
	for _, want := range []string{
		BreakGlassMarker,
		"[!WARNING]",
		"BREAK-GLASS APPLY",
		"@alice",
		"`internal_list`",
		"`approvals`", "`not_in_freeze`",
		"modified in this same PR",
		"> > prod is down",
		"> > second line",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("break-glass comment missing %q:\n%s", want, body)
		}
	}
}

func TestApplyBreakGlassJustificationClampKeepsValidUTF8(t *testing.T) {
	justification := strings.Repeat("é", breakGlassJustificationBudget)
	body := Apply(ApplyInput{
		RunNumber: 4, CommitSHA: "abcdef1234",
		BreakGlass: &BreakGlassNote{Actor: "alice", Justification: justification},
	})
	if !strings.Contains(body, "justification truncated; see the run log") {
		t.Fatal("oversize justification must point to the complete run log")
	}
	if !utf8.ValidString(body) {
		t.Fatal("justification clamp split a UTF-8 encoding")
	}
}

func TestApplyWithoutBreakGlassHasNoMarker(t *testing.T) {
	body := Apply(ApplyInput{RunNumber: 4, CommitSHA: "abcdef1234"})
	if strings.Contains(body, BreakGlassMarker) {
		t.Fatal("non-break-glass apply must not carry the break-glass marker")
	}
}

// A trim that drops the engine's apply output must say so, because the comment
// then tells the reviewer to read the CI run and the caller has to put it there.
func TestApplyTrimmedReportsDroppedOutput(t *testing.T) {
	huge := strings.Repeat("x", githubCommentMaxLen)
	in := ApplyInput{
		RunNumber: 7, CommitSHA: "abc1234", CIRunURL: "https://example.com/runs/7",
		Stacks: []summary.StackSummary{{
			Project: "api", Stack: "prod", Env: "prod",
			Counts: summary.Counts{Change: 1}, Status: summary.StatusPlanned,
			FullPlan: huge,
		}},
	}
	body, trim := ApplyTrimmed(in)
	if !trim.DroppedFullPlan {
		t.Fatal("dropping the apply output must be reported")
	}
	if strings.Contains(body, huge) {
		t.Fatal("oversize output should have been dropped from the body")
	}
	if len(body) > githubCommentMaxLen {
		t.Fatalf("body still over the limit: %d", len(body))
	}
}

func TestApplyTrimmedReportsNothingWhenItFits(t *testing.T) {
	_, trim := ApplyTrimmed(ApplyInput{RunNumber: 1, CommitSHA: "abc1234"})
	if trim.DroppedFullPlan || trim.DroppedDiff {
		t.Fatalf("a comment that fits drops nothing: %+v", trim)
	}
}

// Apply must land on the commit's board, not a second operation-keyed one, so
// the reader has one place to look per commit.
func TestApplySharesTheCommitBoardWithPreview(t *testing.T) {
	const sha = "abc1234def5678"
	previewBody := Preview(PreviewInput{RunNumber: 4, CommitSHA: sha, Style: StyleSection})
	applyBody := Apply(ApplyInput{RunNumber: 5, CommitSHA: sha, Style: StyleSection})

	want := DashboardMarker(StyleSection, sha)
	if !strings.HasPrefix(previewBody, want) || !strings.HasPrefix(applyBody, want) {
		t.Fatalf("preview and apply must open with the same commit board marker %q", want)
	}
	// The retired operation-split marker must never be written again.
	for _, body := range []string{previewBody, applyBody} {
		if strings.Contains(body, "reeve:apply:v1") {
			t.Fatalf("retired apply marker written:\n%s", body)
		}
	}
}

// A different commit is a different board, so the earlier commit's plan stays
// readable on the PR instead of being overwritten.
func TestApplyOnNewCommitDoesNotTouchTheOldBoard(t *testing.T) {
	old := DashboardMarker(StyleSection, "aaaaaaa1111")
	body := Apply(ApplyInput{RunNumber: 6, CommitSHA: "bbbbbbb2222", Style: StyleSection})
	if strings.Contains(body, old) {
		t.Fatalf("a new commit's apply must not target the previous commit's board:\n%s", body)
	}
}

func TestApplyReplaceStyleUsesPRWideMarker(t *testing.T) {
	body := Apply(ApplyInput{RunNumber: 7, CommitSHA: "abc1234def5678"})
	if !strings.HasPrefix(body, Marker) {
		t.Fatalf("replace must upsert the PR-wide board:\n%s", body[:min(120, len(body))])
	}
}
