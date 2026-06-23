package render

import (
	"strings"
	"testing"
)

func TestApplyTimelineAccumulates(t *testing.T) {
	in := TimelineInput{
		RunID:     "apply-118-87dc303",
		RunNumber: 118,
		CommitSHA: "87dc303abc",
		CIRunURL:  "https://ci/run/118",
		Entries: []TimelineEntry{
			{Icon: "🚀", Label: "apply starting"},
			{Icon: "✅", Label: "applied", Detail: "2 stack(s): a/prod, b/prod"},
		},
	}
	got := ApplyTimeline(in)

	if !strings.Contains(got, ApplyTimelineMarker("apply-118-87dc303")) {
		t.Errorf("missing per-run marker:\n%s", got)
	}
	if !strings.Contains(got, "run #118") {
		t.Errorf("missing run number:\n%s", got)
	}
	if !strings.Contains(got, "apply starting") || !strings.Contains(got, "applied") {
		t.Errorf("missing timeline entries:\n%s", got)
	}
	if !strings.Contains(got, "2 stack(s): a/prod, b/prod") {
		t.Errorf("missing detail:\n%s", got)
	}
	if !strings.Contains(got, "[View run](https://ci/run/118)") {
		t.Errorf("missing run link:\n%s", got)
	}
}

func TestApplyTimelineMarkerUniquePerRun(t *testing.T) {
	if ApplyTimelineMarker("apply-1-aaa") == ApplyTimelineMarker("apply-2-bbb") {
		t.Fatal("markers must differ per run")
	}
}

func TestPreviewNoticeBanner(t *testing.T) {
	body := Preview(PreviewInput{
		Op:        "preview",
		RunNumber: 5,
		CommitSHA: "abc1234",
		Notice:    "Commit abc1234 was already applied on run #4.",
	})
	if !strings.Contains(body, "ℹ️") || !strings.Contains(body, "already applied on run #4") {
		t.Errorf("notice banner missing:\n%s", body)
	}
}
