package run

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/reeveops/reeve/internal/core/render"
	"github.com/reeveops/reeve/internal/core/summary"
)

// boardVCS records what a board wrote and what it swept.
type boardVCS struct {
	upserts   []string // markers, in write order
	posts     []string
	bodies    map[string]string
	upsertErr map[string]error

	deletedPrefix string
	deletedKeep   int
	deleteCalls   int
	deleteErr     error
	deleteCount   int

	supportsDelete bool
}

func (f *boardVCS) PostComment(_ context.Context, _ int, body string) error {
	f.posts = append(f.posts, body)
	return nil
}

func (f *boardVCS) UpsertComment(_ context.Context, _ int, body, marker string) error {
	if err := f.upsertErr[marker]; err != nil {
		return err
	}
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	f.upserts = append(f.upserts, marker)
	f.bodies[marker] = body
	return nil
}

// deletingVCS adds the optional delete capability.
type deletingVCS struct{ *boardVCS }

func (f deletingVCS) DeleteCommentsByMarkerPrefix(_ context.Context, _ int, prefix string, keep int) (int, error) {
	f.deleteCalls++
	f.deletedPrefix = prefix
	f.deletedKeep = keep
	return f.deleteCount, f.deleteErr
}

func threeParts() []render.Part {
	return []render.Part{
		{Ordinal: 1, Marker: render.Marker, Body: "part one"},
		{Ordinal: 2, Marker: render.PartMarker(render.Marker, 2), Body: "part two"},
		{Ordinal: 3, Marker: render.PartMarker(render.Marker, 3), Body: "part three"},
	}
}

// Every part is written, under its own marker, part 1 first - a reader watching
// the PR sees the table before the detail.
func TestPostBoardWritesEveryPartInOrder(t *testing.T) {
	fv := &boardVCS{}
	if err := postBoard(context.Background(), fv, 12, "preview", threeParts(), render.Marker, nil); err != nil {
		t.Fatalf("postBoard: %v", err)
	}
	want := []string{render.Marker, render.PartMarker(render.Marker, 2), render.PartMarker(render.Marker, 3)}
	if len(fv.upserts) != len(want) {
		t.Fatalf("wrote %d parts, want %d: %v", len(fv.upserts), len(want), fv.upserts)
	}
	for i, m := range want {
		if fv.upserts[i] != m {
			t.Errorf("part %d written under %q, want %q", i+1, fv.upserts[i], m)
		}
	}
}

func TestPostAppendBoardCreatesEveryPart(t *testing.T) {
	fv := &boardVCS{}
	if err := postAppendBoard(context.Background(), fv, 12, "preview", threeParts(), nil); err != nil {
		t.Fatalf("postAppendBoard: %v", err)
	}
	if len(fv.posts) != 3 || len(fv.upserts) != 0 {
		t.Fatalf("posts=%d upserts=%d; append must create each part", len(fv.posts), len(fv.upserts))
	}
}

// A failed part must not cost the parts after it: the reader is better served by
// a board missing one comment than by a board that stops mid-write.
func TestPostBoardContinuesPastAFailedPart(t *testing.T) {
	boom := errors.New("comment API failed")
	fv := &boardVCS{upsertErr: map[string]error{render.PartMarker(render.Marker, 2): boom}}

	err := postBoard(context.Background(), fv, 12, "preview", threeParts(), render.Marker, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("the failure must be reported: %v", err)
	}
	// Parts 1 and 3 still landed.
	if len(fv.upserts) != 2 {
		t.Fatalf("expected the other two parts to be written: %v", fv.upserts)
	}
	if _, ok := fv.bodies[render.PartMarker(render.Marker, 3)]; !ok {
		t.Error("part 3 must be written even though part 2 failed")
	}
}

// A run that shrinks must remove the parts it no longer writes, or a stale part
// keeps claiming stacks the run does not have.
func TestPostBoardSweepsStaleParts(t *testing.T) {
	base := &boardVCS{supportsDelete: true, deleteCount: 2}
	fv := deletingVCS{base}

	if err := postBoard(context.Background(), fv, 12, "preview", threeParts(), render.Marker, nil); err != nil {
		t.Fatalf("postBoard: %v", err)
	}
	if base.deleteCalls != 1 {
		t.Fatalf("expected one sweep, got %d", base.deleteCalls)
	}
	if base.deletedKeep != 3 {
		t.Errorf("sweep must keep the 3 parts just written, kept %d", base.deletedKeep)
	}
	if base.deletedPrefix != render.PartMarkerPrefix(render.Marker) {
		t.Errorf("sweep prefix %q must be the board's own", base.deletedPrefix)
	}
}

