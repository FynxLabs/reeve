package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/summary"
)

func TestTrimLadderBoundsEveryShape(t *testing.T) {
	mk := func(n int, errLen, diffLen, sumLen int, refLen int) []summary.StackSummary {
		var out []summary.StackSummary
		for i := 0; i < n; i++ {
			out = append(out, summary.StackSummary{
				Project: strings.Repeat("p", refLen) + fmt.Sprint(i),
				Stack:   "production", Env: "production",
				Status:      summary.StatusError,
				Error:       strings.Repeat("e", errLen),
				PlanDiff:    strings.Repeat("d", diffLen),
				PlanSummary: strings.Repeat("s", sumLen),
				FullPlan:    strings.Repeat("f", diffLen),
			})
		}
		return out
	}
	cases := []struct {
		name   string
		stacks []summary.StackSummary
	}{
		{"one huge error", mk(1, 400_000, 0, 0, 4)},
		{"many huge errors", mk(200, 5_000, 0, 0, 4)},
		{"thousands of rows", mk(5000, 50, 0, 0, 30)},
		{"one huge diff", mk(1, 0, 400_000, 0, 4)},
		{"huge summaries", mk(300, 10, 0, 3_000, 4)},
		{"everything huge", mk(500, 5_000, 5_000, 5_000, 40)},
		{"single stack all huge", mk(1, 100_000, 100_000, 100_000, 4)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, style := range []string{"", StyleSection} {
				body, trim := PreviewTrimmed(PreviewInput{
					RunNumber: 1, CommitSHA: "abc1234def", CIRunURL: "https://ci/1",
					Stacks: c.stacks, Style: style,
				})
				if len(body) > githubCommentMaxLen {
					t.Fatalf("style=%q len=%d > %d trim=%+v", style, len(body), githubCommentMaxLen, trim)
				}
				// Structure must survive: no unclosed fence or details.
				if strings.Count(body, "<details>") != strings.Count(body, "</details>") {
					t.Fatalf("style=%q unclosed <details> (len=%d)", style, len(body))
				}
				if strings.Count(body, "```")%2 != 0 {
					t.Fatalf("style=%q unclosed code fence (len=%d)", style, len(body))
				}
			}
		})
	}
}

func TestTrimLadderBoundsApplyAndRefresh(t *testing.T) {
	var stacks []summary.StackSummary
	for i := 0; i < 400; i++ {
		stacks = append(stacks, summary.StackSummary{
			Project: fmt.Sprintf("project-%d", i), Stack: "prod", Env: "prod",
			Status:      summary.StatusError,
			Error:       strings.Repeat("e", 5_000),
			FullPlan:    strings.Repeat("f", 5_000),
			PlanSummary: strings.Repeat("s", 5_000),
		})
	}
	ab, at := ApplyTrimmed(ApplyInput{RunNumber: 1, CommitSHA: "abc1234", Stacks: stacks})
	if len(ab) > githubCommentMaxLen {
		t.Fatalf("apply len=%d trim=%+v", len(ab), at)
	}
	rb, rt := RefreshTrimmed(RefreshInput{RunNumber: 1, CommitSHA: "abc1234", Stacks: stacks})
	if len(rb) > githubCommentMaxLen {
		t.Fatalf("refresh len=%d trim=%+v", len(rb), rt)
	}
	t.Logf("apply=%d %+v refresh=%d %+v", len(ab), at, len(rb), rt)
}

// Force every renderer down to the floor rungs: thousands of stacks with long
// refs, so the table alone cannot fit.
func TestTrimLadderFloorKeepsCommentWellFormed(t *testing.T) {
	var stacks []summary.StackSummary
	for i := 0; i < 6000; i++ {
		stacks = append(stacks, summary.StackSummary{
			Project: fmt.Sprintf("environments-a-rather-long-project-name-%d", i),
			Stack:   "default", Env: "default",
			Status:   summary.StatusError,
			Error:    strings.Repeat("e", 3_000),
			FullPlan: strings.Repeat("f", 3_000),
		})
	}
	pb, pt := PreviewTrimmed(PreviewInput{RunNumber: 1, CommitSHA: "abc1234", CIRunURL: "https://ci/1", Stacks: stacks})
	ab, at := ApplyTrimmed(ApplyInput{RunNumber: 1, CommitSHA: "abc1234", CIRunURL: "https://ci/1", Stacks: stacks})
	rb, rt := RefreshTrimmed(RefreshInput{RunNumber: 1, CommitSHA: "abc1234", CIRunURL: "https://ci/1", Stacks: stacks})

	for _, c := range []struct {
		name string
		body string
		trim Trim
	}{{"preview", pb, pt}, {"apply", ab, at}, {"refresh", rb, rt}} {
		if len(c.body) > githubCommentMaxLen {
			t.Errorf("%s: len=%d > %d trim=%+v", c.name, len(c.body), githubCommentMaxLen, c.trim)
		}
		if strings.Count(c.body, "<details>") != strings.Count(c.body, "</details>") {
			t.Errorf("%s: unclosed <details>", c.name)
		}
		if strings.Count(c.body, "```")%2 != 0 {
			t.Errorf("%s: unclosed code fence", c.name)
		}
		if !c.trim.Lost() {
			t.Errorf("%s: content was dropped but Lost() is false: %+v", c.name, c.trim)
		}
		// The reader must be told rows are missing, not left to assume the
		// table is complete.
		if c.trim.DroppedRows > 0 && !strings.Contains(c.body, "more stacks") {
			t.Errorf("%s: %d rows dropped with no note in the body", c.name, c.trim.DroppedRows)
		}
		if c.trim.DroppedSections > 0 && !strings.Contains(c.body, "omitted") {
			t.Errorf("%s: %d sections dropped with no note", c.name, c.trim.DroppedSections)
		}
		t.Logf("%s len=%d %+v", c.name, len(c.body), c.trim)
	}
}
