package render

import "fmt"

// ApplyTimelineMarker returns the hidden HTML marker that pins a single run's
// timeline comment. Keyed by runID so each apply run owns ONE comment that is
// edited in place as the run progresses (started -> applied/blocked/failed),
// producing a chronological log rather than a single overwritten status.
func ApplyTimelineMarker(runID string) string {
	return fmt.Sprintf("<!-- reeve:apply-timeline:%s -->", runID)
}

// TimelineEntry is one line in a run's timeline.
type TimelineEntry struct {
	Icon   string // emoji, e.g. "🚀" "✅" "🔴" "🔒" "⏭️"
	Label  string // short status, e.g. "apply starting", "applied"
	Detail string // optional extra context (reason, counts), may be empty
}

// TimelineInput is what ApplyTimeline renders.
type TimelineInput struct {
	RunID     string // pins the comment marker
	RunNumber int
	CommitSHA string
	CIRunURL  string
	Entries   []TimelineEntry
}

// ApplyTimeline renders a run's full timeline comment. It is re-rendered from
// the in-memory entry list each time a new event lands and upserted under the
// per-run marker, so the comment grows one line per event.
func ApplyTimeline(in TimelineInput) string {
	var b []byte
	b = append(b, ApplyTimelineMarker(in.RunID)...)
	b = append(b, '\n')
	header := fmt.Sprintf("### 🚀 reeve · apply · %s · [commit %s]\n\n",
		runRef(in.RunNumber, in.CIRunURL), shortSHA(in.CommitSHA))
	b = append(b, header...)
	for _, e := range in.Entries {
		line := fmt.Sprintf("- %s **%s**", e.Icon, e.Label)
		if e.Detail != "" {
			line += ": " + e.Detail
		}
		b = append(b, (line + "\n")...)
	}
	return string(b)
}