func TestPostSingleBoardSweepsAllContinuationParts(t *testing.T) {
	base := &boardVCS{deleteCount: 3}
	fv := deletingVCS{base}
	if err := postSingleBoard(context.Background(), fv, 12, "preview", "one", render.Marker); err != nil {
		t.Fatalf("postSingleBoard: %v", err)
	}
	if base.deleteCalls != 1 || base.deletedKeep != 1 {
		t.Fatalf("delete calls=%d keep=%d; single board must sweep parts above 1", base.deleteCalls, base.deletedKeep)
	}
}

// A sweep failure is a reporting defect, not a reason to fail a run whose real
// work already shipped.
func TestPostBoardSweepFailureIsNotFatal(t *testing.T) {
	base := &boardVCS{supportsDelete: true, deleteErr: errors.New("no permission")}
	fv := deletingVCS{base}

	if err := postBoard(context.Background(), fv, 12, "preview", threeParts(), render.Marker, nil); err != nil {
		t.Fatalf("a sweep failure must not fail the run: %v", err)
	}
}

// An adapter without the capability leaves stale parts rather than erroring.
func TestPostBoardWithoutDeleteCapability(t *testing.T) {
	fv := &boardVCS{}
	if err := postBoard(context.Background(), fv, 12, "preview", threeParts(), render.Marker, nil); err != nil {
		t.Fatalf("postBoard: %v", err)
	}
}

// A part that had to trim carries content the log must hold, exactly as an
// unpaginated trimmed board does - and only that part's own stacks.
func TestPostBoardLogsATrimmedPartsOwnStacks(t *testing.T) {
	stacks := []summary.StackSummary{
		{Project: "api", Stack: "prod", Error: "api exploded"},
		{Project: "web", Stack: "prod", Error: "web exploded"},
	}
	parts := []render.Part{
		{Ordinal: 1, Marker: render.Marker, Body: "one", Stacks: []string{"api/prod"},
			Trim: render.Trim{ClampedErrors: true}},
		{Ordinal: 2, Marker: render.PartMarker(render.Marker, 2), Body: "two", Stacks: []string{"web/prod"}},
	}
	fv := &boardVCS{}
	out := captureLogs(t, func() {
		if err := postBoard(context.Background(), fv, 12, "preview", parts, render.Marker, stacks); err != nil {
			t.Fatalf("postBoard: %v", err)
		}
	})
	if !strings.Contains(out, "api exploded") {
		t.Errorf("the trimmed part's stack must be logged:\n%s", out)
	}
	if strings.Contains(out, "web exploded") {
		t.Errorf("a part that did not trim must not log its stacks:\n%s", out)
	}
}

func TestPostBoardLogsApplyOutputDroppedByOnePart(t *testing.T) {
	stacks := []summary.StackSummary{{Project: "api", Stack: "prod", FullPlan: "apply output"}}
	parts := []render.Part{{Ordinal: 1, Marker: render.Marker, Body: "one",
		Stacks: []string{"api/prod"}, Trim: render.Trim{DroppedFullPlan: true}}}
	out := captureLogs(t, func() {
		if err := postBoard(context.Background(), &boardVCS{}, 12, "apply", parts, render.Marker, stacks); err != nil {
			t.Fatalf("postBoard: %v", err)
		}
	})
	if !strings.Contains(out, "apply output") {
		t.Errorf("dropped apply output missing from log:\n%s", out)
	}
}

func TestStacksByRefSelectsOnlyThePartsOwn(t *testing.T) {
	all := []summary.StackSummary{
		{Project: "a", Stack: "prod"}, {Project: "b", Stack: "prod"}, {Project: "c", Stack: "prod"},
	}
	got := stacksByRef(all, []string{"a/prod", "c/prod"})
	if len(got) != 2 || got[0].Ref() != "a/prod" || got[1].Ref() != "c/prod" {
		t.Fatalf("wrong selection: %+v", got)
	}
}
