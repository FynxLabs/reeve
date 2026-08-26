package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/summary"
)

func overflowStacks(n, diffLen int, status summary.Status) []summary.StackSummary {
	out := make([]summary.StackSummary, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, summary.StackSummary{
			Project: fmt.Sprintf("environments-project-%02d", i), Stack: "default", Env: "default",
			Counts:      summary.Counts{Change: 1},
			Status:      status,
			PlanDiff:    strings.Repeat("~ change\n", diffLen),
			PlanSummary: "~ one resource",
		})
	}
	return out
}

func previewIn(stacks []summary.StackSummary) PreviewInput {
	return PreviewInput{
		Op: "preview", RunNumber: 7, CommitSHA: "abc1234def", CIRunURL: "https://ci/7",
		Stacks: stacks,
	}
}

// Off by default: an operator who has not opted in sees exactly what they saw
// before, one comment.
func TestOverflowDisabledByDefault(t *testing.T) {
	in := previewIn(overflowStacks(60, 600, summary.StatusPlanned))
	if parts := PreviewParts(in, OverflowConfig{}, Marker); parts != nil {
		t.Fatalf("overflow must be opt-in; got %d parts", len(parts))
	}
}

// A board that fits must not paginate, and must be byte-identical to the
// unpaginated render - that is what makes this safe to enable.
func TestOverflowFittingBoardIsUnchanged(t *testing.T) {
	in := previewIn(overflowStacks(3, 2, summary.StatusPlanned))
	parts := PreviewParts(in, OverflowConfig{Mode: OverflowContinue}, Marker)
	if parts != nil {
		t.Fatalf("a board that fits must stay one comment; got %d parts", len(parts))
	}
}

// The whole point: no stack detail is dropped, every part fits, and no stack is
// split across two parts.
func TestOverflowPaginatesWithoutLosingStacks(t *testing.T) {
	for _, split := range []string{SplitDivided, SplitStack, SplitGroup} {
		t.Run(split, func(t *testing.T) {
			stacks := overflowStacks(60, 600, summary.StatusPlanned)
			in := previewIn(stacks)
			parts := PreviewParts(in, OverflowConfig{Mode: OverflowContinue, Split: split}, Marker)
			if len(parts) < 2 {
				t.Fatalf("expected pagination, got %d parts", len(parts))
			}

			seen := map[string]int{}
			for _, p := range parts {
				if len(p.Body) > githubCommentMaxLen {
					t.Errorf("part %d over the limit: %d", p.Ordinal, len(p.Body))
				}
				if strings.Count(p.Body, "<details>") != strings.Count(p.Body, "</details>") {
					t.Errorf("part %d has an unclosed <details>", p.Ordinal)
				}
				if strings.Count(p.Body, "```")%2 != 0 {
					t.Errorf("part %d has an unclosed code fence", p.Ordinal)
				}
				for _, ref := range p.Stacks {
					seen[ref]++
				}
			}
			// Every stack appears exactly once: none lost, none duplicated
			// across parts.
			if len(seen) != len(stacks) {
				t.Errorf("want %d stacks placed, got %d", len(stacks), len(seen))
			}
			for ref, n := range seen {
				if n != 1 {
					t.Errorf("stack %s appears in %d parts; a stack must not be split", ref, n)
				}
			}
		})
	}
}

// Part 1 carries the board's own marker so an existing comment keeps being
// edited; later parts get their own.
func TestOverflowPartMarkers(t *testing.T) {
	if got := PartMarker(Marker, 1); got != Marker {
		t.Fatalf("part 1 must be byte-identical to the board marker, got %q", got)
	}
	got := PartMarker(Marker, 3)
	want := "<!-- reeve:pr-comment:v1:part3 -->"
	if got != want {
		t.Fatalf("part 3 marker: got %q want %q", got, want)
	}
	// Under section the SHA stays in the marker, and parts nest inside it.
	sec := DashboardMarker(StyleSection, "abc1234def")
	if p2 := PartMarker(sec, 2); !strings.Contains(p2, "abc1234") || !strings.Contains(p2, "part2") {
		t.Fatalf("section part marker lost the SHA or the ordinal: %q", p2)
	}
	// The prefix must match every part, so a shrinking run can find them all.
	pre := PartMarkerPrefix(Marker)
	for _, m := range []string{PartMarker(Marker, 1), PartMarker(Marker, 2), PartMarker(Marker, 9)} {
		if !strings.HasPrefix(m, pre) {
			t.Fatalf("prefix %q does not match part marker %q", pre, m)
		}
	}
	parts := PreviewParts(previewIn(overflowStacks(60, 600, summary.StatusPlanned)),
		OverflowConfig{Mode: OverflowContinue}, Marker)
	for _, part := range parts {
		if !strings.HasPrefix(part.Body, part.Marker) {
			t.Errorf("part %d body is not discoverable by marker %q", part.Ordinal, part.Marker)
		}
	}
}

