package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/thefynx/reeve/internal/notify"
	"github.com/thefynx/reeve/internal/slack"
)

// deliverDrift posts one message per drift event (dashboard-style; drift
// runs are usually daily and bucket-side idempotency covers re-runs).
func (s *Sink) deliverDrift(ctx context.Context, p notify.Payload) error {
	text := buildDriftMessage(p)
	blocks := []slack.Block{
		slack.Header(fmt.Sprintf("reeve · drift · %s", labelDriftEvent(p.Event))),
		slack.Section(text),
	}
	_, err := s.client.Upsert(ctx, s.channel, "", text, blocks)
	return err
}

func buildDriftMessage(p notify.Payload) string {
	d := p.Drift
	var b strings.Builder
	fmt.Fprintf(&b, "*%s* - %s (+%d ~%d -%d ±%d)\n",
		d.Ref(), labelDriftEvent(p.Event),
		d.Add, d.Change, d.Delete, d.Replace,
	)
	if d.Error != "" {
		fmt.Fprintf(&b, "_error:_ %s\n", slack.Escape(slack.Truncate(d.Error, 200)))
	}
	if d.PlanSummary != "" {
		fmt.Fprintf(&b, "```%s```\n", slack.FenceSafe(slack.Truncate(d.PlanSummary, 500)))
	}
	return b.String()
}

func labelDriftEvent(e notify.Event) string {
	switch e {
	case notify.EventDriftDetected:
		return "🆕 drift detected"
	case notify.EventDriftOngoing:
		return "🔁 ongoing"
	case notify.EventDriftResolved:
		return "✅ resolved"
	case notify.EventCheckFailed:
		return "💥 check failed"
	}
	return string(e)
}