// The table is the board's index: whole on part 1, absent from the rest.
func TestOverflowTableOnlyOnFirstPart(t *testing.T) {
	stacks := overflowStacks(60, 600, summary.StatusPlanned)
	parts := PreviewParts(previewIn(stacks), OverflowConfig{Mode: OverflowContinue}, Marker)
	if len(parts) < 2 {
		t.Fatal("expected pagination")
	}
	if !strings.Contains(parts[0].Body, "| Stack | Env |") {
		t.Error("part 1 must carry the table")
	}
	// Every stack must be listed on part 1, or the index is incomplete.
	for _, s := range stacks {
		if !strings.Contains(parts[0].Body, s.Ref()) {
			t.Fatalf("stack %s missing from the part 1 table", s.Ref())
		}
	}
	for _, part := range parts {
		for _, ref := range part.Stacks {
			row := tableRow(parts[0].Body, ref)
			if !strings.Contains(row, fmt.Sprintf("part %d", part.Ordinal)) {
				t.Errorf("table row for %s does not point to part %d: %q", ref, part.Ordinal, row)
			}
		}
	}
	for _, p := range parts[1:] {
		if strings.Contains(p.Body, "| Stack | Env |") {
			t.Errorf("part %d repeats the table", p.Ordinal)
		}
	}
}

// A reader landing on any part must know where they are.
func TestOverflowPartsAreSelfLocating(t *testing.T) {
	parts := PreviewParts(previewIn(overflowStacks(60, 600, summary.StatusPlanned)),
		OverflowConfig{Mode: OverflowContinue}, Marker)
	if len(parts) < 2 {
		t.Fatal("expected pagination")
	}
	for _, p := range parts {
		want := fmt.Sprintf("Part %d of %d", p.Ordinal, len(parts))
		if !strings.Contains(p.Body, want) {
			t.Errorf("part %d does not state %q", p.Ordinal, want)
		}
	}
	if !strings.Contains(parts[1].Body, "table is on part 1") {
		t.Error("a later part must point back at the table")
	}
}

// divided balances; it must not leave a one-stack tail beside a full part.
func TestOverflowDividedBalances(t *testing.T) {
	parts := PreviewParts(previewIn(overflowStacks(60, 600, summary.StatusPlanned)),
		OverflowConfig{Mode: OverflowContinue, Split: SplitDivided}, Marker)
	if len(parts) < 2 {
		t.Fatal("expected pagination")
	}
	lo, hi := len(parts[0].Stacks), len(parts[0].Stacks)
	for _, p := range parts {
		if len(p.Stacks) < lo {
			lo = len(p.Stacks)
		}
		if len(p.Stacks) > hi {
			hi = len(p.Stacks)
		}
	}
	// An even split differs by at most one stack between parts.
	if hi-lo > 1 {
		t.Errorf("divided split is ragged: sizes range %d..%d", lo, hi)
	}
}

// group keeps a status group together so a reader after failures opens one part.
func TestOverflowGroupKeepsStatusesTogether(t *testing.T) {
	var stacks []summary.StackSummary
	stacks = append(stacks, overflowStacks(12, 400, summary.StatusError)...)
	for i := range stacks {
		stacks[i].Error = "boom"
	}
	planned := overflowStacks(12, 400, summary.StatusPlanned)
	for i := range planned {
		planned[i].Project = "planned-" + planned[i].Project
	}
	stacks = append(stacks, planned...)

	parts := PreviewParts(previewIn(stacks), OverflowConfig{Mode: OverflowContinue, Split: SplitGroup}, Marker)
	if len(parts) < 2 {
		t.Fatal("expected pagination")
	}
	// No part may mix a failure with a planned stack.
	byRef := map[string]summary.Status{}
	for _, s := range stacks {
		byRef[s.Ref()] = s.Status
	}
	for _, p := range parts {
		seen := map[summary.Status]bool{}
		for _, ref := range p.Stacks {
			seen[byRef[ref]] = true
		}
		if len(seen) > 1 {
			t.Errorf("part %d mixes statuses: %v", p.Ordinal, seen)
		}
	}
}

// A stack too large for a whole comment cannot paginate; it trims, and the rest
// of the board is unaffected.
func TestOverflowSingleOversizeStackTrimsAlone(t *testing.T) {
	stacks := overflowStacks(8, 400, summary.StatusPlanned)
	stacks[0].PlanDiff = strings.Repeat("d", 200_000)
	parts := PreviewParts(previewIn(stacks), OverflowConfig{Mode: OverflowContinue}, Marker)
	if len(parts) < 2 {
		t.Fatal("expected pagination")
	}
	trimmed := 0
	for _, p := range parts {
		if len(p.Body) > githubCommentMaxLen {
			t.Errorf("part %d over the limit: %d", p.Ordinal, len(p.Body))
		}
		if p.Trim.Lost() {
			trimmed++
		}
	}
	if trimmed == 0 {
		t.Error("the oversize stack's part must report what it dropped")
	}
	if trimmed == len(parts) {
		t.Error("only the part holding the oversize stack should have to trim")
	}
}

// max_parts bounds the blast radius, and what it leaves out is stated.
func TestOverflowMaxPartsCapsAndSaysSo(t *testing.T) {
	stacks := overflowStacks(200, 600, summary.StatusPlanned)
	parts := PreviewParts(previewIn(stacks),
		OverflowConfig{Mode: OverflowContinue, Split: SplitStack, MaxParts: 3}, Marker)
	if len(parts) > 3 {
		t.Fatalf("max_parts not honored: %d parts", len(parts))
	}
	placed := 0
	for _, p := range parts {
		placed += len(p.Stacks)
	}
	if placed >= len(stacks) {
		t.Skip("cap not reached with this fixture")
	}
	last := parts[len(parts)-1].Body
	if !strings.Contains(last, "past the comment limit") {
		t.Errorf("stacks dropped past the cap must be named:\n%s", last[max(0, len(last)-400):])
	}
	if len(parts[len(parts)-1].OmittedStacks) != len(stacks)-placed {
		t.Fatalf("omitted refs=%d want=%d", len(parts[len(parts)-1].OmittedStacks), len(stacks)-placed)
	}
	for _, ref := range parts[len(parts)-1].OmittedStacks {
		if row := tableRow(parts[0].Body, ref); !strings.Contains(row, "run log") {
			t.Errorf("omitted stack %s is not mapped to the run log: %q", ref, row)
		}
	}
}

func tableRow(body, ref string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| "+ref+" |") {
			return line
		}
	}
	return ""
}

// Apply and refresh paginate on the same terms.
func TestOverflowAppliesToApplyAndRefresh(t *testing.T) {
	stacks := overflowStacks(60, 600, summary.StatusPlanned)
	for i := range stacks {
		stacks[i].FullPlan = strings.Repeat("f", 4_000)
	}
	cfg := OverflowConfig{Mode: OverflowContinue}

	ap := ApplyParts(ApplyInput{RunNumber: 1, CommitSHA: "abc1234", CIRunURL: "https://ci/1", Stacks: stacks}, cfg, Marker)
	rp := RefreshParts(RefreshInput{RunNumber: 1, CommitSHA: "abc1234", CIRunURL: "https://ci/1", Stacks: stacks}, cfg, RefreshMarker)

	for name, parts := range map[string][]Part{"apply": ap, "refresh": rp} {
		if len(parts) < 2 {
			t.Errorf("%s did not paginate: %d parts", name, len(parts))
			continue
		}
		for _, p := range parts {
			if len(p.Body) > githubCommentMaxLen {
				t.Errorf("%s part %d over the limit: %d", name, p.Ordinal, len(p.Body))
			}
		}
	}
}
